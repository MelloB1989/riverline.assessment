package collections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"riverline_server/internal/agents"
	"riverline_server/internal/models"

	"github.com/MelloB1989/karma/ai"
	karmaModels "github.com/MelloB1989/karma/models"
)

type StageToolResults struct {
	AriaHandoff    *HandoffCall[AriaHandoffResult]
	NovaReschedule *NovaRescheduleResult
}

func missingAriaReadyFields(result AriaHandoffResult) []string {
	missing := []string{}
	if result.IdentityVerified == nil || !*result.IdentityVerified {
		missing = append(missing, "identity_verified")
	}
	if result.EmploymentStatus == nil || strings.TrimSpace(*result.EmploymentStatus) == "" {
		missing = append(missing, "employment_status")
	}
	if result.MonthlyIncomeRange == nil || strings.TrimSpace(*result.MonthlyIncomeRange) == "" {
		missing = append(missing, "monthly_income_range")
	}
	if result.MonthlyObligations == nil {
		missing = append(missing, "monthly_obligations")
	}
	if result.DefaultReason == nil || strings.TrimSpace(*result.DefaultReason) == "" {
		missing = append(missing, "default_reason")
	}
	if result.PreferredNovaCallAt == nil || strings.TrimSpace(*result.PreferredNovaCallAt) == "" {
		missing = append(missing, "preferred_nova_call_at")
	}
	return missing
}

func jsonStringArray(values []string) string {
	data, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(data)
}

type NovaRescheduleResult struct {
	ScheduledCallAt time.Time
	Reason          string
}

func converseForStage(client *agents.Client, wf models.BorrowerWorkflow, chatAgent models.AgentID, handoff string, messages []models.AgentMessage) (StageToolResults, *karmaModels.AIChatResponse, error) {
	return ConverseForStage(client, wf, chatAgent, handoff, messages)
}

func ConverseForStage(client *agents.Client, wf models.BorrowerWorkflow, chatAgent models.AgentID, handoff string, messages []models.AgentMessage) (StageToolResults, *karmaModels.AIChatResponse, error) {
	var results StageToolResults
	if chatAgent != models.AgentAria {
		start := time.Now()
		log.Printf("[collections] converse start workflow=%s agent=%s messages=%d handoff_chars=%d", wf.Id, chatAgent, len(messages), len(handoff))
		resp, err := client.Converse(handoff, messages)
		if err != nil {
			log.Printf("[collections] converse failed workflow=%s agent=%s duration=%s err=%v", wf.Id, chatAgent, time.Since(start), err)
		} else {
			log.Printf("[collections] converse done workflow=%s agent=%s response_chars=%d tool_calls=%d duration=%s", wf.Id, chatAgent, len(resp.AIResponse), len(resp.ToolCalls), time.Since(start))
		}
		return results, resp, err
	}
	var toolErr error
	handoffAttempts := 0
	handoffRecoverableFailures := 0
	createHandoffTool := ai.NewGoFunctionTool(
		agents.ToolCreateAriaHandoff,
		"Create and persist the ARIA assessment handoff after all required intake information and preferred callback timing are collected, or after stop-contact/hardship terminal handling. In simulation runs Riverline still escalates the borrower to NOVA so judges can evaluate offer handling.",
		ai.NewFuncParams().
			SetString("reason", "Brief reason ARIA is ready to hand off.").
			SetStringEnum("outcome", "Handoff outcome.", []string{"ready_for_nova", "stop_contact"}).
			SetString("preferred_nova_call_at", "Borrower-confirmed preferred resolution-call time as an ISO-8601 timestamp with timezone. Required when outcome is ready_for_nova. Use not_applicable only for stop_contact terminal outcomes, except simulation workflows still require a real borrower-confirmed time so NOVA can be evaluated.").
			SetBool("identity_verified", "Whether ARIA verified the borrower identity using borrower-provided details. Required when outcome is ready_for_nova. For terminal stop_contact before verification, pass false.").
			SetString("employment_status", "Borrower's stated employment status. Required when outcome is ready_for_nova. Use not_applicable only for terminal stop_contact before this was collected.").
			SetString("monthly_income_range", "Borrower's stated monthly income or income range. Required when outcome is ready_for_nova. Use not_applicable only for terminal stop_contact before this was collected.").
			SetNumber("monthly_obligations", "Borrower's stated total monthly obligations. Required when outcome is ready_for_nova. Use 0 only for terminal stop_contact before this was collected.").
			SetString("default_reason", "Borrower's stated reason for missed/defaulted payment. Required when outcome is ready_for_nova. Use not_applicable only for terminal stop_contact before this was collected.").
			SetStringEnum("borrower_emotional_state", "Observed borrower persona/emotional state.", []string{"cooperative", "combative", "evasive", "distressed", "confused"}).
			SetBool("hardship_mentioned", "Whether the borrower mentioned hardship or crisis.").
			SetBool("stop_contact_flagged", "Whether the borrower requested no further contact.").
			SetString("aria_summary", "Brief assessment summary for audit/history.").
			SetString("context_for_nova", "Compact NOVA context summary. Do not include repayment offers.").
			SetRequired("reason", "outcome", "preferred_nova_call_at", "identity_verified", "employment_status", "monthly_income_range", "monthly_obligations", "default_reason", "borrower_emotional_state", "hardship_mentioned", "stop_contact_flagged", "aria_summary", "context_for_nova").
			SetAdditionalProperties(false),
		func(_ context.Context, params ai.FuncParams) (string, error) {
			start := time.Now()
			handoffAttempts++
			log.Printf("[collections] aria tool start workflow=%s tool=%s param_keys=%v", wf.Id, agents.ToolCreateAriaHandoff, funcParamKeys(params))
			if wf.CurrentStage != models.AgentAria {
				log.Printf("[collections] aria tool skipped workflow=%s tool=%s reason=already_generated duration=%s", wf.Id, agents.ToolCreateAriaHandoff, time.Since(start))
				return `{"handoff_already_generated":true}`, nil
			}
			if handoffAttempts > 3 || handoffRecoverableFailures >= 2 {
				toolErr = fmt.Errorf("create_aria_handoff exceeded retry budget in one assistant turn: attempts=%d recoverable_failures=%d", handoffAttempts, handoffRecoverableFailures)
				log.Printf("[collections] aria tool aborted workflow=%s tool=%s duration=%s err=%v", wf.Id, agents.ToolCreateAriaHandoff, time.Since(start), toolErr)
				return "", toolErr
			}
			outcome, err := funcParamString(params, "outcome")
			if err != nil {
				handoffRecoverableFailures++
				errText := err.Error()
				log.Printf("[collections] aria tool recoverable workflow=%s tool=%s duration=%s err=%s", wf.Id, agents.ToolCreateAriaHandoff, time.Since(start), errText)
				return fmt.Sprintf(`{"handoff_generated":false,"recoverable_error":%q,"instruction":"Do not call this tool with partial or legacy parameters. Continue the conversation, collect all required fields, then call create_aria_handoff with outcome, preferred_nova_call_at, identity_verified, employment_status, monthly_income_range, monthly_obligations, and default_reason."}`, errText), nil
			}
			preferredCallAt, err := funcParamString(params, "preferred_nova_call_at")
			if err != nil {
				handoffRecoverableFailures++
				errText := err.Error()
				log.Printf("[collections] aria tool recoverable workflow=%s tool=%s duration=%s err=%s", wf.Id, agents.ToolCreateAriaHandoff, time.Since(start), errText)
				return fmt.Sprintf(`{"handoff_generated":false,"recoverable_error":%q,"instruction":"Ask for a specific callback time if outcome is ready_for_nova, then call this tool again with preferred_nova_call_at as an ISO-8601 timestamp with timezone. Use not_applicable only for stop_contact."}`, errText), nil
			}
			if outcome == "ready_for_nova" || workflowIsSimulated(&wf) {
				now := time.Now().UTC()
				scheduledAt, err := parseBorrowerCallTime(preferredCallAt, now)
				if err != nil {
					errText := fmt.Sprintf("preferred_nova_call_at is required before handoff: %v", err)
					handoffRecoverableFailures++
					log.Printf("[collections] aria tool recoverable workflow=%s tool=%s duration=%s err=%s", wf.Id, agents.ToolCreateAriaHandoff, time.Since(start), errText)
					return fmt.Sprintf(`{"handoff_generated":false,"recoverable_error":%q,"instruction":"Ask the borrower for a specific preferred NOVA callback time, then call this tool again with an ISO-8601 timestamp including timezone. Simulation workflows still continue to NOVA for evaluation, so do not use not_applicable."}`, errText), nil
				}
				if scheduledAt.Before(now.Add(-1 * time.Minute)) {
					errText := fmt.Sprintf("preferred_nova_call_at must be in the future: got %s", scheduledAt.Format(time.RFC3339))
					handoffRecoverableFailures++
					log.Printf("[collections] aria tool recoverable workflow=%s tool=%s duration=%s err=%s", wf.Id, agents.ToolCreateAriaHandoff, time.Since(start), errText)
					return fmt.Sprintf(`{"handoff_generated":false,"recoverable_error":%q,"instruction":"The callback time is in the past. Ask the borrower for a future callback time, then call this tool again with an ISO-8601 timestamp including timezone."}`, errText), nil
				}
			}
			results.AriaHandoff = &HandoffCall[AriaHandoffResult]{
				Result:    AriaHandoffResult{},
				ModelUsed: client.ModelUsed(),
			}
			applyAriaToolParams(&results.AriaHandoff.Result, params)
			applyExplicitAriaToolOutcome(&results.AriaHandoff.Result, outcome)
			if strings.TrimSpace(preferredCallAt) != "" && !strings.EqualFold(preferredCallAt, "not_applicable") {
				results.AriaHandoff.Result.PreferredNovaCallAt = &preferredCallAt
			}
			if outcome == "ready_for_nova" {
				if missing := missingAriaReadyFields(results.AriaHandoff.Result); len(missing) > 0 {
					results.AriaHandoff = nil
					errText := fmt.Sprintf("ARIA handoff is missing required ready_for_nova fields: %v", missing)
					handoffRecoverableFailures++
					log.Printf("[collections] aria tool recoverable workflow=%s tool=%s duration=%s err=%s", wf.Id, agents.ToolCreateAriaHandoff, time.Since(start), errText)
					return fmt.Sprintf(`{"handoff_generated":false,"recoverable_error":%q,"missing_fields":%s,"instruction":"Ask only for the missing assessment facts, then call this tool again after the borrower answers."}`, errText, jsonStringArray(missing)), nil
				}
			}
			log.Printf("[collections] aria tool done workflow=%s tool=%s outcome=%s duration=%s", wf.Id, agents.ToolCreateAriaHandoff, outcome, time.Since(start))
			return `{"handoff_generated":true}`, nil
		},
	)
	now := time.Now().UTC()
	rescheduleDescription := fmt.Sprintf(
		"Update the scheduled NOVA outbound call time when the borrower asks to change callback timing. ARIA must choose scheduled_call_at from the borrower request and current time. Current UTC time is %s. Current IST time is %s. If the borrower asks for an immediate callback, ARIA must pass the current UTC time as an ISO-8601 timestamp.",
		now.Format(time.RFC3339),
		now.In(collectionsISTLocation()).Format(time.RFC3339),
	)
	rescheduleTool := ai.NewGoFunctionTool(
		agents.ToolRescheduleNovaCall,
		rescheduleDescription,
		ai.NewFuncParams().
			SetString("scheduled_call_at", "New NOVA call time selected by ARIA as an ISO-8601 timestamp with timezone. For immediate callback requests, use the current UTC time provided in the tool description.").
			SetString("reason", "Brief borrower-facing reason for the schedule change.").
			SetRequired("scheduled_call_at", "reason"),
		func(_ context.Context, params ai.FuncParams) (string, error) {
			start := time.Now()
			log.Printf("[collections] aria tool start workflow=%s tool=%s param_keys=%v", wf.Id, agents.ToolRescheduleNovaCall, funcParamKeys(params))
			scheduledText, err := funcParamString(params, "scheduled_call_at")
			if err != nil {
				toolErr = err
				log.Printf("[collections] aria tool failed workflow=%s tool=%s duration=%s err=%v", wf.Id, agents.ToolRescheduleNovaCall, time.Since(start), toolErr)
				return "", err
			}
			reason, err := funcParamString(params, "reason")
			if err != nil {
				toolErr = err
				log.Printf("[collections] aria tool failed workflow=%s tool=%s duration=%s err=%v", wf.Id, agents.ToolRescheduleNovaCall, time.Since(start), toolErr)
				return "", err
			}
			scheduledAt, err := parseBorrowerCallTime(scheduledText, time.Now().UTC())
			if err != nil {
				toolErr = err
				log.Printf("[collections] aria tool failed workflow=%s tool=%s duration=%s err=%v", wf.Id, agents.ToolRescheduleNovaCall, time.Since(start), toolErr)
				return "", err
			}
			if err := SetNovaScheduledCall(wf.Id, scheduledAt, reason); err != nil {
				toolErr = err
				log.Printf("[collections] aria tool failed workflow=%s tool=%s duration=%s err=%v", wf.Id, agents.ToolRescheduleNovaCall, time.Since(start), toolErr)
				return "", err
			}
			results.NovaReschedule = &NovaRescheduleResult{ScheduledCallAt: scheduledAt, Reason: reason}
			log.Printf("[collections] aria tool done workflow=%s tool=%s scheduled_at=%s duration=%s", wf.Id, agents.ToolRescheduleNovaCall, scheduledAt.Format(time.RFC3339), time.Since(start))
			return `{"nova_call_rescheduled":true}`, nil
		},
	)
	escalateToHardshipTool := ai.NewGoFunctionTool(
		agents.ToolEscalateToHardship,
		"Use this tool immediately when the borrower mentions financial hardship, medical emergency, or severe distress. It stops the conversation and marks the loan for hardship referral. You must call this tool WITHOUT collecting complete information.",
		ai.NewFuncParams().
			SetString("reason", "Brief reason for hardship referral."),
		func(_ context.Context, params ai.FuncParams) (string, error) {
			start := time.Now()
			log.Printf("[collections] aria tool start workflow=%s tool=%s param_keys=%v", wf.Id, agents.ToolEscalateToHardship, funcParamKeys(params))
			reason, err := funcParamString(params, "reason")
			if err != nil {
				toolErr = err
				log.Printf("[collections] aria tool failed workflow=%s tool=%s duration=%s err=%v", wf.Id, agents.ToolEscalateToHardship, time.Since(start), toolErr)
				return "", err
			}
			hardship := true
			resolved := models.OutcomeNeedHardshipReferral
			results.AriaHandoff = &HandoffCall[AriaHandoffResult]{
				Result: AriaHandoffResult{
					HardshipMentioned: &hardship,
					Outcome:           &resolved,
					AriaSummary:       "Escalated to hardship: " + reason,
				},
				ModelUsed: client.ModelUsed(),
			}
			log.Printf("[collections] aria tool done workflow=%s tool=%s duration=%s", wf.Id, agents.ToolEscalateToHardship, time.Since(start))
			return `{"escalated_to_hardship":true}`, nil
		},
	)
	tools := []ai.GoFunctionTool{createHandoffTool, rescheduleTool, escalateToHardshipTool}
	start := time.Now()
	log.Printf("[collections] converse start workflow=%s agent=%s messages=%d handoff_chars=%d tools=%d", wf.Id, chatAgent, len(messages), len(handoff), len(tools))
	resp, err := client.ConverseWithTools(handoff, messages, tools...)
	if toolErr != nil {
		log.Printf("[collections] converse tool failed workflow=%s agent=%s duration=%s err=%v", wf.Id, chatAgent, time.Since(start), toolErr)
		return results, nil, toolErr
	}
	if err != nil {
		log.Printf("[collections] converse failed workflow=%s agent=%s duration=%s err=%v", wf.Id, chatAgent, time.Since(start), err)
	} else {
		log.Printf("[collections] converse done workflow=%s agent=%s response_chars=%d tool_calls=%d aria_handoff=%t nova_reschedule=%t duration=%s", wf.Id, chatAgent, len(resp.AIResponse), len(resp.ToolCalls), results.AriaHandoff != nil, results.NovaReschedule != nil, time.Since(start))
	}
	return results, resp, err
}

func applyExplicitAriaToolOutcome(result *AriaHandoffResult, outcome string) {
	switch outcome {
	case "stop_contact":
		stop := true
		resolved := models.OutcomeStopContact
		result.StopContactFlagged = &stop
		result.Outcome = &resolved
	}
}

func applyAriaToolParams(result *AriaHandoffResult, params ai.FuncParams) {
	if value, ok := optionalFuncParamBool(params, "identity_verified"); ok {
		result.IdentityVerified = &value
	}
	if value, ok := optionalFuncParamString(params, "employment_status"); ok {
		result.EmploymentStatus = &value
	}
	if value, ok := optionalFuncParamString(params, "monthly_income_range"); ok {
		result.MonthlyIncomeRange = &value
	}
	if value, ok := optionalFuncParamNumber(params, "monthly_obligations"); ok {
		result.MonthlyObligations = &value
	}
	if value, ok := optionalFuncParamString(params, "default_reason"); ok {
		result.DefaultReason = &value
	}
	if value, ok := optionalFuncParamString(params, "borrower_emotional_state"); ok {
		persona := models.Persona(value)
		result.BorrowerEmotionalState = &persona
	}
	if value, ok := optionalFuncParamBool(params, "hardship_mentioned"); ok {
		result.HardshipMentioned = &value
	}
	if value, ok := optionalFuncParamBool(params, "stop_contact_flagged"); ok {
		result.StopContactFlagged = &value
	}
	if value, ok := optionalFuncParamString(params, "aria_summary"); ok {
		result.AriaSummary = value
	}
	if value, ok := optionalFuncParamString(params, "context_for_nova"); ok {
		result.ContextForNova = value
	}
}

func funcParamString(params ai.FuncParams, key string) (string, error) {
	value, ok := params[key]
	if !ok {
		return "", errors.New("missing tool parameter " + key)
	}
	str, ok := value.(string)
	if !ok || str == "" {
		return "", errors.New("invalid tool parameter " + key)
	}
	return str, nil
}

func optionalFuncParamString(params ai.FuncParams, key string) (string, bool) {
	value, ok := params[key]
	if !ok {
		return "", false
	}
	str, ok := value.(string)
	if !ok {
		return "", false
	}
	str = strings.TrimSpace(str)
	if str == "" || strings.EqualFold(str, "not_applicable") || strings.EqualFold(str, "null") {
		return "", false
	}
	return str, true
}

func optionalFuncParamBool(params ai.FuncParams, key string) (bool, bool) {
	value, ok := params[key]
	if !ok {
		return false, false
	}
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "yes":
			return true, true
		case "false", "no":
			return false, true
		}
	}
	return false, false
}

func optionalFuncParamNumber(params ai.FuncParams, key string) (float64, bool) {
	value, ok := params[key]
	if !ok {
		return 0, false
	}
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	case string:
		cleaned := strings.NewReplacer("$", "", ",", "", "USD", "", "usd", "").Replace(strings.TrimSpace(typed))
		parsed, err := strconv.ParseFloat(cleaned, 64)
		return parsed, err == nil
	}
	return 0, false
}

func funcParamKeys(params ai.FuncParams) []string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

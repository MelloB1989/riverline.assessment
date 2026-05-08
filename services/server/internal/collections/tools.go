package collections

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
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
	createHandoffTool := ai.NewGoFunctionTool(
		agents.ToolCreateAriaHandoff,
		"Create and persist the ARIA assessment handoff after all required intake information and preferred callback timing are collected, or after stop-contact/hardship terminal handling.",
		ai.NewFuncParams().
			SetString("reason", "Brief reason ARIA is ready to hand off.").
			SetStringEnum("outcome", "Handoff outcome.", []string{"ready_for_nova", "stop_contact", "hardship"}).
			SetString("preferred_nova_call_at", "Borrower-confirmed preferred resolution-call time as an ISO-8601 timestamp with timezone. Required when outcome is ready_for_nova. Use not_applicable only for stop_contact or hardship terminal outcomes.").
			SetRequired("reason", "outcome", "preferred_nova_call_at"),
		func(_ context.Context, params ai.FuncParams) (string, error) {
			start := time.Now()
			log.Printf("[collections] aria tool start workflow=%s tool=%s param_keys=%v", wf.Id, agents.ToolCreateAriaHandoff, funcParamKeys(params))
			if wf.CurrentStage != models.AgentAria {
				log.Printf("[collections] aria tool skipped workflow=%s tool=%s reason=already_generated duration=%s", wf.Id, agents.ToolCreateAriaHandoff, time.Since(start))
				return `{"handoff_already_generated":true}`, nil
			}
			outcome, err := funcParamString(params, "outcome")
			if err != nil {
				toolErr = err
				return "", err
			}
			preferredCallAt, err := funcParamString(params, "preferred_nova_call_at")
			if err != nil {
				toolErr = err
				return "", err
			}
			if outcome == "ready_for_nova" {
				if _, err := parseBorrowerCallTime(preferredCallAt, time.Now().UTC()); err != nil {
					toolErr = fmt.Errorf("preferred_nova_call_at is required before handoff: %w", err)
					return "", toolErr
				}
			}
			// Use an isolated client so structured parsing cannot recursively reuse
			// the tool-enabled chat client that is currently executing this tool.
			results.AriaHandoff, toolErr = GenerateAriaHandoffWithClient(client.Clone(), wf, messages)
			if toolErr != nil {
				log.Printf("[collections] aria tool failed workflow=%s tool=%s duration=%s err=%v", wf.Id, agents.ToolCreateAriaHandoff, time.Since(start), toolErr)
				return "", toolErr
			}
			if outcome == "ready_for_nova" {
				results.AriaHandoff.Result.PreferredNovaCallAt = &preferredCallAt
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
	tools := []ai.GoFunctionTool{createHandoffTool, rescheduleTool}
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

func funcParamKeys(params ai.FuncParams) []string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

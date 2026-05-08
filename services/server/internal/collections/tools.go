package collections

import (
	"context"
	"errors"
	"fmt"
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
		resp, err := client.Converse(handoff, messages)
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
			if wf.CurrentStage != models.AgentAria {
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
			results.AriaHandoff, toolErr = GenerateAriaHandoffWithClient(client, wf, messages)
			if toolErr != nil {
				return "", toolErr
			}
			if outcome == "ready_for_nova" {
				results.AriaHandoff.Result.PreferredNovaCallAt = &preferredCallAt
			}
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
			scheduledText, err := funcParamString(params, "scheduled_call_at")
			if err != nil {
				toolErr = err
				return "", err
			}
			reason, err := funcParamString(params, "reason")
			if err != nil {
				toolErr = err
				return "", err
			}
			scheduledAt, err := parseBorrowerCallTime(scheduledText, time.Now().UTC())
			if err != nil {
				toolErr = err
				return "", err
			}
			if err := SetNovaScheduledCall(wf.Id, scheduledAt, reason); err != nil {
				toolErr = err
				return "", err
			}
			results.NovaReschedule = &NovaRescheduleResult{ScheduledCallAt: scheduledAt, Reason: reason}
			return `{"nova_call_rescheduled":true}`, nil
		},
	)
	tools := []ai.GoFunctionTool{createHandoffTool, rescheduleTool}
	resp, err := client.ConverseWithTools(handoff, messages, tools...)
	if toolErr != nil {
		return results, nil, toolErr
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

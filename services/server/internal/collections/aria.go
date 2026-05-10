package collections

import (
	"riverline_server/internal/models"
	"strings"
	"time"
)

func CompleteARIA(workflowID string) error {
	wf, err := GetWorkflow(workflowID)
	if err != nil {
		return err
	}
	conv, err := latestConversationForAgent(workflowID, models.AgentAria)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	wf.AriaAttempts += 1
	wf.UpdatedAt = now
	wf.HardshipFlagged = boolPtr(derefBool(wf.HardshipMentioned))
	simulated := workflowIsSimulated(wf)
	if !simulated && ariaTerminalOutcome(wf) {
		if wf.Outcome == nil {
			if derefBool(wf.StopContactFlagged) {
				wf.Outcome = outcomePtr(models.OutcomeStopContact)
			} else {
				wf.Outcome = outcomePtr(models.OutcomeHardship)
			}
		}
		wf.ResolvedAt = &now
	} else {
		wf.CurrentStage = models.AgentNova
		if simulated {
			wf.ResolvedAt = nil
		}
	}
	if wf.Outcome != nil {
		conv.Outcome = wf.Outcome
	} else {
		conv.Outcome = outcomePtr(models.OutcomeCommitted)
	}
	conv.EndedAt = &now
	if err := updateConversation(conv); err != nil {
		return err
	}
	if err := updateWorkflow(wf); err != nil {
		return err
	}
	return nil
}

func ApplyAriaHandoffForSimulation(wf *models.BorrowerWorkflow, result AriaHandoffResult) error {
	applyAriaHandoff(wf, result)
	if (!ariaTerminalOutcome(wf) || workflowIsSimulated(wf)) && result.PreferredNovaCallAt != nil && strings.TrimSpace(*result.PreferredNovaCallAt) != "" {
		if err := setInitialNovaSchedule(wf, result); err != nil {
			return err
		}
	}
	wf.UpdatedAt = time.Now().UTC()
	return updateWorkflow(wf)
}

func ForceAdvanceToNova(workflowID string) error {
	wf, err := GetWorkflow(workflowID)
	if err != nil {
		return err
	}
	if !workflowIsSimulated(wf) {
		return nil
	}
	wf.CurrentStage = models.AgentNova
	wf.ResolvedAt = nil
	wf.UpdatedAt = time.Now().UTC()
	return updateWorkflow(wf)
}

func workflowIsSimulated(wf *models.BorrowerWorkflow) bool {
	if wf == nil {
		return false
	}
	return strings.HasPrefix(wf.Id, "sim-wf-") || strings.HasPrefix(wf.UserId, "sim-user-")
}

func ariaTerminalOutcome(wf *models.BorrowerWorkflow) bool {
	if derefBool(wf.StopContactFlagged) || derefBool(wf.HardshipFlagged) || derefBool(wf.HardshipMentioned) {
		return true
	}
	if wf.Outcome == nil {
		return false
	}
	return *wf.Outcome == models.OutcomeStopContact || *wf.Outcome == models.OutcomeHardship
}

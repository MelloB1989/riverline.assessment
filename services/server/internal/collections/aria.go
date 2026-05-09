package collections

import (
	"riverline_server/internal/models"
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
	if ariaTerminalOutcome(wf) {
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
	if !ariaTerminalOutcome(wf) && result.PreferredNovaCallAt != nil && *result.PreferredNovaCallAt != "" {
		if err := setInitialNovaSchedule(wf, result); err != nil {
			return err
		}
	}
	wf.UpdatedAt = time.Now().UTC()
	return updateWorkflow(wf)
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

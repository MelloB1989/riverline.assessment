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
	if derefBool(wf.StopContactFlagged) || (wf.Outcome != nil && *wf.Outcome == models.OutcomeStopContact) {
		wf.Outcome = outcomePtr(models.OutcomeStopContact)
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

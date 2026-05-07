package collections

import (
	"errors"
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
	messages, err := ListMessages(conv.Id, workflowID)
	if err != nil {
		return err
	}
	handoff, err := GenerateHandoff(models.AgentAria, *wf, messages, "")
	if err != nil {
		return err
	}
	applyAssessmentHandoff(wf, handoff.Result)
	if !handoff.Result.StageComplete {
		return errors.New("aria assessment is incomplete")
	}
	now := time.Now().UTC()
	wf.AriaAttempts += 1
	wf.UpdatedAt = now
	wf.HardshipFlagged = boolPtr(derefBool(wf.HardshipMentioned))
	if derefBool(wf.StopContactFlagged) || (handoff.Result.Outcome != nil && *handoff.Result.Outcome == models.OutcomeStopContact) {
		wf.Outcome = outcomePtr(models.OutcomeStopContact)
		wf.ResolvedAt = &now
	} else {
		wf.CurrentStage = models.AgentNova
	}
	conv.Outcome = outcomePtr(models.OutcomeCommitted)
	conv.EndedAt = &now
	if err := updateConversation(conv); err != nil {
		return err
	}
	if err := updateWorkflow(wf); err != nil {
		return err
	}
	agentID := models.AgentAria
	return LogCost("summarization", &agentID, "karma-llama3.3-70b", handoff.Tokens, 0, &conv.Id, nil)
}

package collections

import (
	"riverline_server/internal/models"
	"time"
)

func CompleteDELTA(workflowID string) error {
	wf, err := GetWorkflow(workflowID)
	if err != nil {
		return err
	}
	conv, err := latestConversationForAgent(workflowID, models.AgentDelta)
	if err != nil {
		return err
	}
	messages, err := ListMessages(conv.Id, workflowID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	handoff, err := GenerateHandoff(models.AgentDelta, *wf, messages, "")
	if err != nil {
		return err
	}
	outcome := models.OutcomeEscalated
	if handoff.Result.Outcome != nil {
		outcome = *handoff.Result.Outcome
	}
	if outcome == models.OutcomeCommitted {
		wf.ResolvedAt = &now
	}
	applyDeltaHandoff(wf, handoff.Result)
	wf.Outcome = &outcome
	wf.UpdatedAt = now
	conv.Outcome = &outcome
	conv.EndedAt = &now
	if err := updateConversation(conv); err != nil {
		return err
	}
	if err := updateWorkflow(wf); err != nil {
		return err
	}
	agentID := models.AgentDelta
	return LogCost("summarization", &agentID, "karma-llama3.3-70b", handoff.Tokens, 0, &conv.Id, nil)
}

func SendDELTAFinalOfferEmail(workflowID string) error {
	return sendOfferEmail(workflowID, true)
}

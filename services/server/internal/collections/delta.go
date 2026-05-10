package collections

import (
	"time"

	"riverline_server/internal/agents"
	"riverline_server/internal/models"

	"github.com/MelloB1989/karma/v2/orm"
)

func SendDELTAFinalOfferEmail(workflowID string) error {
	return sendOfferEmail(workflowID, true)
}

func CompleteDeltaConversation(workflowID, conversationID string, client *agents.Client) (models.Outcome, error) {
	wf, err := GetWorkflow(workflowID)
	if err != nil {
		return "", err
	}
	messages, err := ListMessages(conversationID, workflowID)
	if err != nil {
		return "", err
	}
	handoff, err := GenerateDeltaHandoffWithClient(client, *wf, messages)
	if err != nil {
		return "", err
	}
	applyDeltaHandoff(wf, handoff.Result)
	if handoff.Result.Outcome == nil {
		outcome := models.OutcomeEscalated
		wf.Outcome = &outcome
	}
	now := time.Now().UTC()
	wf.ResolvedAt = &now
	wf.UpdatedAt = now
	if offer, err := firstOffer(wf.Id); err == nil {
		applyDeltaOfferOutcome(offer, handoff.Result)
		o := orm.Load(&models.ResolutionOffer{})
		defer o.Close()
		if err := o.Update(offer, offer.Id); err != nil {
			return "", err
		}
	}
	if err := updateWorkflow(wf); err != nil {
		return "", err
	}
	if err := finalizeWorkflowOutcome(wf); err != nil {
		return "", err
	}
	conv, err := getConversationByID(conversationID)
	if err != nil {
		return "", err
	}
	conv.TotalTurns = intPtr(countBorrowerTurns(messages))
	conv.TotalTokensUsed = intPtr(totalMessageTokens(messages) + handoff.Tokens)
	conv.Outcome = wf.Outcome
	conv.EndedAt = &now
	if err := updateConversation(conv); err != nil {
		return "", err
	}
	agentID := models.AgentDelta
	if err := LogCost("summarization", &agentID, handoff.ModelUsed, handoff.Tokens, 0, &conversationID, nil); err != nil {
		return "", err
	}
	if wf.Outcome == nil {
		return models.OutcomeEscalated, nil
	}
	return *wf.Outcome, nil
}

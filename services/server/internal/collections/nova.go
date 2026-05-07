package collections

import (
	"riverline_server/internal/models"
	"time"

	"github.com/MelloB1989/karma/utils"
	"github.com/MelloB1989/karma/v2/orm"
)

func PrepareNOVA(workflowID string) (*models.ResolutionOffer, error) {
	wf, err := GetWorkflow(workflowID)
	if err != nil {
		return nil, err
	}
	handoff, err := GenerateHandoff(models.AgentNova, *wf, nil, "")
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	offer := &models.ResolutionOffer{
		Id:             utils.GenerateID(),
		WorkflowId:     workflowID,
		CandidateOffer: map[string]any{},
		CreatedAt:      now,
	}
	applyNovaHandoff(wf, offer, handoff.Result)
	if offer.EmiStartDate == nil {
		offer.EmiStartDate = timePtr(now.Add(7 * 24 * time.Hour))
	}
	o := orm.Load(&models.ResolutionOffer{})
	defer o.Close()
	var existing []models.ResolutionOffer
	if err := o.GetByFieldEquals("WorkflowId", workflowID).Scan(&existing); err == nil && len(existing) > 0 {
		existing[0].CandidateOffer = offer.CandidateOffer
		existing[0].LumpSumOffered = offer.LumpSumOffered
		existing[0].LumpSumDiscountPct = offer.LumpSumDiscountPct
		existing[0].EmiAmount = offer.EmiAmount
		existing[0].EmiMonths = offer.EmiMonths
		existing[0].EmiStartDate = offer.EmiStartDate
		existing[0].HardshipOffered = offer.HardshipOffered
		if err := o.Update(&existing[0], existing[0].Id); err != nil {
			return nil, err
		}
		agentID := models.AgentNova
		_ = LogCost("summarization", &agentID, "karma-llama3.3-70b", handoff.Tokens, 0, nil, nil)
		return &existing[0], nil
	}
	if err := o.Insert(offer); err != nil {
		return nil, err
	}
	agentID := models.AgentNova
	_ = LogCost("summarization", &agentID, "karma-llama3.3-70b", handoff.Tokens, 0, nil, nil)
	return offer, nil
}

func MarkNOVAStarted(workflowID, callID string, promptVersion int, handoff string) error {
	offer, err := firstOffer(workflowID)
	if err != nil {
		return err
	}
	if callID != "" {
		offer.VapiCallId = &callID
	}
	o := orm.Load(&models.ResolutionOffer{})
	defer o.Close()
	if err := o.Update(offer, offer.Id); err != nil {
		return err
	}
	wf, err := GetWorkflow(workflowID)
	if err != nil {
		return err
	}
	conv, err := getOrCreateConversation(*wf, models.AgentNova, promptVersion)
	if err != nil {
		return err
	}
	msg := models.AgentMessage{
		Id:             utils.GenerateID(),
		ConversationId: conv.Id,
		WorkflowId:     workflowID,
		AgentId:        models.AgentNova,
		Role:           models.MessageRoleAgent,
		Content:        "NOVA outbound call started with handoff: " + handoff,
		CreatedAt:      time.Now().UTC(),
	}
	msgOrm := orm.Load(&models.AgentMessage{})
	defer msgOrm.Close()
	if err := msgOrm.Insert(&msg); err != nil {
		return err
	}
	messages, err := ListMessages(conv.Id, workflowID)
	if err != nil {
		return err
	}
	conv.TotalTokensUsed = intPtr(totalMessageTokens(messages))
	return updateConversation(&conv)
}

func CompleteNOVA(workflowID, callID, transcript, recordingURL string, durationSeconds *int) (models.Outcome, error) {
	wf, err := GetWorkflow(workflowID)
	if err != nil {
		return "", err
	}
	offer, err := firstOffer(workflowID)
	if err != nil {
		offer, err = PrepareNOVA(workflowID)
		if err != nil {
			return "", err
		}
	}
	handoff, err := GenerateHandoff(models.AgentNova, *wf, nil, transcript)
	if err != nil {
		return "", err
	}
	if callID != "" {
		offer.VapiCallId = &callID
	}
	if transcript != "" {
		offer.CallTranscript = &transcript
	}
	if recordingURL != "" {
		offer.CallRecordingUrl = &recordingURL
	}
	offer.CallDurationSeconds = durationSeconds
	applyNovaHandoff(wf, offer, handoff.Result)
	o := orm.Load(&models.ResolutionOffer{})
	defer o.Close()
	if err := o.Update(offer, offer.Id); err != nil {
		return "", err
	}
	accepted := derefBool(offer.OfferAccepted)
	if transcript != "" {
		_ = appendNOVACompletedMessage(workflowID, transcript, accepted)
	}
	now := time.Now().UTC()
	outcome := models.OutcomeRejected
	if handoff.Result.Outcome != nil {
		outcome = *handoff.Result.Outcome
	} else if accepted {
		outcome = models.OutcomeCommitted
	}
	if outcome == models.OutcomeCommitted {
		wf.Outcome = &outcome
		wf.ResolvedAt = &now
	} else {
		wf.CurrentStage = models.AgentDelta
		if wf.FinalOfferDeadline == nil {
			deadline := now.Add(48 * time.Hour)
			wf.FinalOfferDeadline = &deadline
		}
	}
	wf.UpdatedAt = now
	if err := updateWorkflow(wf); err != nil {
		return "", err
	}
	agentID := models.AgentNova
	return outcome, LogCost("summarization", &agentID, "karma-llama3.3-70b", handoff.Tokens, 0, nil, nil)
}

func SendNOVAOfferEmail(workflowID string) error {
	return sendOfferEmail(workflowID, false)
}

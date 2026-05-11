package collections

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"riverline_server/internal/agents"
	"riverline_server/internal/models"

	"github.com/MelloB1989/karma/utils"
	"github.com/MelloB1989/karma/v2/orm"
)

func PrepareNOVA(workflowID string) (*models.ResolutionOffer, error) {
	client, err := agents.NewNova()
	if err != nil {
		return nil, err
	}
	return PrepareNOVAWithClient(workflowID, client)
}

func PrepareNOVAWithClient(workflowID string, client *agents.Client) (*models.ResolutionOffer, error) {
	wf, err := GetWorkflow(workflowID)
	if err != nil {
		return nil, err
	}
	o := orm.Load(&models.ResolutionOffer{})
	defer o.Close()
	var existing []models.ResolutionOffer
	if err := o.GetByFieldEquals("WorkflowId", workflowID).Scan(&existing); err != nil {
		return nil, err
	}
	if len(existing) == 0 || existing[0].ScheduledCallAt == nil {
		return nil, errors.New("resolution offer schedule missing before NOVA preparation")
	}
	strippedScheduleMetadata := stripNovaScheduleMetadata(&existing[0])
	if novaOfferHasRuntimeTerms(&existing[0]) && novaRuntimeContextHasOfferTerms(derefString(wf.ContextForNova), &existing[0]) {
		if strippedScheduleMetadata {
			if err := o.Update(&existing[0], existing[0].Id); err != nil {
				return nil, err
			}
		}
		return &existing[0], nil
	}
	handoff, err := GenerateNovaOfferWithClient(client, *wf)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	offer := &models.ResolutionOffer{
		Id:             utils.GenerateID(),
		WorkflowId:     workflowID,
		CandidateOffer: map[string]any{},
		Status:         models.OfferStatusProposed,
		CreatedAt:      now,
	}
	applyNovaOffer(offer, handoff.Result)
	if offer.EmiStartDate == nil {
		offer.EmiStartDate = timePtr(now.Add(7 * 24 * time.Hour))
	}
	enrichNovaCandidateOffer(offer)
	if !novaOfferHasRuntimeTerms(offer) {
		applyFallbackNovaOffer(offer, wf, now)
	}
	runtimeContext, err := GenerateNovaRuntimeContextWithClient(client, *wf, offer)
	if err != nil {
		return nil, err
	}
	wf.ContextForNova = stringPtr(runtimeContext.Result)
	wf.UpdatedAt = now
	if err := updateWorkflow(wf); err != nil {
		return nil, err
	}
	if existing[0].Status == "" {
		existing[0].Status = models.OfferStatusProposed
	}
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
	_ = LogCost("summarization", &agentID, handoff.ModelUsed, handoff.Tokens, 0, nil, nil)
	_ = LogCost("summarization", &agentID, runtimeContext.ModelUsed, runtimeContext.Tokens, 0, nil, nil)
	return &existing[0], nil
}

func stripNovaScheduleMetadata(offer *models.ResolutionOffer) bool {
	if offer == nil || offer.CandidateOffer == nil {
		return false
	}
	_, hadScheduledAt := offer.CandidateOffer["scheduled_call_at"]
	_, hadScheduleReason := offer.CandidateOffer["schedule_reason"]
	delete(offer.CandidateOffer, "scheduled_call_at")
	delete(offer.CandidateOffer, "schedule_reason")
	return hadScheduledAt || hadScheduleReason
}

func novaOfferHasRuntimeTerms(offer *models.ResolutionOffer) bool {
	if offer == nil {
		return false
	}
	if offer.LumpSumOffered != nil && *offer.LumpSumOffered > 0 {
		return true
	}
	if offer.EmiAmount != nil && *offer.EmiAmount > 0 && offer.EmiMonths != nil && *offer.EmiMonths > 0 {
		return true
	}
	if offer.HardshipOffered != nil && *offer.HardshipOffered {
		return true
	}
	return false
}

func novaRuntimeContextHasOfferTerms(context string, offer *models.ResolutionOffer) bool {
	context = strings.ToLower(strings.TrimSpace(context))
	if context == "" || offer == nil {
		return false
	}
	if offer.LumpSumOffered != nil && *offer.LumpSumOffered > 0 && !contextContainsAmount(context, *offer.LumpSumOffered) {
		return false
	}
	if offer.EmiAmount != nil && *offer.EmiAmount > 0 && !contextContainsAmount(context, *offer.EmiAmount) {
		return false
	}
	if offer.EmiMonths != nil && *offer.EmiMonths > 0 && !strings.Contains(context, fmt.Sprintf("%d", *offer.EmiMonths)) {
		return false
	}
	if offer.HardshipOffered != nil && *offer.HardshipOffered && !strings.Contains(context, "hardship") {
		return false
	}
	return true
}

func contextContainsAmount(context string, amount float64) bool {
	exact := fmt.Sprintf("%.2f", amount)
	trimmed := strings.TrimRight(strings.TrimRight(exact, "0"), ".")
	whole := fmt.Sprintf("%.0f", amount)
	compact := strings.ReplaceAll(exact, ".", "")
	normalized := strings.NewReplacer(",", "", ".", "", "$", "", "₹", "", " ", "").Replace(context)
	return strings.Contains(context, exact) || strings.Contains(context, trimmed) || strings.Contains(context, whole) || strings.Contains(normalized, compact)
}

func applyFallbackNovaOffer(offer *models.ResolutionOffer, wf *models.BorrowerWorkflow, now time.Time) {
	if offer == nil || wf == nil {
		return
	}
	loan, err := GetLoan(wf.LoanId)
	if err != nil || loan == nil || loan.OutstandingAmount <= 0 {
		hardship := true
		offer.HardshipOffered = &hardship
		offer.CandidateOffer = map[string]any{
			"hardship_offered": true,
			"status":           "hardship_review_pending",
		}
		return
	}
	discountPct := loan.PolicyMaxDiscountPct
	if discountPct < 0 {
		discountPct = 0
	}
	if discountPct > 100 {
		discountPct = 100
	}
	lumpSum := roundMoney(loan.OutstandingAmount * (1 - discountPct/100))
	if lumpSum <= 0 {
		lumpSum = roundMoney(loan.OutstandingAmount)
		discountPct = 0
	}
	emiMonths := 6
	emiAmount := roundMoney(loan.OutstandingAmount / float64(emiMonths))
	emiStart := now.Add(7 * 24 * time.Hour)
	hardship := false
	offer.LumpSumOffered = &lumpSum
	offer.LumpSumDiscountPct = &discountPct
	offer.EmiAmount = &emiAmount
	offer.EmiMonths = &emiMonths
	offer.EmiStartDate = &emiStart
	offer.HardshipOffered = &hardship
	offer.CandidateOffer = map[string]any{
		"primary_option": map[string]any{
			"type":         "lump_sum",
			"amount":       lumpSum,
			"discount_pct": discountPct,
			"deadline":     now.Add(48 * time.Hour).Format(time.RFC3339),
		},
		"secondary_option": map[string]any{
			"type":           "emi",
			"monthly_amount": emiAmount,
			"months":         emiMonths,
			"start_date":     emiStart.Format("2006-01-02"),
		},
		"lump_sum_offered":      lumpSum,
		"lump_sum_discount_pct": discountPct,
		"emi_amount":            emiAmount,
		"emi_months":            emiMonths,
		"emi_start_date":        emiStart.Format("2006-01-02"),
		"hardship_offered":      hardship,
	}
}

func enrichNovaCandidateOffer(offer *models.ResolutionOffer) {
	if offer == nil {
		return
	}
	if offer.CandidateOffer == nil {
		offer.CandidateOffer = map[string]any{}
	}
	stripNovaScheduleMetadata(offer)
	if offer.LumpSumOffered != nil && *offer.LumpSumOffered > 0 {
		offer.CandidateOffer["lump_sum_offered"] = *offer.LumpSumOffered
		if offer.LumpSumDiscountPct != nil {
			offer.CandidateOffer["lump_sum_discount_pct"] = *offer.LumpSumDiscountPct
		}
		deadline := time.Now().UTC().Add(48 * time.Hour)
		if offer.ScheduledCallAt != nil {
			deadline = offer.ScheduledCallAt.UTC().Add(48 * time.Hour)
		}
		if option, ok := offer.CandidateOffer["primary_option"].(map[string]any); ok {
			option["type"] = "lump_sum"
			option["amount"] = *offer.LumpSumOffered
			option["deadline"] = deadline.Format(time.RFC3339)
			if offer.LumpSumDiscountPct != nil {
				option["discount_pct"] = *offer.LumpSumDiscountPct
			}
		} else {
			offer.CandidateOffer["primary_option"] = map[string]any{
				"type":     "lump_sum",
				"amount":   *offer.LumpSumOffered,
				"deadline": deadline.Format(time.RFC3339),
			}
			if offer.LumpSumDiscountPct != nil {
				offer.CandidateOffer["primary_option"].(map[string]any)["discount_pct"] = *offer.LumpSumDiscountPct
			}
		}
	}
	if offer.EmiAmount != nil && *offer.EmiAmount > 0 && offer.EmiMonths != nil && *offer.EmiMonths > 0 {
		offer.CandidateOffer["emi_amount"] = *offer.EmiAmount
		offer.CandidateOffer["emi_months"] = *offer.EmiMonths
		if offer.EmiStartDate != nil {
			offer.CandidateOffer["emi_start_date"] = offer.EmiStartDate.Format("2006-01-02")
		}
		if option, ok := offer.CandidateOffer["secondary_option"].(map[string]any); ok {
			option["type"] = "emi"
			option["monthly_amount"] = *offer.EmiAmount
			option["months"] = *offer.EmiMonths
			if offer.EmiStartDate != nil {
				option["start_date"] = offer.EmiStartDate.Format("2006-01-02")
			}
		} else {
			option := map[string]any{
				"type":           "emi",
				"monthly_amount": *offer.EmiAmount,
				"months":         *offer.EmiMonths,
			}
			if offer.EmiStartDate != nil {
				option["start_date"] = offer.EmiStartDate.Format("2006-01-02")
			}
			offer.CandidateOffer["secondary_option"] = option
		}
	}
	if offer.HardshipOffered != nil {
		offer.CandidateOffer["hardship_offered"] = *offer.HardshipOffered
	}
}

func roundMoney(amount float64) float64 {
	return math.Round(amount*100) / 100
}

func SetNovaScheduledCall(workflowID string, scheduledAt time.Time, reason string) error {
	if scheduledAt.IsZero() {
		return errors.New("scheduled call time is required")
	}
	now := time.Now().UTC()
	minTime := now.Add(2 * time.Minute)
	if scheduledAt.Before(minTime) {
		scheduledAt = minTime
	}
	offer, err := firstOffer(workflowID)
	insert := false
	if err != nil {
		offer = &models.ResolutionOffer{
			Id:             utils.GenerateID(),
			WorkflowId:     workflowID,
			CandidateOffer: map[string]any{},
			Status:         models.OfferStatusProposed,
			CreatedAt:      now,
		}
		insert = true
	}
	offer.ScheduledCallAt = &scheduledAt
	if offer.CandidateOffer == nil {
		offer.CandidateOffer = map[string]any{}
	}
	stripNovaScheduleMetadata(offer)
	if novaOfferHasRuntimeTerms(offer) {
		enrichNovaCandidateOffer(offer)
	}
	o := orm.Load(&models.ResolutionOffer{})
	defer o.Close()
	if insert {
		if err := o.Insert(offer); err != nil {
			return err
		}
	} else if err := o.Update(offer, offer.Id); err != nil {
		return err
	}
	wf, err := GetWorkflow(workflowID)
	if err != nil {
		return err
	}
	merged := strings.TrimSpace(derefString(wf.AriaSummary) + "\n" + novaScheduleSummaryLine(scheduledAt, reason))
	wf.AriaSummary = stringPtr(merged)
	wf.UpdatedAt = now
	return updateWorkflow(wf)
}

func GetNovaScheduledCallAt(workflowID string) (time.Time, error) {
	offer, err := firstOffer(workflowID)
	if err != nil || offer.ScheduledCallAt == nil {
		return time.Now().UTC(), nil
	}
	return offer.ScheduledCallAt.UTC(), nil
}

func setInitialNovaSchedule(wf *models.BorrowerWorkflow, result AriaHandoffResult) error {
	if result.PreferredNovaCallAt == nil || strings.TrimSpace(*result.PreferredNovaCallAt) == "" {
		return errors.New("ARIA handoff missing preferred NOVA call time")
	}
	scheduledAt, err := parseBorrowerCallTime(*result.PreferredNovaCallAt, time.Now().UTC())
	if err != nil {
		return err
	}
	reason := "Initial preferred NOVA call time from ARIA intake"
	if err := SetNovaScheduledCall(wf.Id, scheduledAt, reason); err != nil {
		return err
	}
	wf.AriaSummary = stringPtr(strings.TrimSpace(derefString(wf.AriaSummary) + "\n" + novaScheduleSummaryLine(scheduledAt, reason)))
	return nil
}

func novaScheduleSummaryLine(scheduledAt time.Time, reason string) string {
	line := fmt.Sprintf("NOVA call is scheduled for %s.", scheduledAt.In(collectionsISTLocation()).Format("January 2, 2006 15:04 MST"))
	if strings.TrimSpace(reason) != "" {
		line += " Schedule note: " + strings.TrimSpace(reason) + "."
	}
	return line
}

func parseBorrowerCallTime(value string, now time.Time) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, errors.New("scheduled call time is empty")
	}
	layoutsUTC := []string{time.RFC3339, time.RFC3339Nano, "2006-01-02T15:04Z07:00", "2006-01-02 15:04:05 MST", "2006-01-02 15:04 MST"}
	for _, layout := range layoutsUTC {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), nil
		}
	}
	loc := collectionsISTLocation()
	layoutsInIST := []string{"2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"}
	for _, layout := range layoutsInIST {
		if parsed, err := time.ParseInLocation(layout, value, loc); err == nil {
			if layout == "2006-01-02" {
				parsed = time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 10, 0, 0, 0, loc)
			}
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("could not parse scheduled call time %q relative to %s", value, now.Format(time.RFC3339))
}

func collectionsISTLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err == nil {
		return loc
	}
	return time.FixedZone("IST", 5*60*60+30*60)
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

func CompleteNOVA(workflowID, callID, transcript, recordingURL string, durationSeconds *int, structuredOutput map[string]any) (models.Outcome, error) {
	novaClient, err := agents.NewNova()
	if err != nil {
		return "", err
	}
	deltaClient, err := agents.NewDelta()
	if err != nil {
		return "", err
	}
	return CompleteNOVAWithClients(workflowID, callID, transcript, recordingURL, durationSeconds, structuredOutput, novaClient, deltaClient)
}

func CompleteNOVAWithClients(workflowID, callID, transcript, recordingURL string, durationSeconds *int, structuredOutput map[string]any, novaClient *agents.Client, deltaClient *agents.Client) (models.Outcome, error) {
	wf, err := GetWorkflow(workflowID)
	if err != nil {
		return "", err
	}
	offer, err := firstOffer(workflowID)
	if err != nil {
		offer, err = PrepareNOVAWithClient(workflowID, novaClient)
		if err != nil {
			return "", err
		}
	}
	handoff, err := NovaCallHandoffFromStructuredOutput(structuredOutput)
	if err != nil {
		return "", err
	}
	if handoff == nil {
		handoff, err = GenerateNovaCallHandoffWithClient(novaClient, *wf, offer, transcript)
		if err != nil {
			return "", err
		}
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
	applyNovaCallHandoff(wf, offer, handoff.Result)
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
		applyDeltaHandoffFromNova(wf, offer, handoff.Result)
		wf.Outcome = &outcome
		wf.ResolvedAt = &now
	} else {
		wf.CurrentStage = models.AgentDelta
		if wf.FinalOfferDeadline == nil {
			deadline := now.Add(48 * time.Hour)
			wf.FinalOfferDeadline = &deadline
		}
		deltaRuntime, err := GenerateDeltaRuntimeContextWithClient(deltaClient, handoff.Result, offer, *wf)
		if err != nil {
			return "", err
		}
		applyDeltaRuntimeContext(wf, deltaRuntime.Result)
		deltaHandoff, err := GenerateDeltaHandoffWithClient(deltaClient, *wf, nil)
		if err != nil {
			return "", err
		}
		applyDeltaDraftHandoff(wf, deltaHandoff.Result)
		agentID := models.AgentDelta
		if err := LogCost("summarization", &agentID, deltaRuntime.ModelUsed, deltaRuntime.Tokens, 0, nil, nil); err != nil {
			return "", err
		}
		if err := LogCost("summarization", &agentID, deltaHandoff.ModelUsed, deltaHandoff.Tokens, 0, nil, nil); err != nil {
			return "", err
		}
	}
	wf.UpdatedAt = now
	if err := updateWorkflow(wf); err != nil {
		return "", err
	}
	agentID := models.AgentNova
	return outcome, LogCost("summarization", &agentID, handoff.ModelUsed, handoff.Tokens, 0, nil, nil)
}

func SendNOVAOfferEmail(workflowID string) error {
	return sendOfferEmail(workflowID, false)
}

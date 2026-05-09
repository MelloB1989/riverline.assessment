package collections

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"riverline_server/internal/agents"
	"riverline_server/internal/models"
)

type HandoffCall[T any] struct {
	Result    T
	Tokens    int
	ModelUsed string
}

type AriaHandoffResult struct {
	IdentityVerified       *bool           `json:"identity_verified"`
	EmploymentStatus       *string         `json:"employment_status"`
	MonthlyIncomeRange     *string         `json:"monthly_income_range"`
	MonthlyObligations     *float64        `json:"monthly_obligations"`
	DefaultReason          *string         `json:"default_reason"`
	BorrowerEmotionalState *models.Persona `json:"borrower_emotional_state"`
	HardshipMentioned      *bool           `json:"hardship_mentioned"`
	StopContactFlagged     *bool           `json:"stop_contact_flagged"`
	Outcome                *models.Outcome `json:"outcome"`
	AriaSummary            string          `json:"aria_summary"`
	ContextForNova         string          `json:"context_for_nova"`
	PreferredNovaCallAt    *string         `json:"preferred_nova_call_at"`
}

type NovaOfferResult struct {
	CandidateOffer     map[string]any `json:"candidate_offer"`
	LumpSumOffered     *float64       `json:"lump_sum_offered"`
	LumpSumDiscountPct *float64       `json:"lump_sum_discount_pct"`
	EmiAmount          *float64       `json:"emi_amount"`
	EmiMonths          *int           `json:"emi_months"`
	HardshipOffered    *bool          `json:"hardship_offered"`
	ContextForNova     string         `json:"context_for_nova"`
}

type NovaCallHandoffResult struct {
	OfferAccepted           *bool           `json:"offer_accepted"`
	AcceptedOfferType       *string         `json:"accepted_offer_type"`
	ObjectionsRaised        []string        `json:"objections_raised"`
	Outcome                 *models.Outcome `json:"outcome"`
	AriaSummary             string          `json:"aria_summary"`
	FinalOfferAmount        *float64        `json:"final_offer_amount"`
	FinalOfferDeadlineHours *int            `json:"final_offer_deadline_hours"`
}

type DeltaHandoffResult struct {
	StageComplete           bool            `json:"stage_complete"`
	Outcome                 *models.Outcome `json:"outcome"`
	DeltaSummary            string          `json:"delta_summary"`
	FinalOfferAmount        *float64        `json:"final_offer_amount"`
	FinalOfferDeadlineHours *int            `json:"final_offer_deadline_hours"`
}

type DeltaRuntimeContextResult struct {
	ContextForDelta string `json:"context_for_delta"`
}

func GenerateAriaHandoff(wf models.BorrowerWorkflow, messages []models.AgentMessage) (*HandoffCall[AriaHandoffResult], error) {
	client, err := agents.NewAria()
	if err != nil {
		return nil, err
	}
	return GenerateAriaHandoffWithClient(client, wf, messages)
}

func GenerateAriaHandoffWithClient(client *agents.Client, wf models.BorrowerWorkflow, messages []models.AgentMessage) (*HandoffCall[AriaHandoffResult], error) {
	start := time.Now()
	log.Printf("[collections] handoff generation start workflow=%s agent=%s messages=%d", wf.Id, models.AgentAria, len(messages))
	user, _ := GetUser(wf.UserId)
	loan, _ := GetLoan(wf.LoanId)
	accountSummary := ""
	if user != nil && loan != nil {
		accountSummary = borrowerAccountSummaryFromRecords(*user, *loan)
	}
	payload := map[string]any{
		"account_summary":  accountSummary,
		"assessment_state": conciseAssessmentState(wf),
		"messages":         agents.MessagesForCompletion(messages),
	}
	prompt := buildStructuredPrompt("ARIA assessment handoff", payload, `{
  "identity_verified": boolean|null,
  "employment_status": string|null,
  "monthly_income_range": string|null,
  "monthly_obligations": number|null,
  "default_reason": string|null,
  "borrower_emotional_state": "cooperative"|"combative"|"evasive"|"distressed"|"confused"|null,
  "hardship_mentioned": boolean|null,
  "stop_contact_flagged": boolean|null,
  "outcome": "committed"|"rejected"|"no_response"|"hardship"|"stop_contact"|"escalated"|null,
  "aria_summary": string,
  "context_for_nova": string,
  "preferred_nova_call_at": string|null
}`, `Use only the ARIA chat messages plus user and loan facts. Produce the assessment fields ARIA collected and a <= 500 token context for NOVA. For a normal ready-for-call handoff, preferred_nova_call_at must be the borrower-confirmed resolution-call time as an ISO-8601 timestamp with timezone. Use null only for stop-contact or hardship terminal outcomes. Do not compute or offer repayment terms.`)
	var result AriaHandoffResult
	tokens, err := client.ParseHandoff(prompt, &result)
	if err != nil {
		log.Printf("[collections] handoff generation failed workflow=%s agent=%s duration=%s err=%v", wf.Id, models.AgentAria, time.Since(start), err)
		return nil, fmt.Errorf("parse aria handoff: %w", err)
	}
	log.Printf("[collections] handoff generation done workflow=%s agent=%s tokens=%d duration=%s", wf.Id, models.AgentAria, tokens, time.Since(start))
	return &HandoffCall[AriaHandoffResult]{Result: result, Tokens: tokens, ModelUsed: client.ModelUsed()}, nil
}

func GenerateNovaOffer(wf models.BorrowerWorkflow) (*HandoffCall[NovaOfferResult], error) {
	client, err := agents.NewNova()
	if err != nil {
		return nil, err
	}
	return GenerateNovaOfferWithClient(client, wf)
}

func GenerateNovaOfferWithClient(client *agents.Client, wf models.BorrowerWorkflow) (*HandoffCall[NovaOfferResult], error) {
	start := time.Now()
	log.Printf("[collections] handoff generation start workflow=%s agent=%s type=offer", wf.Id, models.AgentNova)
	loan, _ := GetLoan(wf.LoanId)
	payload := map[string]any{
		"aria_handoff": derefString(wf.ContextForNova),
		"aria_summary": derefString(wf.AriaSummary),
	}
	if loan != nil {
		payload["offer_policy"] = map[string]any{
			"outstanding_amount":      loan.OutstandingAmount,
			"policy_max_discount_pct": loan.PolicyMaxDiscountPct,
			"days_overdue":            loan.DaysOverdue,
		}
	}
	prompt := buildStructuredPrompt("NOVA offer generation", payload, `{
  "candidate_offer": object,
  "lump_sum_offered": number|null,
  "lump_sum_discount_pct": number|null,
  "emi_amount": number|null,
  "emi_months": number|null,
  "hardship_offered": boolean|null
}`, `Generate only the exact offer NOVA should present from ARIA context, loan facts, and policy. The candidate_offer must include a primary lump-sum option when policy allows it and a fallback EMI option when feasible. Populate exact amounts, discount percent, EMI amount, months, and hardship eligibility from the provided loan facts and policy. Do not generate runtime context and do not mark a call outcome.`)
	var result NovaOfferResult
	tokens, err := client.ParseHandoff(prompt, &result)
	if err != nil {
		log.Printf("[collections] handoff generation failed workflow=%s agent=%s type=offer duration=%s err=%v", wf.Id, models.AgentNova, time.Since(start), err)
		return nil, fmt.Errorf("parse nova offer: %w", err)
	}
	log.Printf("[collections] handoff generation done workflow=%s agent=%s type=offer tokens=%d duration=%s", wf.Id, models.AgentNova, tokens, time.Since(start))
	return &HandoffCall[NovaOfferResult]{Result: result, Tokens: tokens, ModelUsed: client.ModelUsed()}, nil
}

func GenerateNovaRuntimeContext(wf models.BorrowerWorkflow, offer *models.ResolutionOffer) (*HandoffCall[string], error) {
	client, err := agents.NewNova()
	if err != nil {
		return nil, err
	}
	return GenerateNovaRuntimeContextWithClient(client, wf, offer)
}

func GenerateNovaRuntimeContextWithClient(client *agents.Client, wf models.BorrowerWorkflow, offer *models.ResolutionOffer) (*HandoffCall[string], error) {
	start := time.Now()
	log.Printf("[collections] handoff generation start workflow=%s agent=%s type=runtime_context", wf.Id, models.AgentNova)
	payload := map[string]any{
		"aria_handoff":     derefString(wf.ContextForNova),
		"aria_summary":     derefString(wf.AriaSummary),
		"resolution_offer": conciseOfferState(offer),
	}
	prompt := buildRuntimeSummaryPrompt("NOVA runtime context", payload, "Generate only the <= 500 token voice-call context NOVA needs from ARIA's handoff and NOVA's generated offer. Include borrower/account identifiers only as partial identifiers, ARIA assessment facts, scheduled callback timing if relevant, and exact offer terms. Write the offer terms as call-ready instructions with a primary offer and fallback option where available, including exact amounts, deadlines or start dates, and the commitment question. Do not include raw JSON or unrelated workflow fields.")
	resp, err := client.GenerateTextWithTemporarySystem("You are NOVA's internal runtime-context summarizer. Generate concise call context for the voice assistant from provided JSON. Do not speak to the borrower. Return only plain text.", prompt, 6)
	if err != nil {
		contextForNova := fallbackNovaRuntimeContext(wf, offer)
		log.Printf("[collections] handoff generation fallback workflow=%s agent=%s type=runtime_context duration=%s err=%v", wf.Id, models.AgentNova, time.Since(start), err)
		return &HandoffCall[string]{Result: contextForNova, Tokens: 0, ModelUsed: "deterministic/fallback"}, nil
	}
	contextForNova := sanitizeRuntimeSummary(resp.AIResponse)
	if contextForNova == "" {
		return nil, fmt.Errorf("generate nova runtime context: empty model response")
	}
	tokens := resp.InputTokens + resp.OutputTokens
	log.Printf("[collections] handoff generation done workflow=%s agent=%s type=runtime_context tokens=%d context_chars=%d duration=%s", wf.Id, models.AgentNova, tokens, len(contextForNova), time.Since(start))
	return &HandoffCall[string]{Result: contextForNova, Tokens: tokens, ModelUsed: client.ModelUsed()}, nil
}

func GenerateNovaCallHandoff(wf models.BorrowerWorkflow, offer *models.ResolutionOffer, transcript string) (*HandoffCall[NovaCallHandoffResult], error) {
	client, err := agents.NewNova()
	if err != nil {
		return nil, err
	}
	return GenerateNovaCallHandoffWithClient(client, wf, offer, transcript)
}

func GenerateNovaCallHandoffWithClient(client *agents.Client, wf models.BorrowerWorkflow, offer *models.ResolutionOffer, transcript string) (*HandoffCall[NovaCallHandoffResult], error) {
	start := time.Now()
	log.Printf("[collections] handoff generation start workflow=%s agent=%s type=call_completion transcript_chars=%d", wf.Id, models.AgentNova, len(transcript))
	payload := map[string]any{
		"nova_context":     derefString(wf.ContextForNova),
		"resolution_offer": conciseOfferState(offer),
		"call_transcript":  transcript,
	}
	prompt := buildStructuredPrompt("NOVA call completion handoff", payload, `{
  "offer_accepted": boolean|null,
  "accepted_offer_type": string|null,
  "objections_raised": array,
  "outcome": "committed"|"rejected"|"no_response"|"hardship"|"stop_contact"|"escalated"|null,
  "aria_summary": string,
  "final_offer_amount": number|null,
  "final_offer_deadline_hours": number|null
}`, `Use the NOVA call transcript and persisted offer only. A borrower saying yes to call availability is not offer acceptance. Mark offer_accepted true only if the borrower accepted exact payment terms after they were presented. Use outcome no_response when the call ended before any exact offer was presented. Summarize the call outcome and update ARIA memory with what NOVA already offered and how the borrower reacted. Do not generate DELTA runtime context here; DELTA generates its own runtime summary in a separate step.`)
	var result NovaCallHandoffResult
	tokens, err := client.ParseHandoff(prompt, &result)
	if err != nil {
		log.Printf("[collections] handoff generation failed workflow=%s agent=%s type=call_completion duration=%s err=%v", wf.Id, models.AgentNova, time.Since(start), err)
		return nil, fmt.Errorf("parse nova call handoff: %w", err)
	}
	log.Printf("[collections] handoff generation done workflow=%s agent=%s type=call_completion tokens=%d duration=%s", wf.Id, models.AgentNova, tokens, time.Since(start))
	return &HandoffCall[NovaCallHandoffResult]{Result: result, Tokens: tokens, ModelUsed: client.ModelUsed()}, nil
}

func NovaCallHandoffFromStructuredOutput(output map[string]any) (*HandoffCall[NovaCallHandoffResult], error) {
	if len(output) == 0 {
		return nil, nil
	}
	if nested, ok := output["result"].(map[string]any); ok {
		output = nested
	}
	if !hasNovaStructuredHandoffFields(output) {
		return nil, nil
	}
	data, err := json.Marshal(output)
	if err != nil {
		return nil, err
	}
	var result NovaCallHandoffResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse vapi nova structured output: %w", err)
	}
	return &HandoffCall[NovaCallHandoffResult]{Result: result, ModelUsed: "vapi/structured-output"}, nil
}

func hasNovaStructuredHandoffFields(output map[string]any) bool {
	_, hasAccepted := output["offer_accepted"]
	_, hasOutcome := output["outcome"]
	return hasAccepted && hasOutcome
}

func GenerateDeltaHandoff(wf models.BorrowerWorkflow, messages []models.AgentMessage) (*HandoffCall[DeltaHandoffResult], error) {
	client, err := agents.NewDelta()
	if err != nil {
		return nil, err
	}
	return GenerateDeltaHandoffWithClient(client, wf, messages)
}

func GenerateDeltaHandoffWithClient(client *agents.Client, wf models.BorrowerWorkflow, messages []models.AgentMessage) (*HandoffCall[DeltaHandoffResult], error) {
	start := time.Now()
	log.Printf("[collections] handoff generation start workflow=%s agent=%s type=handoff messages=%d", wf.Id, models.AgentDelta, len(messages))
	payload := map[string]any{
		"delta_runtime_summary": derefString(wf.ContextForDelta),
		"delta_messages":        agents.MessagesForCompletion(messages),
	}
	prompt := buildStructuredPrompt("DELTA final handoff", payload, `{
  "stage_complete": boolean,
  "outcome": "committed"|"rejected"|"no_response"|"hardship"|"stop_contact"|"escalated"|null,
  "delta_summary": string,
  "final_offer_amount": number|null,
  "final_offer_deadline_hours": number|null
}`, `Use only DELTA's runtime summary and any DELTA messages if present. If no DELTA messages are present, draft the one-time final offer handoff for email, keep stage_complete false, and leave outcome null. If DELTA messages are present, set stage_complete true only when the borrower clearly accepts the final offer, clearly rejects it, asks for stop-contact, reports terminal hardship handling, or the final-notice outcome is otherwise resolved. Use outcome committed for accepted final offer and rejected or escalated for declined/unresolved final offer. Always produce a <= 500 token delta_summary for ARIA memory.`)
	var result DeltaHandoffResult
	tokens, err := client.ParseHandoff(prompt, &result)
	if err != nil {
		log.Printf("[collections] handoff generation failed workflow=%s agent=%s type=handoff duration=%s err=%v", wf.Id, models.AgentDelta, time.Since(start), err)
		return nil, fmt.Errorf("parse delta handoff: %w", err)
	}
	log.Printf("[collections] handoff generation done workflow=%s agent=%s type=handoff tokens=%d duration=%s", wf.Id, models.AgentDelta, tokens, time.Since(start))
	return &HandoffCall[DeltaHandoffResult]{Result: result, Tokens: tokens, ModelUsed: client.ModelUsed()}, nil
}

func GenerateDeltaRuntimeContext(handoff NovaCallHandoffResult, offer *models.ResolutionOffer, wf models.BorrowerWorkflow) (*HandoffCall[DeltaRuntimeContextResult], error) {
	client, err := agents.NewDelta()
	if err != nil {
		return nil, err
	}
	return GenerateDeltaRuntimeContextWithClient(client, handoff, offer, wf)
}

func GenerateDeltaRuntimeContextWithClient(client *agents.Client, handoff NovaCallHandoffResult, offer *models.ResolutionOffer, wf models.BorrowerWorkflow) (*HandoffCall[DeltaRuntimeContextResult], error) {
	start := time.Now()
	log.Printf("[collections] handoff generation start workflow=%s agent=%s type=runtime_context", wf.Id, models.AgentDelta)
	payload := map[string]any{
		"nova_handoff":     novaOutcomeForDelta(handoff, offer, wf),
		"resolution_offer": conciseOfferState(offer),
		"final_offer":      conciseFinalOfferState(wf),
	}
	prompt := buildRuntimeSummaryPrompt("DELTA runtime context", payload, "Generate only the <= 500 token chat context DELTA needs from NOVA's handoff/outcome. Include what NOVA offered, whether the borrower accepted or rejected, objections, final offer amount/deadline, and any hardship or stop-contact flags. Do not include raw JSON or unrelated account data.")
	resp, err := client.GenerateTextWithTemporarySystem("You are DELTA's internal runtime-context summarizer. Generate concise chat context for the final notice assistant from provided JSON. Do not speak to the borrower. Return only plain text.", prompt, 6)
	if err != nil {
		contextForDelta := fallbackDeltaRuntimeContext(handoff, offer, wf)
		log.Printf("[collections] handoff generation fallback workflow=%s agent=%s type=runtime_context duration=%s err=%v", wf.Id, models.AgentDelta, time.Since(start), err)
		return &HandoffCall[DeltaRuntimeContextResult]{Result: DeltaRuntimeContextResult{ContextForDelta: contextForDelta}, Tokens: 0, ModelUsed: "deterministic/fallback"}, nil
	}
	contextForDelta := sanitizeRuntimeSummary(resp.AIResponse)
	if contextForDelta == "" {
		return nil, fmt.Errorf("generate delta runtime context: empty model response")
	}
	result := DeltaRuntimeContextResult{ContextForDelta: contextForDelta}
	tokens := resp.InputTokens + resp.OutputTokens
	log.Printf("[collections] handoff generation done workflow=%s agent=%s type=runtime_context tokens=%d context_chars=%d duration=%s", wf.Id, models.AgentDelta, tokens, len(result.ContextForDelta), time.Since(start))
	return &HandoffCall[DeltaRuntimeContextResult]{Result: result, Tokens: tokens, ModelUsed: client.ModelUsed()}, nil
}

func novaOutcomeForDelta(handoff NovaCallHandoffResult, offer *models.ResolutionOffer, wf models.BorrowerWorkflow) map[string]any {
	out := map[string]any{
		"aria_memory_after_nova": handoff.AriaSummary,
		"workflow_outcome":       handoff.Outcome,
		"final_offer_amount":     handoff.FinalOfferAmount,
		"hardship_flagged":       wf.HardshipFlagged,
		"stop_contact_flagged":   wf.StopContactFlagged,
	}
	out["offer_accepted"] = handoff.OfferAccepted
	out["accepted_offer_type"] = handoff.AcceptedOfferType
	out["objections_raised"] = handoff.ObjectionsRaised
	if handoff.FinalOfferDeadlineHours != nil {
		out["final_offer_deadline_hours"] = handoff.FinalOfferDeadlineHours
	}
	if offer != nil && offer.CandidateOffer != nil {
		out["candidate_offer"] = offer.CandidateOffer
	}
	return out
}

func buildStructuredPrompt(name string, payload map[string]any, schema string, rules string) string {
	data, _ := json.Marshal(payload)
	return fmt.Sprintf(`Generate %s.

Rules:
- %s
- Use only the provided input JSON.
- Do not invent facts; use null or empty values when unknown.
- Return ONLY valid JSON matching this schema:
%s

INPUT JSON:
%s`, name, rules, schema, string(data))
}

func buildRuntimeSummaryPrompt(name string, payload map[string]any, rules string) string {
	data, _ := json.Marshal(payload)
	return fmt.Sprintf(`Generate %s.

Rules:
- %s
- Use only the provided input JSON.
- Do not invent facts.
- Return plain text only. Do not return JSON, markdown, labels, or explanations.

INPUT JSON:
%s`, name, rules, string(data))
}

func sanitizeRuntimeSummary(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "```text")
	value = strings.TrimPrefix(value, "```")
	value = strings.TrimSuffix(value, "```")
	return strings.TrimSpace(value)
}

func fallbackNovaRuntimeContext(wf models.BorrowerWorkflow, offer *models.ResolutionOffer) string {
	lines := []string{
		"Use one Riverline identity and disclose that this is an AI-assisted recorded call.",
		"ARIA handoff summary: " + emptyAsNotRecorded(derefString(wf.AriaSummary)),
		"NOVA context: " + emptyAsNotRecorded(derefString(wf.ContextForNova)),
		"Present the available payment options before asking for commitment.",
	}
	if offer != nil {
		lines = append(lines, offerOptionLines(wf, *offer)...)
		if offer.ScheduledCallAt != nil {
			lines = append(lines, "Scheduled callback time: "+offer.ScheduledCallAt.Format(time.RFC3339)+".")
		}
	}
	lines = append(lines, "Ask whether the borrower accepts one of the exact options. Availability to talk is not acceptance.")
	return strings.Join(lines, "\n")
}

func fallbackDeltaRuntimeContext(handoff NovaCallHandoffResult, offer *models.ResolutionOffer, wf models.BorrowerWorkflow) string {
	lines := []string{
		"Use one Riverline identity in chat.",
		"NOVA outcome summary: " + emptyAsNotRecorded(handoff.AriaSummary),
		fmt.Sprintf("Offer accepted: %v.", handoff.OfferAccepted),
	}
	if handoff.AcceptedOfferType != nil {
		lines = append(lines, "Accepted option: "+*handoff.AcceptedOfferType+".")
	}
	if len(handoff.ObjectionsRaised) > 0 {
		lines = append(lines, "Objections: "+strings.Join(handoff.ObjectionsRaised, "; ")+".")
	}
	if offer != nil {
		lines = append(lines, offerOptionLines(wf, *offer)...)
	}
	if wf.FinalOfferAmount != nil {
		lines = append(lines, "Final offer amount: "+moneyText(*wf.FinalOfferAmount)+".")
	}
	if wf.FinalOfferDeadline != nil {
		lines = append(lines, "Final offer deadline: "+wf.FinalOfferDeadline.Format(time.RFC3339)+".")
	}
	return strings.Join(lines, "\n")
}

func conciseAssessmentState(wf models.BorrowerWorkflow) map[string]any {
	return map[string]any{
		"workflow_id":              wf.Id,
		"stage":                    wf.CurrentStage,
		"identity_verified":        wf.IdentityVerified,
		"employment_status":        wf.EmploymentStatus,
		"monthly_income_range":     wf.MonthlyIncomeRange,
		"monthly_obligations":      wf.MonthlyObligations,
		"default_reason":           wf.DefaultReason,
		"borrower_emotional_state": wf.BorrowerEmotionalState,
		"hardship_mentioned":       wf.HardshipMentioned,
		"stop_contact_flagged":     wf.StopContactFlagged,
	}
}

func conciseOfferState(offer *models.ResolutionOffer) map[string]any {
	if offer == nil {
		return map[string]any{}
	}
	return map[string]any{
		"scheduled_call_at":     offer.ScheduledCallAt,
		"lump_sum_offered":      offer.LumpSumOffered,
		"lump_sum_discount_pct": offer.LumpSumDiscountPct,
		"emi_amount":            offer.EmiAmount,
		"emi_months":            offer.EmiMonths,
		"emi_start_date":        offer.EmiStartDate,
		"hardship_offered":      offer.HardshipOffered,
		"offer_accepted":        offer.OfferAccepted,
		"accepted_offer_type":   offer.AcceptedOfferType,
		"objections_raised":     offer.ObjectionsRaised,
	}
}

func conciseFinalOfferState(wf models.BorrowerWorkflow) map[string]any {
	return map[string]any{
		"outcome":              wf.Outcome,
		"final_offer_amount":   wf.FinalOfferAmount,
		"final_offer_deadline": wf.FinalOfferDeadline,
		"hardship_flagged":     wf.HardshipFlagged,
		"stop_contact_flagged": wf.StopContactFlagged,
	}
}

func applyAriaHandoff(wf *models.BorrowerWorkflow, result AriaHandoffResult) {
	if result.Outcome != nil && (*result.Outcome == models.OutcomeStopContact || *result.Outcome == models.OutcomeHardship) {
		wf.Outcome = result.Outcome
		wf.StopContactFlagged = result.StopContactFlagged
		wf.HardshipMentioned = result.HardshipMentioned
		if strings.TrimSpace(result.AriaSummary) != "" {
			wf.AriaSummary = stringPtr(strings.TrimSpace(result.AriaSummary))
		}
		if strings.TrimSpace(result.ContextForNova) != "" {
			wf.ContextForNova = stringPtr(strings.TrimSpace(result.ContextForNova))
		}
		return
	}
	if result.IdentityVerified != nil && !*result.IdentityVerified {
		return
	}
	wf.IdentityVerified = result.IdentityVerified
	wf.EmploymentStatus = cleanStringPtr(result.EmploymentStatus)
	wf.MonthlyIncomeRange = cleanStringPtr(result.MonthlyIncomeRange)
	wf.MonthlyObligations = result.MonthlyObligations
	wf.DefaultReason = cleanStringPtr(result.DefaultReason)
	wf.BorrowerEmotionalState = result.BorrowerEmotionalState
	wf.HardshipMentioned = result.HardshipMentioned
	wf.StopContactFlagged = result.StopContactFlagged
	wf.Outcome = result.Outcome
	if strings.TrimSpace(result.AriaSummary) != "" {
		wf.AriaSummary = stringPtr(strings.TrimSpace(result.AriaSummary))
	}
	if strings.TrimSpace(result.ContextForNova) != "" {
		wf.ContextForNova = stringPtr(strings.TrimSpace(result.ContextForNova))
	}
}

func applyNovaOffer(offer *models.ResolutionOffer, result NovaOfferResult) {
	if len(result.CandidateOffer) > 0 {
		offer.CandidateOffer = result.CandidateOffer
	}
	offer.LumpSumOffered = result.LumpSumOffered
	offer.LumpSumDiscountPct = result.LumpSumDiscountPct
	offer.EmiAmount = result.EmiAmount
	offer.EmiMonths = result.EmiMonths
	offer.HardshipOffered = result.HardshipOffered
}

func applyNovaCallHandoff(wf *models.BorrowerWorkflow, offer *models.ResolutionOffer, result NovaCallHandoffResult) {
	offer.OfferAccepted = result.OfferAccepted
	offer.Status = offerStatusFromNovaHandoff(result)
	if result.Outcome != nil && *result.Outcome == models.OutcomeCommitted {
		accepted := true
		offer.OfferAccepted = &accepted
	}
	offer.AcceptedOfferType = cleanStringPtr(result.AcceptedOfferType)
	offer.ObjectionsRaised = result.ObjectionsRaised
	if strings.TrimSpace(result.AriaSummary) != "" {
		wf.AriaSummary = stringPtr(strings.TrimSpace(result.AriaSummary))
	}
	if result.FinalOfferAmount != nil {
		wf.FinalOfferAmount = result.FinalOfferAmount
	} else if offer.LumpSumOffered != nil {
		wf.FinalOfferAmount = offer.LumpSumOffered
	}
	if result.FinalOfferDeadlineHours != nil && *result.FinalOfferDeadlineHours > 0 {
		deadline := time.Now().UTC().Add(time.Duration(*result.FinalOfferDeadlineHours) * time.Hour)
		wf.FinalOfferDeadline = &deadline
	}
}

func offerStatusFromNovaHandoff(result NovaCallHandoffResult) models.OfferStatus {
	if result.Outcome != nil && *result.Outcome == models.OutcomeCommitted {
		return models.OfferStatusAccepted
	}
	if result.OfferAccepted != nil {
		if *result.OfferAccepted {
			return models.OfferStatusAccepted
		}
		return models.OfferStatusRejected
	}
	if result.Outcome != nil {
		switch *result.Outcome {
		case models.OutcomeCommitted:
			return models.OfferStatusAccepted
		case models.OutcomeRejected, models.OutcomeEscalated:
			return models.OfferStatusRejected
		}
	}
	return models.OfferStatusProposed
}

func applyDeltaRuntimeContext(wf *models.BorrowerWorkflow, result DeltaRuntimeContextResult) {
	if strings.TrimSpace(result.ContextForDelta) != "" {
		wf.ContextForDelta = stringPtr(strings.TrimSpace(result.ContextForDelta))
	}
}

func applyDeltaHandoff(wf *models.BorrowerWorkflow, result DeltaHandoffResult) {
	applyDeltaDraftHandoff(wf, result)
	if result.Outcome != nil {
		wf.Outcome = result.Outcome
	}
}

func applyDeltaDraftHandoff(wf *models.BorrowerWorkflow, result DeltaHandoffResult) {
	if strings.TrimSpace(result.DeltaSummary) != "" {
		wf.AriaSummary = stringPtr(strings.TrimSpace(result.DeltaSummary))
	}
	if result.FinalOfferAmount != nil {
		wf.FinalOfferAmount = result.FinalOfferAmount
	}
	if result.FinalOfferDeadlineHours != nil && *result.FinalOfferDeadlineHours > 0 {
		deadline := time.Now().UTC().Add(time.Duration(*result.FinalOfferDeadlineHours) * time.Hour)
		wf.FinalOfferDeadline = &deadline
	}
}

func applyDeltaOfferOutcome(offer *models.ResolutionOffer, result DeltaHandoffResult) {
	if result.Outcome == nil {
		return
	}
	switch *result.Outcome {
	case models.OutcomeCommitted:
		accepted := true
		offer.OfferAccepted = &accepted
		offer.Status = models.OfferStatusAccepted
		if offer.AcceptedOfferType == nil {
			acceptedType := "final_offer"
			offer.AcceptedOfferType = &acceptedType
		}
	case models.OutcomeRejected, models.OutcomeEscalated:
		accepted := false
		offer.OfferAccepted = &accepted
		offer.Status = models.OfferStatusRejected
	}
}

func cleanStringPtr(v *string) *string {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil
	}
	clean := strings.TrimSpace(*v)
	return &clean
}

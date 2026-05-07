package collections

import (
	"encoding/json"
	"fmt"
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
	ContextForDelta         string          `json:"context_for_delta"`
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

func GenerateAriaHandoff(wf models.BorrowerWorkflow, messages []models.AgentMessage) (*HandoffCall[AriaHandoffResult], error) {
	client, err := agents.NewAria()
	if err != nil {
		return nil, err
	}
	user, _ := GetUser(wf.UserId)
	loan, _ := GetLoan(wf.LoanId)
	payload := map[string]any{
		"workflow": wf,
		"messages": agents.MessagesForCompletion(messages),
		"user":     user,
		"loan":     loan,
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
}`, `Use only the ARIA chat messages plus user and loan facts. Produce the assessment fields ARIA collected and a <= 500 token context for NOVA. Include preferred_nova_call_at as an ISO-8601 timestamp with timezone when the borrower gave a preferred NOVA callback time; otherwise null. Do not compute or offer repayment terms.`)
	var result AriaHandoffResult
	tokens, err := client.ParseHandoff(prompt, &result)
	if err != nil {
		return nil, fmt.Errorf("parse aria handoff: %w", err)
	}
	return &HandoffCall[AriaHandoffResult]{Result: result, Tokens: tokens, ModelUsed: client.ModelUsed()}, nil
}

func GenerateNovaOffer(wf models.BorrowerWorkflow) (*HandoffCall[NovaOfferResult], error) {
	client, err := agents.NewNova()
	if err != nil {
		return nil, err
	}
	user, _ := GetUser(wf.UserId)
	loan, _ := GetLoan(wf.LoanId)
	payload := map[string]any{
		"workflow":         wf,
		"user":             user,
		"loan":             loan,
		"aria_summary":     wf.AriaSummary,
		"context_for_nova": wf.ContextForNova,
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
  "hardship_offered": boolean|null,
  "context_for_nova": string
}`, `Generate the offer NOVA should present from ARIA context, loan facts, and policy only. Keep context_for_nova <= 500 tokens. Do not mark a call outcome.`)
	var result NovaOfferResult
	tokens, err := client.ParseHandoff(prompt, &result)
	if err != nil {
		return nil, fmt.Errorf("parse nova offer: %w", err)
	}
	return &HandoffCall[NovaOfferResult]{Result: result, Tokens: tokens, ModelUsed: client.ModelUsed()}, nil
}

func GenerateNovaCallHandoff(wf models.BorrowerWorkflow, offer *models.ResolutionOffer, transcript string) (*HandoffCall[NovaCallHandoffResult], error) {
	client, err := agents.NewNova()
	if err != nil {
		return nil, err
	}
	payload := map[string]any{
		"workflow":         wf,
		"resolution_offer": offer,
		"call_transcript":  transcript,
		"aria_summary":     wf.AriaSummary,
		"context_for_nova": wf.ContextForNova,
	}
	prompt := buildStructuredPrompt("NOVA call completion handoff", payload, `{
  "offer_accepted": boolean|null,
  "accepted_offer_type": string|null,
  "objections_raised": array,
  "outcome": "committed"|"rejected"|"no_response"|"hardship"|"stop_contact"|"escalated"|null,
  "aria_summary": string,
  "context_for_delta": string,
  "final_offer_amount": number|null,
  "final_offer_deadline_hours": number|null
}`, `Use the call transcript and persisted offer only. Summarize the call outcome, update ARIA memory, and create <= 500 token context_for_delta if the account must continue to DELTA.`)
	var result NovaCallHandoffResult
	tokens, err := client.ParseHandoff(prompt, &result)
	if err != nil {
		return nil, fmt.Errorf("parse nova call handoff: %w", err)
	}
	return &HandoffCall[NovaCallHandoffResult]{Result: result, Tokens: tokens, ModelUsed: client.ModelUsed()}, nil
}

func GenerateDeltaHandoff(wf models.BorrowerWorkflow, messages []models.AgentMessage) (*HandoffCall[DeltaHandoffResult], error) {
	client, err := agents.NewDelta()
	if err != nil {
		return nil, err
	}
	user, _ := GetUser(wf.UserId)
	loan, _ := GetLoan(wf.LoanId)
	offer, _ := firstOffer(wf.Id)
	payload := map[string]any{
		"workflow":          wf,
		"user":              user,
		"loan":              loan,
		"resolution_offer":  offer,
		"messages":          agents.MessagesForCompletion(messages),
		"aria_summary":      wf.AriaSummary,
		"context_for_delta": wf.ContextForDelta,
	}
	prompt := buildStructuredPrompt("DELTA final handoff", payload, `{
  "stage_complete": boolean,
  "outcome": "committed"|"rejected"|"no_response"|"hardship"|"stop_contact"|"escalated"|null,
  "delta_summary": string,
  "final_offer_amount": number|null,
  "final_offer_deadline_hours": number|null
}`, `Use prior ARIA/NOVA context and any DELTA messages if present. If no DELTA messages are present, draft the one-time final offer handoff for email, keep stage_complete false, and leave outcome null. If DELTA messages are present, decide whether the final notice stage is complete. Always produce a <= 500 token delta_summary for ARIA memory.`)
	var result DeltaHandoffResult
	tokens, err := client.ParseHandoff(prompt, &result)
	if err != nil {
		return nil, fmt.Errorf("parse delta handoff: %w", err)
	}
	return &HandoffCall[DeltaHandoffResult]{Result: result, Tokens: tokens, ModelUsed: client.ModelUsed()}, nil
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

func applyAriaHandoff(wf *models.BorrowerWorkflow, result AriaHandoffResult) {
	wf.IdentityVerified = result.IdentityVerified
	wf.EmploymentStatus = cleanStringPtr(result.EmploymentStatus)
	wf.MonthlyIncomeRange = cleanStringPtr(result.MonthlyIncomeRange)
	wf.MonthlyObligations = result.MonthlyObligations
	wf.DefaultReason = cleanStringPtr(result.DefaultReason)
	wf.BorrowerEmotionalState = result.BorrowerEmotionalState
	wf.HardshipMentioned = result.HardshipMentioned
	wf.StopContactFlagged = result.StopContactFlagged
	if strings.TrimSpace(result.AriaSummary) != "" {
		wf.AriaSummary = stringPtr(strings.TrimSpace(result.AriaSummary))
	}
	if strings.TrimSpace(result.ContextForNova) != "" {
		wf.ContextForNova = stringPtr(strings.TrimSpace(result.ContextForNova))
	}
}

func applyNovaOffer(wf *models.BorrowerWorkflow, offer *models.ResolutionOffer, result NovaOfferResult) {
	if len(result.CandidateOffer) > 0 {
		offer.CandidateOffer = result.CandidateOffer
	}
	offer.LumpSumOffered = result.LumpSumOffered
	offer.LumpSumDiscountPct = result.LumpSumDiscountPct
	offer.EmiAmount = result.EmiAmount
	offer.EmiMonths = result.EmiMonths
	offer.HardshipOffered = result.HardshipOffered
	if strings.TrimSpace(result.ContextForNova) != "" {
		wf.ContextForNova = stringPtr(strings.TrimSpace(result.ContextForNova))
	}
}

func applyNovaCallHandoff(wf *models.BorrowerWorkflow, offer *models.ResolutionOffer, result NovaCallHandoffResult) {
	offer.OfferAccepted = result.OfferAccepted
	offer.AcceptedOfferType = cleanStringPtr(result.AcceptedOfferType)
	offer.ObjectionsRaised = result.ObjectionsRaised
	if strings.TrimSpace(result.AriaSummary) != "" {
		wf.AriaSummary = stringPtr(strings.TrimSpace(result.AriaSummary))
	}
	if strings.TrimSpace(result.ContextForDelta) != "" {
		wf.ContextForDelta = stringPtr(strings.TrimSpace(result.ContextForDelta))
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

func cleanStringPtr(v *string) *string {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil
	}
	clean := strings.TrimSpace(*v)
	return &clean
}

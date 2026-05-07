package collections

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"riverline_server/internal/agents"
	"riverline_server/internal/models"
)

type HandoffResult struct {
	StageComplete           bool            `json:"stage_complete"`
	IdentityVerified        *bool           `json:"identity_verified"`
	EmploymentStatus        *string         `json:"employment_status"`
	MonthlyIncomeRange      *string         `json:"monthly_income_range"`
	MonthlyObligations      *float64        `json:"monthly_obligations"`
	DefaultReason           *string         `json:"default_reason"`
	BorrowerEmotionalState  *models.Persona `json:"borrower_emotional_state"`
	HardshipMentioned       *bool           `json:"hardship_mentioned"`
	StopContactFlagged      *bool           `json:"stop_contact_flagged"`
	Outcome                 *models.Outcome `json:"outcome"`
	AriaSummary             string          `json:"aria_summary"`
	ContextForNova          string          `json:"context_for_nova"`
	ContextForDelta         string          `json:"context_for_delta"`
	DeltaSummary            string          `json:"delta_summary"`
	CandidateOffer          map[string]any  `json:"candidate_offer"`
	LumpSumOffered          *float64        `json:"lump_sum_offered"`
	LumpSumDiscountPct      *float64        `json:"lump_sum_discount_pct"`
	EmiAmount               *float64        `json:"emi_amount"`
	EmiMonths               *int            `json:"emi_months"`
	HardshipOffered         *bool           `json:"hardship_offered"`
	OfferAccepted           *bool           `json:"offer_accepted"`
	AcceptedOfferType       *string         `json:"accepted_offer_type"`
	ObjectionsRaised        []string        `json:"objections_raised"`
	FinalOfferAmount        *float64        `json:"final_offer_amount"`
	FinalOfferDeadlineHours *int            `json:"final_offer_deadline_hours"`
}

type HandoffCall struct {
	Result HandoffResult
	Tokens int
}

func GenerateHandoff(agentID models.AgentID, wf models.BorrowerWorkflow, messages []models.AgentMessage, transcript string) (*HandoffCall, error) {
	client, err := handoffClient(agentID)
	if err != nil {
		return nil, err
	}
	user, _ := GetUser(wf.UserId)
	loan, _ := GetLoan(wf.LoanId)
	prompt := buildHandoffPrompt(agentID, wf, user, loan, messages, transcript)
	var result HandoffResult
	tokens, err := client.ParseHandoff(prompt, &result)
	if err != nil {
		return nil, fmt.Errorf("parse %s handoff: %w", agentID, err)
	}
	return &HandoffCall{Result: result, Tokens: tokens}, nil
}

func buildHandoffPrompt(agentID models.AgentID, wf models.BorrowerWorkflow, user *models.User, loan *models.Loan, messages []models.AgentMessage, transcript string) string {
	payload := map[string]any{
		"agent_id":              agentID,
		"workflow":              wf,
		"messages":              messages,
		"voice_transcript":      transcript,
		"existing_aria_summary": wf.AriaSummary,
		"context_for_nova":      wf.ContextForNova,
		"context_for_delta":     wf.ContextForDelta,
	}
	if user != nil {
		payload["user"] = user
	}
	if loan != nil {
		payload["loan"] = loan
		payload["offer_policy"] = map[string]any{
			"outstanding_amount":      loan.OutstandingAmount,
			"policy_max_discount_pct": loan.PolicyMaxDiscountPct,
			"days_overdue":            loan.DaysOverdue,
		}
	}
	data, _ := json.Marshal(payload)
	return fmt.Sprintf(`Generate the single authoritative handoff object for the current borrower stage.

Rules:
- Use only the provided conversation, transcript, user, loan, workflow, and policy data.
- Do not invent facts. If a field is unknown, use null, false only when explicitly false, or an empty string.
- All summaries and contexts must be <= 500 tokens.
- For ARIA: fill assessment fields, stage_complete, aria_summary, context_for_nova, outcome, hardship/stop-contact flags.
- For NOVA: fill candidate_offer, actual offer fields, offer outcome, objections, aria_summary, context_for_delta, final offer fields.
- For DELTA: fill outcome and delta_summary.
- Return ONLY valid JSON matching these keys:
{
  "stage_complete": boolean,
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
  "context_for_delta": string,
  "delta_summary": string,
  "candidate_offer": object,
  "lump_sum_offered": number|null,
  "lump_sum_discount_pct": number|null,
  "emi_amount": number|null,
  "emi_months": number|null,
  "hardship_offered": boolean|null,
  "offer_accepted": boolean|null,
  "accepted_offer_type": string|null,
  "objections_raised": array,
  "final_offer_amount": number|null,
  "final_offer_deadline_hours": number|null
}

INPUT JSON:
%s`, string(data))
}

func handoffClient(agentID models.AgentID) (*agents.Client, error) {
	switch agentID {
	case models.AgentNova:
		return agents.NewNova()
	case models.AgentDelta:
		return agents.NewDelta()
	default:
		return agents.NewAria()
	}
}

func applyAssessmentHandoff(wf *models.BorrowerWorkflow, result HandoffResult) {
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

func applyNovaHandoff(wf *models.BorrowerWorkflow, offer *models.ResolutionOffer, result HandoffResult) {
	if len(result.CandidateOffer) > 0 {
		offer.CandidateOffer = result.CandidateOffer
	}
	offer.LumpSumOffered = result.LumpSumOffered
	offer.LumpSumDiscountPct = result.LumpSumDiscountPct
	offer.EmiAmount = result.EmiAmount
	offer.EmiMonths = result.EmiMonths
	offer.HardshipOffered = result.HardshipOffered
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
	} else if result.LumpSumOffered != nil {
		wf.FinalOfferAmount = result.LumpSumOffered
	}
	if result.FinalOfferDeadlineHours != nil && *result.FinalOfferDeadlineHours > 0 {
		deadline := time.Now().UTC().Add(time.Duration(*result.FinalOfferDeadlineHours) * time.Hour)
		wf.FinalOfferDeadline = &deadline
	}
}

func applyDeltaHandoff(wf *models.BorrowerWorkflow, result HandoffResult) {
	if strings.TrimSpace(result.DeltaSummary) != "" {
		wf.AriaSummary = stringPtr(strings.TrimSpace(result.DeltaSummary))
	}
	if result.Outcome != nil {
		wf.Outcome = result.Outcome
	}
}

func cleanStringPtr(v *string) *string {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil
	}
	clean := strings.TrimSpace(*v)
	return &clean
}

package collections

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"riverline_server/internal/models"
)

type DeltaHandoffExport struct {
	WorkflowID         string                  `json:"workflow_id"`
	UserID             string                  `json:"user_id"`
	LoanID             string                  `json:"loan_id"`
	CurrentStage       models.AgentID          `json:"current_stage"`
	Outcome            *models.Outcome         `json:"outcome,omitempty"`
	DeltaSummary       *string                 `json:"delta_summary,omitempty"`
	ContextForDelta    *string                 `json:"context_for_delta,omitempty"`
	FinalOfferAmount   *float64                `json:"final_offer_amount,omitempty"`
	FinalOfferDeadline *time.Time              `json:"final_offer_deadline,omitempty"`
	ResolvedAt         *time.Time              `json:"resolved_at,omitempty"`
	StopContactFlagged *bool                   `json:"stop_contact_flagged,omitempty"`
	HardshipFlagged    *bool                   `json:"hardship_flagged,omitempty"`
	Offer              *models.ResolutionOffer `json:"offer,omitempty"`
	ExportedAt         time.Time               `json:"exported_at"`
}

var ErrDeltaHandoffUnavailable = errors.New("delta handoff is not available yet")

func DeltaHandoffForWorkflow(workflowID string) (*DeltaHandoffExport, error) {
	wf, err := GetWorkflow(workflowID)
	if err != nil {
		return nil, err
	}
	offer, _ := firstOffer(wf.Id)
	if !deltaHandoffAvailable(*wf, offer) {
		return nil, ErrDeltaHandoffUnavailable
	}
	return &DeltaHandoffExport{
		WorkflowID:         wf.Id,
		UserID:             wf.UserId,
		LoanID:             wf.LoanId,
		CurrentStage:       wf.CurrentStage,
		Outcome:            wf.Outcome,
		DeltaSummary:       cleanExportString(wf.AriaSummary),
		ContextForDelta:    cleanExportString(wf.ContextForDelta),
		FinalOfferAmount:   wf.FinalOfferAmount,
		FinalOfferDeadline: wf.FinalOfferDeadline,
		ResolvedAt:         wf.ResolvedAt,
		StopContactFlagged: wf.StopContactFlagged,
		HardshipFlagged:    wf.HardshipFlagged,
		Offer:              offer,
		ExportedAt:         time.Now().UTC(),
	}, nil
}

func DeltaHandoffMessageForWorkflow(workflowID string) (string, error) {
	export, err := DeltaHandoffForWorkflow(workflowID)
	if err != nil {
		return "", err
	}
	lines := []string{}
	if export.ContextForDelta != nil && strings.TrimSpace(*export.ContextForDelta) != "" {
		lines = append(lines, strings.TrimSpace(*export.ContextForDelta))
	}
	if export.Outcome != nil {
		lines = append(lines, "Outcome: "+string(*export.Outcome)+".")
	}
	if export.FinalOfferAmount != nil {
		lines = append(lines, fmt.Sprintf("Final offer amount: %.2f.", *export.FinalOfferAmount))
	}
	if export.FinalOfferDeadline != nil {
		lines = append(lines, "Final offer deadline: "+export.FinalOfferDeadline.Format(time.RFC3339)+".")
	}
	if export.Offer != nil {
		if export.Offer.OfferAccepted != nil {
			lines = append(lines, fmt.Sprintf("NOVA offer accepted: %t.", *export.Offer.OfferAccepted))
		}
		if export.Offer.AcceptedOfferType != nil && strings.TrimSpace(*export.Offer.AcceptedOfferType) != "" {
			lines = append(lines, "Accepted NOVA offer type: "+strings.TrimSpace(*export.Offer.AcceptedOfferType)+".")
		}
	}
	if len(lines) == 0 {
		return "", ErrDeltaHandoffUnavailable
	}
	return strings.TrimSpace(strings.Join(lines, "\n")), nil
}

func deltaHandoffAvailable(wf models.BorrowerWorkflow, offer *models.ResolutionOffer) bool {
	if wf.CurrentStage != models.AgentDelta && wf.ResolvedAt == nil && wf.Outcome == nil {
		return false
	}
	if strings.TrimSpace(derefString(wf.ContextForDelta)) != "" {
		return true
	}
	if wf.FinalOfferAmount != nil || wf.FinalOfferDeadline != nil {
		return true
	}
	return offer != nil && (offer.OfferAccepted != nil || offer.Status != "")
}

func cleanExportString(value *string) *string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	clean := strings.TrimSpace(*value)
	return &clean
}

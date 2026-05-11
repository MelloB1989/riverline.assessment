package collections

import (
	"testing"
	"time"

	"riverline_server/internal/models"
)

func TestScheduleMetadataDoesNotCountAsNovaOfferTerms(t *testing.T) {
	offer := &models.ResolutionOffer{
		CandidateOffer: map[string]any{
			"scheduled_call_at": "2026-05-11T14:56:48Z",
			"schedule_reason":   "Initial preferred NOVA call time from ARIA intake",
		},
	}

	if novaOfferHasRuntimeTerms(offer) {
		t.Fatal("schedule-only candidate_offer must not be treated as prepared NOVA terms")
	}
	if !stripNovaScheduleMetadata(offer) {
		t.Fatal("expected schedule metadata to be stripped")
	}
	if len(offer.CandidateOffer) != 0 {
		t.Fatalf("candidate_offer should be empty after stripping schedule metadata, got %#v", offer.CandidateOffer)
	}
}

func TestEnrichNovaCandidateOfferAddsPaymentOptions(t *testing.T) {
	lumpSum := 7663.5
	discount := 22.0
	emiAmount := 1637.5
	emiMonths := 6
	emiStart := time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC)
	scheduledAt := time.Date(2026, 5, 11, 14, 56, 48, 0, time.UTC)
	offer := &models.ResolutionOffer{
		CandidateOffer:     map[string]any{"scheduled_call_at": scheduledAt.Format(time.RFC3339)},
		ScheduledCallAt:    &scheduledAt,
		LumpSumOffered:     &lumpSum,
		LumpSumDiscountPct: &discount,
		EmiAmount:          &emiAmount,
		EmiMonths:          &emiMonths,
		EmiStartDate:       &emiStart,
	}

	enrichNovaCandidateOffer(offer)

	if _, ok := offer.CandidateOffer["scheduled_call_at"]; ok {
		t.Fatal("candidate_offer must not retain scheduling metadata")
	}
	if offer.CandidateOffer["lump_sum_offered"] != lumpSum {
		t.Fatalf("missing flat lump_sum_offered, got %#v", offer.CandidateOffer)
	}
	if offer.CandidateOffer["emi_amount"] != emiAmount || offer.CandidateOffer["emi_months"] != emiMonths {
		t.Fatalf("missing flat EMI details, got %#v", offer.CandidateOffer)
	}
	if _, ok := offer.CandidateOffer["primary_option"].(map[string]any); !ok {
		t.Fatalf("missing primary option object, got %#v", offer.CandidateOffer)
	}
	if _, ok := offer.CandidateOffer["secondary_option"].(map[string]any); !ok {
		t.Fatalf("missing secondary option object, got %#v", offer.CandidateOffer)
	}
}

func TestNovaRuntimeContextMustContainOfferTerms(t *testing.T) {
	lumpSum := 7663.5
	emiAmount := 1637.5
	emiMonths := 6
	offer := &models.ResolutionOffer{
		LumpSumOffered: &lumpSum,
		EmiAmount:      &emiAmount,
		EmiMonths:      &emiMonths,
	}

	ariaOnly := "Personal loan ending 6789, outstanding $9,825.00, 74 days overdue."
	if novaRuntimeContextHasOfferTerms(ariaOnly, offer) {
		t.Fatal("ARIA-only context must not count as prepared NOVA runtime context")
	}

	withTerms := "Primary option is a lump-sum settlement of $7,663.50. Secondary option is $1,637.50 per month for 6 months."
	if !novaRuntimeContextHasOfferTerms(withTerms, offer) {
		t.Fatal("context containing exact lump-sum and EMI terms should be accepted")
	}
}

func TestNormalizeHardshipOfferFlagRequiresExplicitHardship(t *testing.T) {
	hardship := true
	lumpSum := 7663.5
	wf := &models.BorrowerWorkflow{
		AriaSummary:    stringPtr("Student with income about 1000 and obligations about 500. Wants lower payments."),
		ContextForNova: stringPtr("Affordability concern and request for installments only."),
	}
	offer := &models.ResolutionOffer{
		LumpSumOffered:  &lumpSum,
		HardshipOffered: &hardship,
	}

	normalizeHardshipOfferFlag(offer, wf)

	if offer.HardshipOffered == nil || *offer.HardshipOffered {
		t.Fatal("ordinary affordability facts must not leave hardship_offered true")
	}
}

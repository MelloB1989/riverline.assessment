package eval

import (
	"strings"
	"testing"
	"time"

	"riverline_server/internal/models"
)

func TestSimulationReadyForSystemScoringRequiresCompleteFlow(t *testing.T) {
	sim := SimulatedConversation{
		Transcript: "ARIA TRANSCRIPT\nok\n\nNOVA TRANSCRIPT\nok\n\nDELTA TRANSCRIPT\nok",
		AgentTranscripts: map[models.AgentID]string{
			models.AgentAria:  "Borrower: hi",
			models.AgentNova:  "Agent: offer",
			models.AgentDelta: "Delta handoff context: final offer",
		},
		Metadata: map[string]any{},
	}
	if !simulationReadyForSystemScoring(sim) {
		t.Fatal("expected complete ARIA/NOVA/DELTA flow to be scoreable")
	}

	delete(sim.AgentTranscripts, models.AgentDelta)
	if simulationReadyForSystemScoring(sim) {
		t.Fatal("expected missing DELTA handoff to block scoring")
	}
}

func TestSimulationReadyForSystemScoringBlocksSimulationErrors(t *testing.T) {
	sim := SimulatedConversation{
		Transcript: "ARIA TRANSCRIPT\nok\n\nNOVA TRANSCRIPT\nok\n\nDELTA TRANSCRIPT\nok",
		AgentTranscripts: map[models.AgentID]string{
			models.AgentAria:  "Borrower: hi",
			models.AgentNova:  "Agent: offer",
			models.AgentDelta: "Delta handoff context: final offer",
		},
		Metadata: map[string]any{"simulation_error": "empty AI response"},
	}
	if simulationReadyForSystemScoring(sim) {
		t.Fatal("expected preserved partial simulation with error to block scoring")
	}
}

func TestEvaluationPromptShapeDoesNotJSONWrapRubric(t *testing.T) {
	systemPrompt := buildEvaluationSystemPrompt(models.EvaluatorVersion{
		AgentId:       models.AgentAria,
		VersionNumber: 1,
		JudgePrompt:   "Rubric: score carefully.",
	}, true)
	userPrompt := buildEvaluationUserPrompt("ARIA TRANSCRIPT\nBorrower: hi", true)

	if !strings.Contains(systemPrompt, "Rubric: score carefully.") {
		t.Fatal("expected evaluator rubric in system prompt")
	}
	if strings.Contains(userPrompt, "judge_prompt") {
		t.Fatal("user prompt should not JSON-wrap the evaluator rubric")
	}
	if !strings.Contains(userPrompt, "ARIA TRANSCRIPT") {
		t.Fatal("expected transcript in user prompt")
	}
}

func TestNoteProviderRateLimitKeepsLongestCooldown(t *testing.T) {
	provider := "test-provider"
	providerRateLimitUntil.Delete(providerRateLimitKey(provider))

	noteProviderRateLimit(provider, errRateLimitedForTest{}, 50*time.Millisecond)
	firstRaw, ok := providerRateLimitUntil.Load(providerRateLimitKey(provider))
	if !ok {
		t.Fatal("expected cooldown to be recorded")
	}
	first := firstRaw.(time.Time)

	noteProviderRateLimit(provider, errRateLimitedForTest{}, 5*time.Millisecond)
	secondRaw, ok := providerRateLimitUntil.Load(providerRateLimitKey(provider))
	if !ok {
		t.Fatal("expected cooldown to remain recorded")
	}
	second := secondRaw.(time.Time)
	if !second.Equal(first) {
		t.Fatal("expected shorter cooldown not to replace longer cooldown")
	}

	providerRateLimitUntil.Delete(providerRateLimitKey(provider))
}

type errRateLimitedForTest struct{}

func (errRateLimitedForTest) Error() string {
	return "429 too many requests"
}

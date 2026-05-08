package vapi

import (
	"strings"
	"testing"
)

func TestNovaSystemPromptIncludesDynamicVariables(t *testing.T) {
	prompt := NovaSystemPrompt("base nova prompt")
	for _, token := range []string{
		"base nova prompt",
		"{{context_for_nova}}",
		"{{current_ist_timestamp}}",
		"only permission to continue",
		"must present the exact primary payment option",
	} {
		if !strings.Contains(prompt, token) {
			t.Fatalf("prompt missing %s", token)
		}
	}
}

func TestNovaFirstMessageUsesSingleRiverlineIdentity(t *testing.T) {
	message := novaFirstMessageTemplate()
	if strings.Contains(message, "Nova") || strings.Contains(message, "NOVA") {
		t.Fatalf("first message exposed internal agent name: %s", message)
	}
	if !strings.Contains(message, "lay out the repayment options") {
		t.Fatalf("first message should prime the offer presentation, got: %s", message)
	}
}

func TestNovaVoicemailUsesSingleRiverlineIdentity(t *testing.T) {
	message := novaVoicemailMessageTemplate()
	if strings.Contains(message, "Nova") || strings.Contains(message, "NOVA") {
		t.Fatalf("voicemail exposed internal agent name: %s", message)
	}
	if !strings.Contains(message, "Riverline") {
		t.Fatalf("voicemail should identify Riverline, got: %s", message)
	}
}

func TestExtractNovaStructuredOutput(t *testing.T) {
	payload := map[string]any{
		"artifact": map[string]any{
			"structuredOutputs": map[string]any{
				"output-id": map[string]any{
					"name": NovaStructuredOutputName,
					"result": map[string]any{
						"offer_accepted": true,
						"outcome":        "committed",
					},
				},
			},
		},
	}
	result := ExtractNovaStructuredOutput(payload)
	if result == nil {
		t.Fatal("expected structured output result")
	}
	if result["outcome"] != "committed" {
		t.Fatalf("outcome = %v, want committed", result["outcome"])
	}
}

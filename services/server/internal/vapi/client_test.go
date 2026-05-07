package vapi

import (
	"strings"
	"testing"
)

func TestNovaSystemPromptIncludesDynamicVariables(t *testing.T) {
	prompt := NovaSystemPrompt("base nova prompt")
	for _, token := range []string{
		"base nova prompt",
		"{{borrower_context}}",
		"{{loan_context}}",
		"{{context_for_nova}}",
		"{{resolution_offer_context}}",
		"{{current_ist_timestamp}}",
	} {
		if !strings.Contains(prompt, token) {
			t.Fatalf("prompt missing %s", token)
		}
	}
}

func TestExtractNovaStructuredOutput(t *testing.T) {
	payload := map[string]any{
		"artifact": map[string]any{
			"structuredOutputs": map[string]any{
				"output-id": map[string]any{
					"name": NovaStructuredOutputName,
					"result": map[string]any{
						"offer_accepted":    true,
						"outcome":           "committed",
						"context_for_delta": "",
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

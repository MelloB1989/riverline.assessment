package agents

import (
	"strings"
	"testing"
	"time"

	"riverline_server/internal/models"
)

func TestMessagesForCompletionPrefixesBorrowerMessagesWithIST(t *testing.T) {
	messages := []models.AgentMessage{
		{
			Id:        "borrower-message",
			Role:      models.MessageRoleBorrower,
			Content:   "I can pay next week.",
			CreatedAt: time.Date(2026, 5, 7, 18, 0, 0, 0, time.UTC),
		},
		{
			Id:        "agent-message",
			Role:      models.MessageRoleAgent,
			Content:   "Thanks for confirming.",
			CreatedAt: time.Date(2026, 5, 7, 18, 1, 0, 0, time.UTC),
		},
	}

	formatted := MessagesForCompletion(messages)

	if got, want := formatted[0].Content, "[IST 2026-05-07 23:30:00 IST] I can pay next week."; got != want {
		t.Fatalf("borrower content = %q, want %q", got, want)
	}
	if got, want := formatted[1].Content, "Thanks for confirming."; got != want {
		t.Fatalf("agent content = %q, want %q", got, want)
	}
	if messages[0].Content != "I can pay next week." {
		t.Fatalf("stored message was mutated: %q", messages[0].Content)
	}
}

func TestToKarmaHistoryDoesNotInjectRuntimeContextAsMessage(t *testing.T) {
	messages := []models.AgentMessage{
		{
			Id:        "borrower-message",
			Role:      models.MessageRoleBorrower,
			Content:   "What is my balance?",
			CreatedAt: time.Date(2026, 5, 7, 18, 0, 0, 0, time.UTC),
		},
	}

	history := toKarmaHistory(messages)

	if len(history.Messages) != 1 {
		t.Fatalf("history message count = %d, want 1", len(history.Messages))
	}
	if history.Messages[0].Role != "user" {
		t.Fatalf("history role = %q, want user", history.Messages[0].Role)
	}
}

func TestSystemPromptIncludesRuntimeContext(t *testing.T) {
	client := &Client{prompt: "BASE PROMPT"}

	got := client.systemPrompt(`{"loan":{"outstanding_amount":12345}}`)

	if want := "BASE PROMPT"; got[:len(want)] != want {
		t.Fatalf("system prompt prefix = %q, want %q", got[:len(want)], want)
	}
	if !containsAll(got, "RUNTIME CONTEXT", "outstanding_amount", "12345") {
		t.Fatalf("system prompt missing runtime context: %s", got)
	}
}

func containsAll(value string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(value, needle) {
			return false
		}
	}
	return true
}

package agents

import (
	"math"
	"strings"

	"riverline_server/internal/models"
)

func CountTokens(text string) int {
	if text == "" {
		return 0
	}
	return int(math.Ceil(float64(len([]rune(text))) / 4.0))
}

func fallbackReply(agentID models.AgentID, history []models.AgentMessage) string {
	switch agentID {
	case models.AgentDelta:
		return "I am DELTA, an AI final notice agent acting on behalf of Riverline. This conversation is being logged. The final offer is now documented with a hard deadline. Reply ACCEPT to begin the settlement process."
	case models.AgentNova:
		return "I am NOVA, an AI resolution agent acting on behalf of Riverline. This call is being recorded. I can present the settlement options already calculated for this account."
	default:
		return ariaFallback(history)
	}
}

func ariaFallback(history []models.AgentMessage) string {
	text := strings.ToLower(join(history))
	missing := []string{}
	if !strings.Contains(text, "yes") && !strings.Contains(text, "verify") {
		missing = append(missing, "identity confirmation")
	}
	if !strings.Contains(text, "employed") && !strings.Contains(text, "job") && !strings.Contains(text, "unemployed") {
		missing = append(missing, "employment status")
	}
	if !strings.Contains(text, "income") && !strings.Contains(text, "salary") && !strings.Contains(text, "earn") {
		missing = append(missing, "monthly income")
	}
	if !strings.Contains(text, "rent") && !strings.Contains(text, "obligation") && !strings.Contains(text, "expenses") {
		missing = append(missing, "monthly obligations")
	}
	if !strings.Contains(text, "reason") && !strings.Contains(text, "missed") && !strings.Contains(text, "medical") {
		missing = append(missing, "reason for default")
	}
	if len(missing) == 0 {
		return "Thank you. A resolution specialist will be in touch shortly."
	}
	return "I am ARIA, an AI assessment agent acting on behalf of Riverline. This conversation is being logged. Please provide: " + strings.Join(missing, ", ") + "."
}

func join(messages []models.AgentMessage) string {
	var b strings.Builder
	for _, msg := range messages {
		b.WriteString(msg.Content)
		b.WriteByte('\n')
	}
	return b.String()
}

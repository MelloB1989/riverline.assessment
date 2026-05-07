package agents

import (
	"riverline_server/internal/models"

	"github.com/MelloB1989/karma/ai"
)

const (
	TotalContextBudget   = 2000
	HandoffContextBudget = 500
)

type Client struct {
	agentID       models.AgentID
	promptVersion int
	prompt        string
	aiClient      *ai.KarmaAI
}

type Config struct {
	Temperature float32
	TopP        float32
	TopK        int
}

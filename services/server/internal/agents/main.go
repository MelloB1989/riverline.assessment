package agents

import (
	"riverline_server/internal/models"

	"github.com/MelloB1989/karma/ai"
)

type Client struct {
	agentID       models.AgentID
	promptVersion int
	prompt        string
	modelID       string
	providerID    string
	aiClient      *ai.KarmaAI
}

type Config struct {
	Model       ai.BaseModel
	Provider    ai.Provider
	Temperature float32
	TopP        float32
	TopK        int
}

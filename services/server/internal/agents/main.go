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
	cfg           Config
	aiClient      *ai.KarmaAI
}

type Config struct {
	Model       ai.BaseModel
	Provider    ai.Provider
	Temperature float32
	TopP        float32
	TopK        int
}

func DefaultConfig(agentID models.AgentID) Config {
	switch agentID {
	case models.AgentNova:
		return Config{Model: ai.GPTOSS_120B, Provider: ai.Groq, Temperature: 0.5, TopP: 0.90, TopK: 50}
	case models.AgentDelta:
		return Config{Model: ai.GPTOSS_120B, Provider: ai.Groq, Temperature: 0.1, TopP: 0.80, TopK: 30}
	default:
		return Config{Model: ai.GPTOSS_120B, Provider: ai.Groq, Temperature: 0.2, TopP: 0.85, TopK: 40}
	}
}

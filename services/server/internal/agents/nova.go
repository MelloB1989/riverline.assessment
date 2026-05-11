package agents

import (
	"riverline_server/internal/models"

	"github.com/MelloB1989/karma/ai"
)

func NewNova() (*Client, error) {
	return newClient(models.AgentNova, DefaultConfig(models.AgentNova))
}

func NewNovaGrok4FastReasoning() (*Client, error) {
	return newClient(models.AgentNova, Config{Model: ai.Grok4ReasoningFast, Provider: ai.XAI, Temperature: 0.1, TopP: 0.85, TopK: 40})
}

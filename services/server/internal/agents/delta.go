package agents

import (
	"riverline_server/internal/models"

	"github.com/MelloB1989/karma/ai"
)

func NewDelta() (*Client, error) {
	return newClient(models.AgentDelta, Config{Model: ai.Llama33_70B, Provider: ai.Groq, Temperature: 0.1, TopP: 0.80, TopK: 30})
}

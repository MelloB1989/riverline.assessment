package agents

import (
	"riverline_server/internal/models"

	"github.com/MelloB1989/karma/ai"
)

func NewAria() (*Client, error) {
	return newClient(models.AgentAria, Config{Model: ai.Llama33_70B, Provider: ai.Groq, Temperature: 0.2, TopP: 0.85, TopK: 40})
}

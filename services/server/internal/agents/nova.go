package agents

import (
	"riverline_server/internal/models"

	"github.com/MelloB1989/karma/ai"
)

func NewNova() (*Client, error) {
	return newClient(models.AgentNova, Config{Model: ai.Llama33_70B, Provider: ai.Groq, Temperature: 0.5, TopP: 0.90, TopK: 50})
}

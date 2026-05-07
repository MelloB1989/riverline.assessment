package agents

import "riverline_server/internal/models"

func NewAria() (*Client, error) {
	return newClient(models.AgentAria, Config{Temperature: 0.2, TopP: 0.85, TopK: 40})
}

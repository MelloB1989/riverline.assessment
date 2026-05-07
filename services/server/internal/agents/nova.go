package agents

import "riverline_server/internal/models"

func NewNova() (*Client, error) {
	return newClient(models.AgentNova, Config{Temperature: 0.5, TopP: 0.90, TopK: 50})
}

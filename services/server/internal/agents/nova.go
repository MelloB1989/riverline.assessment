package agents

import "riverline_server/internal/models"

func NewNova() (*Client, error) {
	return newClient(models.AgentNova, DefaultConfig(models.AgentNova))
}

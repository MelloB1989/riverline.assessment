package agents

import "riverline_server/internal/models"

func NewAria() (*Client, error) {
	return newClient(models.AgentAria, DefaultConfig(models.AgentAria))
}

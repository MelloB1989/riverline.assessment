package agents

import "riverline_server/internal/models"

func NewDelta() (*Client, error) {
	return newClient(models.AgentDelta, DefaultConfig(models.AgentDelta))
}

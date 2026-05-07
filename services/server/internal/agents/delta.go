package agents

import "riverline_server/internal/models"

func NewDelta() (*Client, error) {
	return newClient(models.AgentDelta, Config{Temperature: 0.1, TopP: 0.80, TopK: 30})
}

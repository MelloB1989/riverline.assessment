package agents

import (
	m "riverline_server/internal/models"

	"github.com/MelloB1989/karma/models"
)

type Agent interface {
	Start(summary string) (conversationId string, Err error)
	Converse(user_message string) (*models.AIChatResponse, error)
	CreateHandOff() (string, error)
	GetAgentId() m.AgentID
	GetConversationId() string
}

package agents

import (
	"errors"
	"riverline_server/internal/models"
	"time"

	m "github.com/MelloB1989/karma/models"

	"github.com/MelloB1989/karma/ai"
	"github.com/MelloB1989/karma/utils"
	"github.com/MelloB1989/karma/v2/orm"
)

type Aria struct {
	aiClient       *ai.KarmaAI
	userId         string
	conversationId string
	promptVersion  int
}

func NewAria(userId string, promptVersion int) (Agent, error) {
	promptOrm := orm.Load(&models.PromptVersion{})
	defer promptOrm.Close()

	var p []models.PromptVersion
	if err := promptOrm.GetByFieldsEquals(map[string]any{
		"AgentId":       models.AgentAria,
		"VersionNumber": promptVersion,
	}).Scan(&p); err != nil {
		return nil, err
	}
	if len(p) == 0 {
		return nil, errors.New("prompt version not found")
	}
	prompt := p[0]
	if !prompt.IsActive {
		return nil, errors.New("prompt version is not active")
	}
	return &Aria{
		aiClient: ai.NewKarmaAI(
			ai.Llama33_70B,
			ai.Groq,
			ai.WithMaxTokens(500),
			ai.WithSystemMessage(prompt.PromptText),
			ai.WithTemperature(0.2), //Low temperature because ARIA must be consistent and clinical.
			ai.WithTopK(40),
			ai.WithTopP(0.85),
		),
		userId:        userId,
		promptVersion: promptVersion,
	}, nil
}

func (a *Aria) Start(summary string) (string, error) {
	conversationOrm := orm.Load(&models.AgentConversation{})
	defer conversationOrm.Close()

	newConversation := models.AgentConversation{
		AgentId:       models.AgentAria,
		Id:            utils.GenerateID(),
		UserId:        a.userId,
		PromptVersion: a.promptVersion,
		StartedAt:     time.Now(),
	}
	if err := conversationOrm.Insert(&newConversation); err != nil {
		return "", err
	}
	return newConversation.Id, nil
}

func (a *Aria) Converse(user_message string) (*m.AIChatResponse, error) {
	history, err := generateAIChatHistory(a.conversationId)
	if err != nil {
		return nil, err
	}
	watchAndAppendMessages(history)
	return a.aiClient.ChatCompletionManaged(history)
}

func (a *Aria) CreateHandOff() (string, error) {
	return "", nil
}

func (a *Aria) GetAgentId() models.AgentID {
	return models.AgentAria
}

func (a *Aria) GetConversationId() string {
	return a.conversationId
}

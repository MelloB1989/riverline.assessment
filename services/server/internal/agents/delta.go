package agents

import (
	"errors"
	"riverline_server/internal/models"
	"time"

	"github.com/MelloB1989/karma/ai"
	m "github.com/MelloB1989/karma/models"
	"github.com/MelloB1989/karma/utils"
	"github.com/MelloB1989/karma/v2/orm"
)

type Delta struct {
	aiClient       *ai.KarmaAI
	userId         string
	conversationId string
	promptVersion  int
}

func NewDelta(userId, conversationId string, promptVersion int) (*Delta, error) {
	promptOrm := orm.Load(&models.PromptVersion{})
	var p []models.PromptVersion
	if err := promptOrm.GetByFieldsEquals(map[string]any{
		"AgentId":       models.AgentDelta,
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
	return &Delta{
		aiClient: ai.NewKarmaAI(
			ai.Llama33_70B,
			ai.Groq,
			ai.WithMaxTokens(500),
			ai.WithSystemMessage(prompt.PromptText),
			ai.WithTemperature(0.1), // Lowest temperature. DELTA's job is to state facts and deadlines with zero drift. There is no scenario where you want DELTA to get creative.
			ai.WithTopK(30),
			ai.WithTopP(0.80),
		),
		userId:         userId,
		conversationId: conversationId,
	}, nil
}

func (a *Delta) Start(summary string) (string, error) {
	conversationOrm := orm.Load(&models.AgentConversation{})
	defer conversationOrm.Close()

	newConversation := models.AgentConversation{
		AgentId:       models.AgentDelta,
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

func (a *Delta) Converse(user_message string) (*m.AIChatResponse, error) {
	history, err := generateAIChatHistory(a.conversationId)
	if err != nil {
		return nil, err
	}
	watchAndAppendMessages(history)
	return a.aiClient.ChatCompletionManaged(history)
}

func (a *Delta) CreateHandOff() (string, error) {
	return "", nil
}

func (a *Delta) GetAgentId() models.AgentID {
	return models.AgentDelta
}

func (a *Delta) GetConversationId() string {
	return a.conversationId
}

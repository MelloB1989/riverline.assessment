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

type Nova struct {
	aiClient       *ai.KarmaAI
	userId         string
	conversationId string
	promptVersion  int
}

func NewNova(userId, conversationId string, promptVersion int) (*Nova, error) {
	promptOrm := orm.Load(&models.PromptVersion{})
	var p []models.PromptVersion
	if err := promptOrm.GetByFieldsEquals(map[string]any{
		"AgentId":       models.AgentNova,
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
	return &Nova{
		aiClient: ai.NewKarmaAI(
			ai.Llama33_70B,
			ai.Groq,
			ai.WithMaxTokens(500),
			ai.WithSystemMessage(prompt.PromptText),
			ai.WithTemperature(0.5), // Highest temperature of the three because voice conversations are unpredictable. NOVA needs to handle objections that come in unexpected forms, respond naturally to interruptions, and vary its phrasing so it doesn't sound like a recording.
			ai.WithTopK(50),
			ai.WithTopP(0.90),
		),
		userId:         userId,
		conversationId: conversationId,
	}, nil
}

func (a *Nova) Start(summary string) (string, error) {
	conversationOrm := orm.Load(&models.AgentConversation{})
	defer conversationOrm.Close()

	newConversation := models.AgentConversation{
		AgentId:       models.AgentNova,
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

func (a *Nova) Converse(user_message string) (*m.AIChatResponse, error) {
	history, err := generateAIChatHistory(a.conversationId)
	if err != nil {
		return nil, err
	}
	watchAndAppendMessages(history)
	return a.aiClient.ChatCompletionManaged(history)
}

func (a *Nova) CreateHandOff() (string, error) {
	return "", nil
}

func (a *Nova) GetAgentId() models.AgentID {
	return models.AgentNova
}

func (a *Nova) GetConversationId() string {
	return a.conversationId
}

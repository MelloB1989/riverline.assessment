package agents

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"riverline_server/internal/models"

	"github.com/MelloB1989/karma/ai"
	karmaModels "github.com/MelloB1989/karma/models"
	"github.com/MelloB1989/karma/v2/orm"
)

func newClient(agentID models.AgentID, cfg Config) (*Client, error) {
	prompt, err := activePrompt(agentID)
	if err != nil {
		return nil, err
	}
	return &Client{
		agentID:       agentID,
		promptVersion: prompt.VersionNumber,
		prompt:        prompt.PromptText,
		aiClient: ai.NewKarmaAI(
			ai.Llama33_70B,
			ai.Groq,
			ai.WithMaxTokens(500),
			ai.WithSystemMessage(prompt.PromptText),
			ai.WithTemperature(cfg.Temperature),
			ai.WithTopK(cfg.TopK),
			ai.WithTopP(cfg.TopP),
		),
	}, nil
}

func (c *Client) AgentID() models.AgentID {
	return c.agentID
}

func (c *Client) PromptVersion() int {
	return c.promptVersion
}

func (c *Client) AssertBudget(handoff string, history []models.AgentMessage) error {
	if CountTokens(handoff) > HandoffContextBudget {
		return fmt.Errorf("handoff context exceeds %d token budget", HandoffContextBudget)
	}
	total := CountTokens(c.prompt) + CountTokens(handoff)
	for _, msg := range history {
		total += CountTokens(msg.Content)
	}
	if total > TotalContextBudget {
		return fmt.Errorf("%s context exceeds %d token budget: %d", c.agentID, TotalContextBudget, total)
	}
	return nil
}

func (c *Client) Converse(handoff string, history []models.AgentMessage) (*karmaModels.AIChatResponse, error) {
	if err := c.AssertBudget(handoff, history); err != nil {
		return nil, err
	}
	chatHistory := toKarmaHistory(handoff, history)
	resp, err := c.aiClient.ChatCompletionManaged(chatHistory)
	if err == nil && strings.TrimSpace(resp.AIResponse) != "" {
		return resp, nil
	}
	reply := fallbackReply(c.agentID, history)
	return &karmaModels.AIChatResponse{AIResponse: reply, Tokens: CountTokens(reply)}, nil
}

func activePrompt(agentID models.AgentID) (*models.PromptVersion, error) {
	o := orm.Load(&models.PromptVersion{})
	defer o.Close()
	var rows []models.PromptVersion
	if err := o.GetByFieldsEquals(map[string]any{"AgentId": agentID, "IsActive": true}).Scan(&rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New("active prompt version not found")
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].VersionNumber > rows[j].VersionNumber })
	return &rows[0], nil
}

func toKarmaHistory(handoff string, messages []models.AgentMessage) *karmaModels.AIChatHistory {
	h := &karmaModels.AIChatHistory{
		Context:  handoff,
		Messages: make([]karmaModels.AIMessage, 0, len(messages)),
	}
	for _, msg := range messages {
		role := karmaModels.User
		if msg.Role == models.MessageRoleAgent {
			role = karmaModels.Assistant
		}
		h.Messages = append(h.Messages, karmaModels.AIMessage{
			UniqueId:  msg.Id,
			Role:      role,
			Message:   msg.Content,
			Timestamp: msg.CreatedAt,
		})
	}
	return h
}

package agents

import (
	"errors"
	"sort"
	"strings"
	"time"

	"riverline_server/internal/models"

	"github.com/MelloB1989/karma/ai"
	"github.com/MelloB1989/karma/ai/parser"
	karmaModels "github.com/MelloB1989/karma/models"
	"github.com/MelloB1989/karma/v2/orm"
)

func newClient(agentID models.AgentID, cfg Config) (*Client, error) {
	prompt, err := activePrompt(agentID)
	if err != nil {
		return nil, err
	}
	return newClientWithPrompt(agentID, prompt.VersionNumber, prompt.PromptText, cfg), nil
}

func NewWithPrompt(agentID models.AgentID, promptVersion int, promptText string, cfg Config) (*Client, error) {
	if strings.TrimSpace(promptText) == "" {
		return nil, errors.New("prompt text is required")
	}
	return newClientWithPrompt(agentID, promptVersion, promptText, cfg), nil
}

func NewWithPromptVersion(agentID models.AgentID, version int, cfg Config) (*Client, error) {
	prompt, err := promptVersion(agentID, version)
	if err != nil {
		return nil, err
	}
	return newClientWithPrompt(agentID, prompt.VersionNumber, prompt.PromptText, cfg), nil
}

func newClientWithPrompt(agentID models.AgentID, promptVersion int, promptText string, cfg Config) *Client {
	if cfg.Model == "" {
		cfg.Model = ai.Llama33_70B
	}
	if cfg.Provider == "" {
		cfg.Provider = ai.Groq
	}
	modelCfg := ai.ModelConfig{BaseModel: cfg.Model, Provider: cfg.Provider}
	return &Client{
		agentID:       agentID,
		promptVersion: promptVersion,
		prompt:        promptText,
		modelID:       modelCfg.GetModelString(),
		providerID:    string(cfg.Provider),
		cfg:           cfg,
		aiClient: ai.NewKarmaAI(
			cfg.Model,
			cfg.Provider,
			ai.WithMaxTokens(500),
			ai.WithSystemMessage(promptText),
			ai.WithTemperature(cfg.Temperature),
			ai.WithTopK(cfg.TopK),
			ai.WithTopP(cfg.TopP),
		),
	}
}

func (c *Client) Clone() *Client {
	return newClientWithPrompt(c.agentID, c.promptVersion, c.prompt, c.cfg)
}

func (c *Client) AgentID() models.AgentID {
	return c.agentID
}

func (c *Client) PromptVersion() int {
	return c.promptVersion
}

func (c *Client) ModelID() string {
	return c.modelID
}

func (c *Client) ProviderID() string {
	return c.providerID
}

func (c *Client) ModelUsed() string {
	if c.providerID == "" {
		return c.modelID
	}
	return c.providerID + "/" + c.modelID
}

func (c *Client) Converse(handoff string, history []models.AgentMessage) (*karmaModels.AIChatResponse, error) {
	restoreSystemPrompt := c.useRuntimeSystemPrompt(handoff)
	defer restoreSystemPrompt()
	chatHistory := toKarmaHistory(history)
	resp, err := c.aiClient.ChatCompletionManaged(chatHistory)
	if err == nil && (strings.TrimSpace(resp.AIResponse) != "" || len(resp.ToolCalls) > 0) {
		return resp, nil
	}
	if err != nil {
		return nil, err
	}
	return nil, errors.New("empty AI response")
}

func (c *Client) useRuntimeSystemPrompt(handoff string) func() {
	previous := c.aiClient.SystemMessage
	c.aiClient.SystemMessage = c.systemPrompt(handoff)
	return func() {
		c.aiClient.SystemMessage = previous
	}
}

func (c *Client) systemPrompt(handoff string) string {
	handoff = strings.TrimSpace(handoff)
	if handoff == "" {
		return c.prompt
	}
	return c.prompt + "\n\n" + runtimeContextMessage(handoff)
}

func (c *Client) ConverseWithTools(handoff string, history []models.AgentMessage, tools ...ai.GoFunctionTool) (*karmaModels.AIChatResponse, error) {
	c.aiClient.ClearGoFunctionTools()
	defer func() {
		c.aiClient.ClearGoFunctionTools()
		c.aiClient.ToolsEnabled = false
		c.aiClient.UseMCPExecution = false
	}()
	c.aiClient.EnableTools()
	for _, tool := range tools {
		if err := c.aiClient.AddGoFunctionTool(tool); err != nil {
			return nil, err
		}
	}
	return c.Converse(handoff, history)
}

func (c *Client) GenerateText(prompt string) (*karmaModels.AIChatResponse, error) {
	resp, err := c.aiClient.GenerateFromSinglePrompt(prompt)
	if err == nil && strings.TrimSpace(resp.AIResponse) != "" {
		return resp, nil
	}
	if err != nil {
		return nil, err
	}
	return nil, errors.New("empty AI response")
}

func (c *Client) GenerateTextWithContext(handoff string, prompt string) (*karmaModels.AIChatResponse, error) {
	restoreSystemPrompt := c.useRuntimeSystemPrompt(handoff)
	defer restoreSystemPrompt()
	return c.GenerateText(prompt)
}

func (c *Client) ParseStructured(prompt string, output any) (int, error) {
	p := parser.NewParser(parser.WithAIClient(c.aiClient), parser.WithMaxRetries(2))
	_, tokens, err := p.Parse(prompt, "", output)
	return tokens, err
}

func (c *Client) ParseHandoff(prompt string, output any) (int, error) {
	return c.ParseStructured(prompt, output)
}

func MessagesForCompletion(messages []models.AgentMessage) []models.AgentMessage {
	out := make([]models.AgentMessage, 0, len(messages))
	for _, msg := range messages {
		msg.Content = messageForCompletion(msg)
		out = append(out, msg)
	}
	return out
}

const (
	ToolCreateAriaHandoff  = "create_aria_handoff"
	ToolRescheduleNovaCall = "reschedule_nova_call"
)

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

func promptVersion(agentID models.AgentID, version int) (*models.PromptVersion, error) {
	o := orm.Load(&models.PromptVersion{})
	defer o.Close()
	var rows []models.PromptVersion
	if err := o.GetByFieldsEquals(map[string]any{"AgentId": agentID, "VersionNumber": version}).Scan(&rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New("prompt version not found")
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].CreatedAt.After(rows[j].CreatedAt) })
	return &rows[0], nil
}

func toKarmaHistory(messages []models.AgentMessage) *karmaModels.AIChatHistory {
	h := &karmaModels.AIChatHistory{
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
			Message:   messageForCompletion(msg),
			Timestamp: msg.CreatedAt,
		})
	}
	return h
}

func runtimeContextMessage(context string) string {
	return "RUNTIME CONTEXT - authoritative borrower, loan, workflow, and handoff data. Use exact values from this context. Do not output field names or placeholders. If a value is present here, do not say it is unavailable.\n" + context
}

func messageForCompletion(msg models.AgentMessage) string {
	if msg.Role != models.MessageRoleBorrower {
		return msg.Content
	}
	return formatISTMessage(msg.CreatedAt, msg.Content)
}

func formatISTMessage(createdAt time.Time, content string) string {
	return "[IST " + createdAt.In(istLocation()).Format("2006-01-02 15:04:05 MST") + "] " + content
}

func istLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err == nil {
		return loc
	}
	return time.FixedZone("IST", 5*60*60+30*60)
}

package agents

import (
	"encoding/json"
	"errors"
	"fmt"
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
		cfg.Model = ai.GPTOSS_20B
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
	var lastErr error
	emptyManagedResponses := 0
	singlePromptAttempts := 0
	for attempt := 0; attempt < 5; attempt++ {
		var resp *karmaModels.AIChatResponse
		var err error
		if emptyManagedResponses >= 2 && singlePromptAttempts < 2 {
			singlePromptAttempts++
			resp, err = c.aiClient.GenerateFromSinglePrompt(flattenConversationForSinglePromptRetry(handoff, history))
		} else {
			resp, err = c.aiClient.ChatCompletionManaged(chatHistory)
		}
		if err == nil && resp != nil && (strings.TrimSpace(resp.AIResponse) != "" || len(resp.ToolCalls) > 0) {
			return resp, nil
		}
		if err != nil {
			lastErr = err
		} else if resp == nil {
			lastErr = errors.New("empty AI response")
			emptyManagedResponses++
		} else {
			lastErr = errors.New("empty AI response")
			if len(resp.ToolCalls) == 0 {
				emptyManagedResponses++
			}
		}
		if attempt < 4 {
			time.Sleep(time.Duration(attempt+1) * 300 * time.Millisecond)
		}
	}
	return nil, lastErr
}

func flattenConversationForSinglePromptRetry(handoff string, history []models.AgentMessage) string {
	var b strings.Builder
	if strings.TrimSpace(handoff) != "" {
		b.WriteString("Runtime context:\n")
		b.WriteString(strings.TrimSpace(handoff))
		b.WriteString("\n\n")
	}
	b.WriteString("Conversation so far:\n")
	for _, msg := range history {
		label := "Borrower"
		if msg.Role == models.MessageRoleAgent {
			label = "Agent"
		}
		content := strings.TrimSpace(messageForCompletion(msg))
		if content == "" {
			continue
		}
		b.WriteString(label)
		b.WriteString(": ")
		b.WriteString(content)
		b.WriteByte('\n')
	}
	b.WriteString("\nReturn only the next Agent reply. Do not include labels.")
	return b.String()
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
	previousToolsEnabled := c.aiClient.ToolsEnabled
	previousUseMCPExecution := c.aiClient.UseMCPExecution
	previousMaxTokens := c.aiClient.MaxTokens
	c.aiClient.ClearGoFunctionTools()
	defer func() {
		c.aiClient.ClearGoFunctionTools()
		c.aiClient.ToolsEnabled = previousToolsEnabled
		c.aiClient.UseMCPExecution = previousUseMCPExecution
		c.aiClient.MaxTokens = previousMaxTokens
	}()
	if c.aiClient.MaxTokens < toolCallMaxTokens {
		c.aiClient.MaxTokens = toolCallMaxTokens
	}
	c.aiClient.EnableTools()
	c.aiClient.UseMCPExecution = true
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

func (c *Client) GenerateTextViaChat(prompt string) (*karmaModels.AIChatResponse, error) {
	history := &karmaModels.AIChatHistory{
		Messages: []karmaModels.AIMessage{{
			UniqueId:  "prompt-generation",
			Role:      karmaModels.User,
			Message:   prompt,
			Timestamp: time.Now().UTC(),
		}},
	}
	resp, err := c.aiClient.ChatCompletionManaged(history)
	if err == nil && strings.TrimSpace(resp.AIResponse) != "" {
		return resp, nil
	}
	if err != nil {
		return nil, err
	}
	return nil, errors.New("empty AI response")
}

func (c *Client) GenerateTextReliable(prompt string, attempts int) (*karmaModels.AIChatResponse, error) {
	if attempts <= 0 {
		attempts = 3
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		resp, err := c.GenerateText(prompt)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		resp, err = c.GenerateTextViaChat(prompt)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if attempt < attempts-1 {
			time.Sleep(time.Duration(attempt+1) * 300 * time.Millisecond)
		}
	}
	return nil, lastErr
}

func (c *Client) GenerateTextWithTemporarySystem(systemPrompt string, prompt string, attempts int) (*karmaModels.AIChatResponse, error) {
	previous := c.aiClient.SystemMessage
	c.aiClient.SystemMessage = systemPrompt
	defer func() {
		c.aiClient.SystemMessage = previous
	}()
	return c.GenerateTextReliable(prompt, attempts)
}

func (c *Client) GenerateTextWithContext(handoff string, prompt string) (*karmaModels.AIChatResponse, error) {
	restoreSystemPrompt := c.useRuntimeSystemPrompt(handoff)
	defer restoreSystemPrompt()
	return c.GenerateTextReliable(prompt, 6)
}

func (c *Client) ParseStructured(prompt string, output any) (int, error) {
	previous := c.aiClient.SystemMessage
	c.aiClient.SystemMessage = "You are Riverline's internal structured-output generator for the active agent model. Return only valid JSON matching the requested schema. Do not speak to the borrower, do not roleplay, and do not include explanations or markdown."
	defer func() {
		c.aiClient.SystemMessage = previous
	}()
	p := parser.NewParser(parser.WithAIClient(c.aiClient), parser.WithMaxRetries(6))
	_, tokens, err := p.Parse(prompt, "", output)
	if err == nil {
		return tokens, nil
	}
	repairTokens, repairErr := c.parseStructuredViaChat(prompt, output)
	if repairErr == nil {
		return repairTokens, nil
	}
	return tokens + repairTokens, fmt.Errorf("%w; JSON repair parse failed: %v", err, repairErr)
}

func (c *Client) ParseHandoff(prompt string, output any) (int, error) {
	return c.ParseStructured(prompt, output)
}

func (c *Client) parseStructuredViaChat(prompt string, output any) (int, error) {
	currentPrompt := prompt + "\n\nReturn only valid JSON. No markdown. No explanation."
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		resp, err := c.GenerateTextViaChat(currentPrompt)
		if err != nil {
			lastErr = err
			continue
		}
		if err := json.Unmarshal([]byte(extractJSONObject(resp.AIResponse)), output); err == nil {
			return resp.InputTokens + resp.OutputTokens, nil
		} else {
			lastErr = err
			currentPrompt = fmt.Sprintf("Fix this JSON. Return only valid JSON matching the requested schema.\nError: %v\nBad response:\n%s", err, resp.AIResponse)
		}
	}
	return 0, lastErr
}

func extractJSONObject(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "```json")
	value = strings.TrimPrefix(value, "```")
	value = strings.TrimSuffix(value, "```")
	value = strings.TrimSpace(value)
	start := strings.Index(value, "{")
	end := strings.LastIndex(value, "}")
	if start >= 0 && end >= start {
		return value[start : end+1]
	}
	return value
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
	toolCallMaxTokens      = 1200
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

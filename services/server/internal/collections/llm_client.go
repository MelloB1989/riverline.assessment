package collections

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type LlmMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model       string       `json:"model"`
	MaxTokens   int          `json:"max_tokens"`
	System      string       `json:"system,omitempty"`
	Messages    []LlmMessage `json:"messages"`
	Temperature float64      `json:"temperature,omitempty"`
	Stream      bool         `json:"stream,omitempty"`
}

type anthropicContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Thinking string `json:"thinking,omitempty"`
}

type anthropicResponse struct {
	Content []anthropicContentBlock `json:"content"`
	Error   *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
	Type string `json:"type"`
}

type LlmChatResult struct {
	Content      string `json:"content"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	TotalTokens  int    `json:"total_tokens"`
	Model        string `json:"model"`
	StopReason   string `json:"stop_reason"`
}

type LlmClient struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

func NewLLMClient(baseURL, apiKey, model string) *LlmClient {
	return &LlmClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

func (c *LlmClient) Chat(ctx context.Context, messages []LlmMessage, temperature float64) (string, error) {
	return c.ChatWithTokens(ctx, messages, temperature, 4096)
}

func (c *LlmClient) ChatWithTokens(ctx context.Context, messages []LlmMessage, temperature float64, maxTokens int) (string, error) {
	result, err := c.ChatWithTokenUsage(ctx, messages, temperature, maxTokens)
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

func (c *LlmClient) ChatWithTokenUsage(ctx context.Context, messages []LlmMessage, temperature float64, maxTokens int) (*LlmChatResult, error) {
	return c.chatWithModelUsage(ctx, c.model, messages, temperature, maxTokens)
}

func (c *LlmClient) chatWithModelUsage(ctx context.Context, model string, messages []LlmMessage, temperature float64, maxTokens int) (*LlmChatResult, error) {
	var system string
	var userMsgs []LlmMessage
	for _, m := range messages {
		if m.Role == "system" {
			if system != "" {
				system += "\n"
			}
			system += m.Content
		} else {
			userMsgs = append(userMsgs, m)
		}
	}

	body := anthropicRequest{
		Model:     model,
		MaxTokens: maxTokens,
		System:    system,
		Messages:  userMsgs,
		Stream:    true,
	}
	if temperature > 0 {
		body.Temperature = temperature
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Claude API returned %d: %s", resp.StatusCode, string(respBody))
	}

	return c.readSSEStream(resp.Body, model)
}

func (c *LlmClient) readSSEStream(body io.Reader, model string) (*LlmChatResult, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	var textContent strings.Builder
	inputTokens := 0
	outputTokens := 0
	stopReason := ""

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		if data == "[DONE]" {
			break
		}

		var event struct {
			Type  string `json:"type"`
			Index int    `json:"index"`
			Delta *struct {
				Type       string `json:"type,omitempty"`
				Text       string `json:"text,omitempty"`
				StopReason string `json:"stop_reason,omitempty"`
			} `json:"delta,omitempty"`
			ContentBlock *struct {
				Type string `json:"type"`
			} `json:"content_block,omitempty"`
			Message *struct {
				Usage *struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage,omitempty"`
			} `json:"message,omitempty"`
			Usage *struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage,omitempty"`
			Error *struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error,omitempty"`
		}

		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		if event.Error != nil {
			return nil, fmt.Errorf("Claude stream error: %s", event.Error.Message)
		}

		if event.Type == "content_block_delta" && event.Delta != nil && event.Delta.Type == "text_delta" {
			textContent.WriteString(event.Delta.Text)
		}
		if event.Delta != nil && event.Delta.StopReason != "" {
			stopReason = event.Delta.StopReason
		}
		if event.Message != nil && event.Message.Usage != nil {
			if event.Message.Usage.InputTokens > 0 {
				inputTokens = event.Message.Usage.InputTokens
			}
			if event.Message.Usage.OutputTokens > outputTokens {
				outputTokens = event.Message.Usage.OutputTokens
			}
		}
		if event.Usage != nil {
			if event.Usage.InputTokens > 0 {
				inputTokens = event.Usage.InputTokens
			}
			if event.Usage.OutputTokens > outputTokens {
				outputTokens = event.Usage.OutputTokens
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("stream read error: %w", err)
	}

	result := textContent.String()
	if result == "" {
		return nil, fmt.Errorf("Claude API returned no text content")
	}

	return &LlmChatResult{Content: result, InputTokens: inputTokens, OutputTokens: outputTokens, TotalTokens: inputTokens + outputTokens, Model: model, StopReason: stopReason}, nil
}

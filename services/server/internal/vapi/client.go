package vapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type HandoffContext struct {
	WorkflowID     string         `json:"workflow_id"`
	AriaSummary    string         `json:"aria_summary"`
	ContextForNova string         `json:"context_for_nova"`
	Offers         map[string]any `json:"offers"`
}

type CallDetails struct {
	ID              string
	Status          string
	EndedReason     string
	Transcript      string
	Summary         string
	RecordingURL    string
	DurationSeconds *int
}

type Client struct {
	APIKey        string
	BaseURL       string
	PhoneNumberID string
	AssistantID   string
	DryRun        bool
	HTTPClient    *http.Client
}

func New(apiKey, baseURL, phoneNumberID, assistantID string, dryRun bool) *Client {
	if baseURL == "" {
		baseURL = "https://api.vapi.ai"
	}
	return &Client{
		APIKey:        apiKey,
		BaseURL:       strings.TrimRight(baseURL, "/"),
		PhoneNumberID: phoneNumberID,
		AssistantID:   assistantID,
		DryRun:        dryRun,
		HTTPClient:    &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *Client) StartCall(ctx context.Context, phone string, context HandoffContext) (string, error) {
	if c.DryRun || c.APIKey == "" || phone == "" || isReservedTestPhone(phone) {
		return "mock-vapi-" + time.Now().UTC().Format("20060102150405"), nil
	}
	body := map[string]any{
		"phoneNumberId": c.PhoneNumberID,
		"assistantId":   c.AssistantID,
		"customer": map[string]any{
			"number": phone,
		},
		"assistantOverrides": map[string]any{
			"variableValues": map[string]any{
				"workflow_id":      context.WorkflowID,
				"aria_summary":     context.AriaSummary,
				"context_for_nova": context.ContextForNova,
				"offers":           context.Offers,
			},
		},
		"metadata": map[string]any{
			"source":      "riverline",
			"workflow_id": context.WorkflowID,
		},
	}
	resp, err := c.do(ctx, http.MethodPost, "/call", body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if id, ok := payload["id"].(string); ok && id != "" {
		return id, nil
	}
	return "", fmt.Errorf("vapi response missing id")
}

func isReservedTestPhone(phone string) bool {
	compact := strings.ReplaceAll(strings.TrimSpace(phone), " ", "")
	return strings.HasPrefix(compact, "+1555555")
}

func (c *Client) GetTranscript(ctx context.Context, callID string) (string, error) {
	payload, err := c.getCall(ctx, callID)
	if err != nil {
		return "", err
	}
	for _, key := range []string{"transcript", "summary"} {
		if v, ok := payload[key].(string); ok {
			return v, nil
		}
	}
	return "", nil
}

func (c *Client) GetCallDetails(ctx context.Context, callID string) (*CallDetails, error) {
	payload, err := c.getCall(ctx, callID)
	if err != nil {
		return nil, err
	}
	details := &CallDetails{
		ID:          firstPayloadString(payload, "id"),
		Status:      firstPayloadString(payload, "status"),
		EndedReason: firstPayloadString(payload, "endedReason", "ended_reason"),
		Transcript:  firstPayloadString(payload, "transcript"),
		Summary:     firstPayloadString(payload, "summary"),
		RecordingURL: firstPayloadString(payload,
			"recordingUrl",
			"stereoRecordingUrl",
			"recording_url",
			"stereo_recording_url",
		),
		DurationSeconds: firstPayloadInt(payload, "durationSeconds", "duration_seconds"),
	}
	if details.Transcript == "" {
		details.Transcript = details.Summary
	}
	return details, nil
}

func (c *Client) GetRecordingURL(ctx context.Context, callID string) (string, error) {
	payload, err := c.getCall(ctx, callID)
	if err != nil {
		return "", err
	}
	for _, key := range []string{"recordingUrl", "stereoRecordingUrl"} {
		if v, ok := payload[key].(string); ok {
			return v, nil
		}
	}
	return "", nil
}

func firstPayloadString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := payload[key].(string); ok {
			return value
		}
	}
	return ""
}

func firstPayloadInt(payload map[string]any, keys ...string) *int {
	for _, key := range keys {
		switch value := payload[key].(type) {
		case int:
			return &value
		case int64:
			v := int(value)
			return &v
		case float64:
			v := int(value)
			return &v
		}
	}
	return nil
}

func (c *Client) getCall(ctx context.Context, callID string) (map[string]any, error) {
	if c.APIKey == "" {
		return map[string]any{}, nil
	}
	resp, err := c.do(ctx, http.MethodGet, "/call/"+callID, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func (c *Client) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("vapi %s %s failed: %s: %s", method, path, resp.Status, string(data))
	}
	return resp, nil
}

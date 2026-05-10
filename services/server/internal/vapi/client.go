package vapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const NovaStructuredOutputName = "Riverline NOVA Handoff"

type HandoffContext struct {
	WorkflowID           string `json:"workflow_id"`
	BorrowerFirstName    string `json:"borrower_first_name"`
	AccountNumberPartial string `json:"account_number_partial"`
	ContextForNova       string `json:"context_for_nova"`
	CurrentISTTimestamp  string `json:"current_ist_timestamp"`
	CurrentUTCTimestamp  string `json:"current_utc_timestamp"`
}

type CallDetails struct {
	ID               string
	Status           string
	EndedReason      string
	Transcript       string
	Summary          string
	RecordingURL     string
	DurationSeconds  *int
	StructuredOutput map[string]any
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
	customer := map[string]any{"number": phone}
	body := map[string]any{
		"phoneNumberId": c.PhoneNumberID,
		"assistantId":   c.AssistantID,
		"customer":      customer,
		"assistantOverrides": map[string]any{
			"variableValues": map[string]any{
				"workflow_id":            context.WorkflowID,
				"borrower_first_name":    context.BorrowerFirstName,
				"account_number_partial": context.AccountNumberPartial,
				"context_for_nova":       context.ContextForNova,
				"current_ist_timestamp":  context.CurrentISTTimestamp,
				"current_utc_timestamp":  context.CurrentUTCTimestamp,
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
	details.StructuredOutput = ExtractNovaStructuredOutput(payload)
	return details, nil
}

func (c *Client) SyncNovaAssistant(ctx context.Context, systemPrompt string) (string, error) {
	if c.DryRun || c.APIKey == "" || c.AssistantID == "" {
		return "", nil
	}
	structuredOutputID, err := c.EnsureNovaStructuredOutput(ctx)
	if err != nil {
		return "", err
	}
	assistant, err := c.getAssistant(ctx)
	if err != nil {
		return "", err
	}
	model, _ := assistant["model"].(map[string]any)
	if model == nil {
		model = map[string]any{}
	}
	model["messages"] = []map[string]string{{"role": "system", "content": systemPrompt}}
	model["tools"] = []any{}
	model["toolIds"] = []string{}
	artifactPlan, _ := assistant["artifactPlan"].(map[string]any)
	if artifactPlan == nil {
		artifactPlan = map[string]any{}
	}
	artifactPlan["structuredOutputIds"] = appendUniqueStringPayload(artifactPlan["structuredOutputIds"], structuredOutputID)
	body := map[string]any{
		"model":                  model,
		"artifactPlan":           artifactPlan,
		"firstMessage":           novaFirstMessageTemplate(),
		"firstMessageMode":       "assistant-speaks-first",
		"voicemailMessage":       novaVoicemailMessageTemplate(),
		"endCallFunctionEnabled": false,
		"endCallMessage":         "",
		"endCallPhrases":         []string{},
	}
	resp, err := c.do(ctx, http.MethodPatch, "/assistant/"+c.AssistantID, body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	return structuredOutputID, nil
}

func (c *Client) EnsureNovaStructuredOutput(ctx context.Context) (string, error) {
	existing, err := c.findStructuredOutputByName(ctx, NovaStructuredOutputName)
	if err != nil {
		return "", err
	}
	if existing != nil {
		id := firstPayloadString(existing, "id")
		if id == "" {
			return "", fmt.Errorf("vapi structured output %q missing id", NovaStructuredOutputName)
		}
		if err := c.updateStructuredOutput(ctx, id); err != nil {
			return "", err
		}
		return id, nil
	}
	resp, err := c.do(ctx, http.MethodPost, "/structured-output", novaStructuredOutputPayload(c.AssistantID))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	id := firstPayloadString(payload, "id")
	if id == "" {
		return "", fmt.Errorf("vapi structured output create response missing id")
	}
	return id, nil
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
	if c.APIKey == "" || c.DryRun || strings.HasPrefix(callID, "mock-vapi-") {
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

func (c *Client) getAssistant(ctx context.Context) (map[string]any, error) {
	resp, err := c.do(ctx, http.MethodGet, "/assistant/"+c.AssistantID, nil)
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

func (c *Client) findStructuredOutputByName(ctx context.Context, name string) (map[string]any, error) {
	path := "/structured-output?limit=100&name=" + url.QueryEscape(name)
	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var payload any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	for _, item := range payloadItems(payload) {
		if strings.EqualFold(firstPayloadString(item, "name"), name) {
			return item, nil
		}
	}
	return nil, nil
}

func (c *Client) updateStructuredOutput(ctx context.Context, id string) error {
	resp, err := c.do(ctx, http.MethodPatch, "/structured-output/"+id+"?schemaOverride=true", novaStructuredOutputPayload(c.AssistantID))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
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

func appendUniqueStringPayload(existing any, value string) []string {
	out := []string{}
	switch v := existing.(type) {
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
	case []string:
		out = append(out, v...)
	}
	for _, item := range out {
		if item == value {
			return out
		}
	}
	if value != "" {
		out = append(out, value)
	}
	return out
}

func payloadItems(payload any) []map[string]any {
	switch v := payload.(type) {
	case []any:
		items := make([]map[string]any, 0, len(v))
		for _, item := range v {
			if obj, ok := item.(map[string]any); ok {
				items = append(items, obj)
			}
		}
		return items
	case map[string]any:
		for _, key := range []string{"data", "items", "results"} {
			if items, ok := v[key].([]any); ok {
				return payloadItems(items)
			}
		}
	}
	return nil
}

func novaFirstMessageTemplate() string {
	return "Hello {{borrower_first_name}}, this is Riverline calling about your loan account ending {{account_number_partial}}. For transparency, I am an AI assistant and this call may be recorded. Is this a good time for me to lay out the repayment options now?"
}

func novaVoicemailMessageTemplate() string {
	return "Hello, this is Riverline calling about your loan account. Please call us back at your earliest convenience."
}

func novaStructuredOutputPayload(assistantID string) map[string]any {
	return map[string]any{
		"name":        NovaStructuredOutputName,
		"type":        "ai",
		"description": "Extract NOVA collections call outcome, accepted offer, and borrower objections.",
		"schema":      novaStructuredOutputSchema(),
		"assistantIds": []string{
			assistantID,
		},
	}
}

func novaStructuredOutputSchema() map[string]any {
	nullableString := []string{"string", "null"}
	nullableNumber := []string{"number", "null"}
	nullableInteger := []string{"integer", "null"}
	nullableBoolean := []string{"boolean", "null"}
	return map[string]any{
		"type":        "object",
		"description": "NOVA call completion handoff for Riverline collections workflow.",
		"properties": map[string]any{
			"offer_accepted": map[string]any{
				"type":        nullableBoolean,
				"description": "Whether the borrower accepted a specific repayment offer after exact terms were presented. A yes to call availability is not offer acceptance. Null when no offer terms were presented or the call did not reach a decision.",
			},
			"accepted_offer_type": map[string]any{
				"type":        nullableString,
				"description": "Accepted offer category, for example lump_sum, emi, hardship, or null.",
			},
			"objections_raised": map[string]any{
				"type":        "array",
				"description": "Specific objections or concerns the borrower raised.",
				"items":       map[string]any{"type": "string"},
			},
			"outcome": map[string]any{
				"type":        nullableString,
				"enum":        []any{"committed", "rejected", "no_response", "hardship", "stop_contact", "escalated", nil},
				"description": "Final call outcome. Use committed only after exact offer terms were presented and accepted. Use no_response if the call ended before an offer was presented.",
			},
			"aria_summary": map[string]any{
				"type":        "string",
				"description": "Updated memory summary preserving ARIA context plus relevant NOVA call facts.",
			},
			"final_offer_amount": map[string]any{
				"type":        nullableNumber,
				"description": "Final amount to use for DELTA or commitment confirmation when applicable.",
			},
			"final_offer_deadline_hours": map[string]any{
				"type":        nullableInteger,
				"description": "Deadline window in hours for the final offer, when applicable.",
			},
		},
		"required": []string{"offer_accepted", "objections_raised", "outcome", "aria_summary"},
	}
}

func NovaSystemPrompt(basePrompt string) string {
	return strings.TrimSpace(basePrompt) + `

[Vapi Voice Runtime Context]
You are speaking with the borrower over {{transport.conversationType}}. Treat the following dynamic variables as the only authoritative runtime context for this exact NOVA call:
- Current IST timestamp: {{current_ist_timestamp}}
- Workflow ID: {{workflow_id}}
- NOVA context summary: {{context_for_nova}}

[Voice Requirements]
- Use only the NOVA context summary for borrower/account facts, ARIA handoff facts, and exact offer terms.
- Do not say that loan amount, overdue days, account context, or offer details are unavailable when they are present in the NOVA context summary.
- The borrower saying yes to the opening good-time question is only permission to continue. It is not acceptance of an offer.
- After the borrower says it is a good time, your next substantive turn must present the exact primary payment option from the NOVA context summary, including amount, timing/deadline, and required borrower action.
- Do not end the call, promise an email, or classify the call as complete until you have presented at least one exact payment option and the borrower has accepted, rejected, raised hardship, requested no contact, or failed to engage after the offer was attempted.
- Speak naturally for a phone call; do not use Markdown.
- Ask one question at a time and keep turns concise.
- If the borrower disputes identity, requests no contact, reports hardship, accepts an offer, rejects all offers, or the call should end, close politely. The backend will consume Vapi structured outputs after the call ends.`
}

func ExtractNovaStructuredOutput(payloads ...map[string]any) map[string]any {
	for _, payload := range payloads {
		if result := extractNovaStructuredOutput(payload); len(result) > 0 {
			return result
		}
	}
	return nil
}

func extractNovaStructuredOutput(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	for _, path := range []string{"artifact.structuredOutputs", "analysis.structuredOutputs", "structuredOutputs"} {
		if result := extractNovaStructuredOutputFromValue(nestedAny(payload, path)); len(result) > 0 {
			return result
		}
	}
	if result := extractNovaStructuredOutputFromValue(payload); len(result) > 0 {
		return result
	}
	return nil
}

func extractNovaStructuredOutputFromValue(value any) map[string]any {
	switch v := value.(type) {
	case map[string]any:
		if name, _ := v["name"].(string); strings.EqualFold(name, NovaStructuredOutputName) {
			if result, ok := v["result"].(map[string]any); ok {
				return result
			}
		}
		if result, ok := v["result"].(map[string]any); ok && looksLikeNovaHandoff(result) {
			return result
		}
		if looksLikeNovaHandoff(v) {
			return v
		}
		for _, child := range v {
			if result := extractNovaStructuredOutputFromValue(child); len(result) > 0 {
				return result
			}
		}
	case []any:
		for _, item := range v {
			if result := extractNovaStructuredOutputFromValue(item); len(result) > 0 {
				return result
			}
		}
	}
	return nil
}

func looksLikeNovaHandoff(value map[string]any) bool {
	_, hasOutcome := value["outcome"]
	_, hasAccepted := value["offer_accepted"]
	return hasOutcome && hasAccepted
}

func nestedAny(m map[string]any, path string) any {
	var cur any = m
	for _, part := range strings.Split(path, ".") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = obj[part]
	}
	return cur
}

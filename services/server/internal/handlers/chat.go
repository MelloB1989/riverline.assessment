package handlers

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"riverline_server/constants"
	"riverline_server/internal/collections"
	"riverline_server/internal/models"
	"riverline_server/internal/workflows"

	"github.com/MelloB1989/karma/v2/orm"
	"github.com/gofiber/fiber/v2"
	"go.temporal.io/sdk/client"
)

type startWorkflowRequest struct {
	UserID string `json:"user_id"`
	LoanID string `json:"loan_id"`
}

type chatRequest struct {
	Message string `json:"message"`
}

type vapiWebhook struct {
	Type       string         `json:"type"`
	Message    map[string]any `json:"message"`
	Call       map[string]any `json:"call"`
	WorkflowID string         `json:"workflow_id"`
	CallID     string         `json:"call_id"`
	Transcript string         `json:"transcript"`
	Recording  string         `json:"recording_url"`
}

func StartWorkflow(c *fiber.Ctx) error {
	var req startWorkflowRequest
	_ = c.BodyParser(&req)
	wf, err := collections.StartWorkflow(req.UserID, req.LoanID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	temporalClient, err := temporalClient()
	if err != nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, err.Error())
	}
	defer temporalClient.Close()
	_, err = temporalClient.ExecuteWorkflow(c.Context(), client.StartWorkflowOptions{
		ID:        wf.Id,
		TaskQueue: workflows.BorrowerCollectionsTaskQueue,
	}, workflows.BorrowerCollectionsWorkflow, workflows.BorrowerWorkflowInput{WorkflowID: wf.Id, UserID: wf.UserId, LoanID: wf.LoanId})
	if err != nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, err.Error())
	}
	return c.JSON(fiber.Map{"workflow": wf})
}

func GetWorkflow(c *fiber.Ctx) error {
	wf, err := collections.GetWorkflow(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	return c.JSON(fiber.Map{"workflow": wf})
}

func PostChat(c *fiber.Ctx) error {
	var req chatRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	resp, err := collections.HandleChat(c.Params("workflowId"), req.Message)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	if resp.StageComplete {
		signalName := "aria_complete"
		if resp.Conversation.AgentId == "delta" {
			signalName = "delta_complete"
		}
		if err := signalWorkflow(c, c.Params("workflowId"), signalName, nil); err != nil {
			return fiber.NewError(fiber.StatusServiceUnavailable, err.Error())
		}
	}
	return c.JSON(resp)
}

func GetConversation(c *fiber.Ctx) error {
	view, err := collections.ConversationByIDOrWorkflow(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	return c.JSON(view)
}

func StreamChat(c *fiber.Ctx) error {
	workflowID := c.Params("workflowId")
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for i := 0; i < 15; i++ {
			view, err := collections.ConversationByIDOrWorkflow(workflowID)
			if err == nil {
				data, _ := json.Marshal(view.Messages)
				_, _ = fmt.Fprintf(w, "event: messages\ndata: %s\n\n", data)
				_ = w.Flush()
			}
			<-ticker.C
		}
	})
	return nil
}

func VapiWebhook(c *fiber.Ctx) error {
	if secret := constants.AppCfg.Get().VapiWebhookSecret; secret != "" && c.Get("x-vapi-secret") != secret {
		return fiber.NewError(fiber.StatusUnauthorized, "invalid vapi webhook secret")
	}
	var event vapiWebhook
	if err := c.BodyParser(&event); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	workflowID := firstString(event.WorkflowID, nestedString(event.Message, "workflow_id"), nestedString(event.Call, "workflow_id"), nestedString(event.Call, "metadata.workflow_id"))
	callID := firstString(event.CallID, nestedString(event.Message, "call.id"), nestedString(event.Call, "id"), nestedString(event.Message, "callId"))
	transcript := firstString(event.Transcript, nestedString(event.Message, "transcript"), nestedString(event.Call, "transcript"))
	recordingURL := firstString(event.Recording, nestedString(event.Message, "recordingUrl"), nestedString(event.Call, "recordingUrl"))
	eventType := firstString(event.Type, nestedString(event.Message, "type"))

	if workflowID == "" {
		return c.JSON(fiber.Map{"ok": true, "ignored": "workflow_id missing"})
	}
	if strings.Contains(eventType, "ended") || eventType == "call.ended" || transcript != "" {
		signal := workflows.NovaCompleteSignal{CallID: callID, Transcript: transcript, RecordingURL: recordingURL}
		if err := signalWorkflow(c, workflowID, "nova_complete", signal); err != nil {
			return fiber.NewError(fiber.StatusServiceUnavailable, err.Error())
		}
	}
	return c.JSON(fiber.Map{"ok": true})
}

func AdminEvalSummary(c *fiber.Ctx) error {
	scoreOrm := orm.Load(&models.ConversationScore{})
	defer scoreOrm.Close()
	expOrm := orm.Load(&models.PromptExperiment{})
	defer expOrm.Close()
	costOrm := orm.Load(&models.LlmCostLog{})
	defer costOrm.Close()

	var scores []models.ConversationScore
	if err := scoreOrm.GetAll().Scan(&scores); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	var experiments []models.PromptExperiment
	if err := expOrm.GetAll().Scan(&experiments); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	var costs []models.LlmCostLog
	if err := costOrm.GetAll().Scan(&costs); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	totalCost := 0.0
	for _, row := range costs {
		totalCost += row.CostUsd
	}
	return c.JSON(fiber.Map{
		"conversation_scores": scores,
		"prompt_experiments":  experiments,
		"cost_log":            costs,
		"total_cost_usd":      totalCost,
	})
}

func signalWorkflow(c *fiber.Ctx, workflowID, signalName string, payload any) error {
	temporalClient, err := temporalClient()
	if err != nil {
		return err
	}
	defer temporalClient.Close()
	if payload == nil {
		payload = struct{}{}
	}
	return temporalClient.SignalWorkflow(c.Context(), workflowID, "", signalName, payload)
}

func temporalClient() (client.Client, error) {
	return client.Dial(client.Options{HostPort: constants.AppCfg.Get().TemporalHostPort})
}

func firstString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func nestedString(m map[string]any, path string) string {
	if m == nil {
		return ""
	}
	var cur any = m
	for _, part := range strings.Split(path, ".") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = obj[part]
	}
	if s, ok := cur.(string); ok {
		return s
	}
	return ""
}

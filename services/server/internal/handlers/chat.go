package handlers

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"riverline_server/constants"
	"riverline_server/internal/collections"
	rivereval "riverline_server/internal/eval"
	"riverline_server/internal/middleware"
	"riverline_server/internal/models"
	"riverline_server/internal/temporalclient"
	vapiclient "riverline_server/internal/vapi"
	"riverline_server/internal/workflows"

	"github.com/MelloB1989/karma/utils"
	"github.com/MelloB1989/karma/v2/orm"
	"github.com/gofiber/fiber/v2"
	"go.temporal.io/sdk/client"
)

type startWorkflowRequest struct {
	LoanID string `json:"loan_id"`
}

type chatRequest struct {
	Message string `json:"message"`
}

type adminPromptExperimentRequest struct {
	AgentID          models.AgentID                   `json:"agent_id"`
	Seed             int64                            `json:"seed"`
	BatchSize        int                              `json:"batch_size"`
	Personas         []models.Persona                 `json:"personas"`
	MaxTurnsPerAgent int                              `json:"max_turns_per_agent"`
	Judges           []constants.EvaluatorJudgeConfig `json:"judges"`
}

type adminMetaEvaluationRequest struct {
	AgentID models.AgentID `json:"agent_id"`
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

type adminEvalRun struct {
	ID          string
	Status      string
	Config      rivereval.FullCycleConfig
	StartedAt   time.Time
	CompletedAt *time.Time
	Error       *string
}

type adminEvalRunSnapshot struct {
	ID          string                    `json:"id"`
	Status      string                    `json:"status"`
	Config      rivereval.FullCycleConfig `json:"config"`
	StartedAt   time.Time                 `json:"started_at"`
	CompletedAt *time.Time                `json:"completed_at"`
	Error       *string                   `json:"error"`
}

type adminConversationPreview struct {
	Conversation models.AgentConversation  `json:"conversation"`
	Messages     []models.AgentMessage     `json:"messages"`
	Score        *models.ConversationScore `json:"score,omitempty"`
}

type adminEvalProgress struct {
	Run             *adminEvalRunSnapshot      `json:"run"`
	Counts          map[string]int             `json:"counts"`
	TotalCostUSD    float64                    `json:"total_cost_usd"`
	RecentScores    []models.ConversationScore `json:"recent_scores"`
	Experiments     []models.PromptExperiment  `json:"experiments"`
	Conversations   []adminConversationPreview `json:"conversations"`
	LastGeneratedAt time.Time                  `json:"last_generated_at"`
}

var adminEvalRuns = struct {
	sync.Mutex
	latest string
	runs   map[string]*adminEvalRun
}{runs: map[string]*adminEvalRun{}}

func StartWorkflow(c *fiber.Ctx) error {
	var req startWorkflowRequest
	_ = c.BodyParser(&req)
	userID := middleware.GetUserID(c)
	if userID == "" {
		return fiber.NewError(fiber.StatusUnauthorized, "missing authenticated user")
	}
	if err := collections.EnsureUserFromAuth(userID, middleware.GetUserEmail(c), middleware.GetUserFirstName(c), middleware.GetUserLastName(c), middleware.GetUserName(c)); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if active, err := collections.ActiveWorkflowForUser(userID); err == nil {
		return c.JSON(fiber.Map{"workflow": active, "existing": true})
	} else if !errors.Is(err, collections.ErrActiveWorkflowNotFound) {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	wf, err := collections.StartWorkflow(userID, req.LoanID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"workflow": wf, "existing": false})
}

func GetWorkflow(c *fiber.Ctx) error {
	wf, err := collections.GetWorkflow(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	if wf.UserId != middleware.GetUserID(c) {
		return fiber.NewError(fiber.StatusForbidden, "workflow does not belong to authenticated user")
	}
	return c.JSON(fiber.Map{"workflow": wf})
}

func PostChat(c *fiber.Ctx) error {
	var req chatRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	wf, err := collections.GetWorkflow(c.Params("workflowId"))
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	if wf.UserId != middleware.GetUserID(c) {
		return fiber.NewError(fiber.StatusForbidden, "workflow does not belong to authenticated user")
	}
	resp, err := collections.HandleChat(wf.Id, req.Message)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	if resp.StageComplete && resp.Conversation.AgentId == models.AgentAria {
		workflowID := c.Params("workflowId")
		if err := startTemporalWorkflow(c, workflowID+"-aria-handoff", workflows.AriaHandoffWorkflow, workflowID); err != nil {
			return fiber.NewError(fiber.StatusServiceUnavailable, err.Error())
		}
	}
	if resp.NovaCallRescheduled && resp.NovaScheduledCallAt != nil {
		workflowID := c.Params("workflowId")
		signal := workflows.RescheduleNovaCallSignal{ScheduledCallAt: *resp.NovaScheduledCallAt, Reason: derefString(resp.NovaRescheduleReason)}
		if err := signalTemporalWorkflow(c, workflowID+"-aria-handoff", workflows.RescheduleNovaCallSignalName, signal); err != nil {
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
	if view.Workflow.UserId != middleware.GetUserID(c) {
		return fiber.NewError(fiber.StatusForbidden, "conversation does not belong to authenticated user")
	}
	return c.JSON(view)
}

func GetDeltaHandoff(c *fiber.Ctx) error {
	export, err := collections.DeltaHandoffForWorkflow(c.Params("workflowId"))
	if err != nil {
		if errors.Is(err, collections.ErrDeltaHandoffUnavailable) {
			return fiber.NewError(fiber.StatusNotFound, err.Error())
		}
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	if export.UserID != middleware.GetUserID(c) {
		return fiber.NewError(fiber.StatusForbidden, "workflow does not belong to authenticated user")
	}
	return c.JSON(fiber.Map{"handoff": export})
}

func GetDeltaHandoffPDF(c *fiber.Ctx) error {
	export, err := collections.DeltaHandoffForWorkflow(c.Params("workflowId"))
	if err != nil {
		if errors.Is(err, collections.ErrDeltaHandoffUnavailable) {
			return fiber.NewError(fiber.StatusNotFound, err.Error())
		}
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	if export.UserID != middleware.GetUserID(c) {
		return fiber.NewError(fiber.StatusForbidden, "workflow does not belong to authenticated user")
	}
	pdf, err := collections.DeltaHandoffPDF(export.WorkflowID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	c.Set("Content-Type", "application/pdf")
	c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="riverline-delta-handoff-%s.pdf"`, export.WorkflowID))
	return c.Send(pdf)
}

func StreamChat(c *fiber.Ctx) error {
	workflowID := c.Params("workflowId")
	wf, err := collections.GetWorkflow(workflowID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	if wf.UserId != middleware.GetUserID(c) {
		return fiber.NewError(fiber.StatusForbidden, "workflow does not belong to authenticated user")
	}
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
		signal := workflows.NovaCompleteSignal{
			CallID:           callID,
			Transcript:       transcript,
			RecordingURL:     recordingURL,
			StructuredOutput: vapiclient.ExtractNovaStructuredOutput(event.Message, event.Call),
		}
		if err := signalWithStartTemporalWorkflow(c, workflows.NovaCompletionWorkflowID(workflowID), workflows.NovaCompleteSignalName, signal, workflows.NovaCompletionWorkflow, workflowID); err != nil {
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
	promptOrm := orm.Load(&models.PromptVersion{})
	defer promptOrm.Close()
	metaOrm := orm.Load(&models.MetaFlag{})
	defer metaOrm.Close()
	evaluatorOrm := orm.Load(&models.EvaluatorVersion{})
	defer evaluatorOrm.Close()
	canaryOrm := orm.Load(&models.CanaryResult{})
	defer canaryOrm.Close()

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
	var promptVersions []models.PromptVersion
	if err := promptOrm.GetAll().Scan(&promptVersions); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	var metaFlags []models.MetaFlag
	if err := metaOrm.GetAll().Scan(&metaFlags); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	var evaluatorVersions []models.EvaluatorVersion
	if err := evaluatorOrm.GetAll().Scan(&evaluatorVersions); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	var canaryResults []models.CanaryResult
	if err := canaryOrm.GetAll().Scan(&canaryResults); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	totalCost := 0.0
	for _, row := range costs {
		totalCost += row.CostUsd
	}
	if scores == nil {
		scores = []models.ConversationScore{}
	}
	if experiments == nil {
		experiments = []models.PromptExperiment{}
	}
	if costs == nil {
		costs = []models.LlmCostLog{}
	}
	if promptVersions == nil {
		promptVersions = []models.PromptVersion{}
	}
	if metaFlags == nil {
		metaFlags = []models.MetaFlag{}
	}
	if evaluatorVersions == nil {
		evaluatorVersions = []models.EvaluatorVersion{}
	}
	if canaryResults == nil {
		canaryResults = []models.CanaryResult{}
	}
	return c.JSON(fiber.Map{
		"conversation_scores": scores,
		"prompt_experiments":  experiments,
		"cost_log":            costs,
		"prompt_versions":     promptVersions,
		"meta_flags":          metaFlags,
		"evaluator_versions":  evaluatorVersions,
		"canary_results":      canaryResults,
		"total_cost_usd":      totalCost,
	})
}

func AdminRunSimulations(c *fiber.Ctx) error {
	var req rivereval.SimConfig
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	sims, err := rivereval.RunSimulation(req)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	agentID := req.AgentID
	if agentID == "" {
		agentID = models.AgentAria
	}
	scores, err := rivereval.ScoreSimulationsForAgent(sims, agentID, req.Judges)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"simulations": sims, "scores": scores})
}

func AdminRunPromptExperiment(c *fiber.Ctx) error {
	var req adminPromptExperimentRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	if req.AgentID == "" {
		return fiber.NewError(fiber.StatusBadRequest, "agent_id is required")
	}
	cfg := rivereval.SimConfig{
		Seed:             req.Seed,
		BatchSize:        req.BatchSize,
		Personas:         req.Personas,
		AgentID:          req.AgentID,
		MaxTurnsPerAgent: req.MaxTurnsPerAgent,
		Judges:           req.Judges,
	}
	exp, err := rivereval.RunImprovementCycle(req.AgentID, cfg)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"experiment": exp})
}

func AdminRerunEvaluations(c *fiber.Ctx) error {
	var req rivereval.RerunRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	result, err := rivereval.RerunEvaluations(req)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(result)
}

func AdminRollbackPrompt(c *fiber.Ctx) error {
	var req rivereval.RollbackRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	version, err := rivereval.RollbackPrompt(req)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.JSON(fiber.Map{"prompt_version": version})
}

func AdminRunMetaEvaluation(c *fiber.Ctx) error {
	var req adminMetaEvaluationRequest
	_ = c.BodyParser(&req)
	agents := []models.AgentID{models.AgentAria, models.AgentNova, models.AgentDelta}
	if req.AgentID != "" {
		agents = []models.AgentID{req.AgentID}
	}
	out := map[models.AgentID][]models.MetaFlag{}
	for _, agentID := range agents {
		flags, err := rivereval.RunMetaEvaluation(agentID)
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		out[agentID] = flags
	}
	return c.JSON(fiber.Map{"flags": out})
}

func AdminEvalMetrics(c *fiber.Ctx) error {
	metrics, err := rivereval.LoadMetrics()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(metrics)
}

func AdminRunFullCycle(c *fiber.Ctx) error {
	var req rivereval.FullCycleConfig
	_ = c.BodyParser(&req)
	if req.Reset {
		if err := collections.ResetApplicationData(); err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
	}
	report, err := rivereval.RunFullCycle(req)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(report)
}

func AdminStartFullCycle(c *fiber.Ctx) error {
	var req rivereval.FullCycleConfig
	_ = c.BodyParser(&req)
	if active := activeAdminEvalRun(); active != nil {
		return c.JSON(fiber.Map{"run_id": active.ID, "existing": true, "run": adminEvalRunToSnapshot(active)})
	}
	run := &adminEvalRun{
		ID:        utils.GenerateID(),
		Status:    "running",
		Config:    req,
		StartedAt: time.Now().UTC(),
	}
	adminEvalRuns.Lock()
	adminEvalRuns.runs[run.ID] = run
	adminEvalRuns.latest = run.ID
	adminEvalRuns.Unlock()

	go func(runID string, cfg rivereval.FullCycleConfig) {
		if cfg.Reset {
			if err := collections.ResetApplicationData(); err != nil {
				markAdminEvalRunFailed(runID, err)
				return
			}
			cfg.Reset = false
		}
		if _, err := rivereval.RunFullCycle(cfg); err != nil {
			markAdminEvalRunFailed(runID, err)
			return
		}
		markAdminEvalRunCompleted(runID)
	}(run.ID, req)

	return c.JSON(fiber.Map{"run_id": run.ID, "existing": false, "run": adminEvalRunToSnapshot(run)})
}

func AdminEvalProgress(c *fiber.Ctx) error {
	runID := c.Params("id")
	if runID == "" || runID == "latest" {
		runID = latestAdminEvalRunID()
	}
	var run *adminEvalRunSnapshot
	if runID != "" {
		stored := getAdminEvalRun(runID)
		if stored != nil {
			run = adminEvalRunToSnapshot(stored)
		}
	}
	progress, err := buildAdminEvalProgress(run)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(progress)
}

func AdminResetAndReseed(c *fiber.Ctx) error {
	if active := activeAdminEvalRun(); active != nil {
		return fiber.NewError(fiber.StatusConflict, "cannot reset database while an evaluation run is active")
	}
	if err := collections.ResetApplicationData(); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	clearAdminEvalRuns()
	return c.JSON(fiber.Map{"ok": true, "message": "application data reset and initial prompts reseeded"})
}

func AdminEvalMeta(c *fiber.Ctx) error {
	flagOrm := orm.Load(&models.MetaFlag{})
	defer flagOrm.Close()
	evalOrm := orm.Load(&models.EvaluatorVersion{})
	defer evalOrm.Close()
	canaryOrm := orm.Load(&models.ComplianceCanary{})
	defer canaryOrm.Close()
	resultOrm := orm.Load(&models.CanaryResult{})
	defer resultOrm.Close()

	var flags []models.MetaFlag
	if err := flagOrm.GetAll().Scan(&flags); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	var evaluatorVersions []models.EvaluatorVersion
	if err := evalOrm.GetAll().Scan(&evaluatorVersions); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	var canaries []models.ComplianceCanary
	if err := canaryOrm.GetAll().Scan(&canaries); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	var canaryResults []models.CanaryResult
	if err := resultOrm.GetAll().Scan(&canaryResults); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if flags == nil {
		flags = []models.MetaFlag{}
	}
	if evaluatorVersions == nil {
		evaluatorVersions = []models.EvaluatorVersion{}
	}
	if canaries == nil {
		canaries = []models.ComplianceCanary{}
	}
	if canaryResults == nil {
		canaryResults = []models.CanaryResult{}
	}
	return c.JSON(fiber.Map{
		"meta_flags":          flags,
		"evaluator_versions":  evaluatorVersions,
		"compliance_canaries": canaries,
		"canary_results":      canaryResults,
	})
}

func AdminExperimentDetail(c *fiber.Ctx) error {
	o := orm.Load(&models.PromptExperiment{})
	defer o.Close()
	var rows []models.PromptExperiment
	if err := o.GetByFieldEquals("Id", c.Params("id")).Scan(&rows); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if len(rows) == 0 {
		return fiber.NewError(fiber.StatusNotFound, "prompt experiment not found")
	}
	return c.JSON(fiber.Map{"experiment": rows[0]})
}

func activeAdminEvalRun() *adminEvalRun {
	adminEvalRuns.Lock()
	defer adminEvalRuns.Unlock()
	for _, run := range adminEvalRuns.runs {
		if run.Status == "running" {
			clone := *run
			return &clone
		}
	}
	return nil
}

func latestAdminEvalRunID() string {
	adminEvalRuns.Lock()
	defer adminEvalRuns.Unlock()
	return adminEvalRuns.latest
}

func getAdminEvalRun(id string) *adminEvalRun {
	adminEvalRuns.Lock()
	defer adminEvalRuns.Unlock()
	run := adminEvalRuns.runs[id]
	if run == nil {
		return nil
	}
	clone := *run
	return &clone
}

func markAdminEvalRunCompleted(id string) {
	adminEvalRuns.Lock()
	defer adminEvalRuns.Unlock()
	run := adminEvalRuns.runs[id]
	if run == nil {
		return
	}
	now := time.Now().UTC()
	run.Status = "completed"
	run.CompletedAt = &now
}

func markAdminEvalRunFailed(id string, err error) {
	adminEvalRuns.Lock()
	defer adminEvalRuns.Unlock()
	run := adminEvalRuns.runs[id]
	if run == nil {
		return
	}
	now := time.Now().UTC()
	msg := err.Error()
	run.Status = "failed"
	run.CompletedAt = &now
	run.Error = &msg
}

func clearAdminEvalRuns() {
	adminEvalRuns.Lock()
	defer adminEvalRuns.Unlock()
	adminEvalRuns.latest = ""
	adminEvalRuns.runs = map[string]*adminEvalRun{}
}

func adminEvalRunToSnapshot(run *adminEvalRun) *adminEvalRunSnapshot {
	if run == nil {
		return nil
	}
	return &adminEvalRunSnapshot{
		ID:          run.ID,
		Status:      run.Status,
		Config:      run.Config,
		StartedAt:   run.StartedAt,
		CompletedAt: run.CompletedAt,
		Error:       run.Error,
	}
}

func buildAdminEvalProgress(run *adminEvalRunSnapshot) (*adminEvalProgress, error) {
	scoreOrm := orm.Load(&models.ConversationScore{})
	defer scoreOrm.Close()
	expOrm := orm.Load(&models.PromptExperiment{})
	defer expOrm.Close()
	costOrm := orm.Load(&models.LlmCostLog{})
	defer costOrm.Close()
	convOrm := orm.Load(&models.AgentConversation{})
	defer convOrm.Close()
	msgOrm := orm.Load(&models.AgentMessage{})
	defer msgOrm.Close()

	var scores []models.ConversationScore
	if err := scoreOrm.GetAll().Scan(&scores); err != nil {
		return nil, err
	}
	var experiments []models.PromptExperiment
	if err := expOrm.GetAll().Scan(&experiments); err != nil {
		return nil, err
	}
	var costs []models.LlmCostLog
	if err := costOrm.GetAll().Scan(&costs); err != nil {
		return nil, err
	}
	var conversations []models.AgentConversation
	if err := convOrm.GetAll().Scan(&conversations); err != nil {
		return nil, err
	}
	var messages []models.AgentMessage
	if err := msgOrm.GetAll().Scan(&messages); err != nil {
		return nil, err
	}

	totalCost := 0.0
	for _, row := range costs {
		totalCost += row.CostUsd
	}
	sort.Slice(scores, func(i, j int) bool { return scores[i].CreatedAt.After(scores[j].CreatedAt) })
	sort.Slice(experiments, func(i, j int) bool { return experiments[i].CreatedAt.After(experiments[j].CreatedAt) })
	sort.Slice(conversations, func(i, j int) bool { return conversations[i].StartedAt.After(conversations[j].StartedAt) })
	sort.Slice(messages, func(i, j int) bool { return messages[i].CreatedAt.Before(messages[j].CreatedAt) })

	scoreByConversation := map[string]models.ConversationScore{}
	for _, score := range scores {
		if _, exists := scoreByConversation[score.ConversationId]; !exists {
			scoreByConversation[score.ConversationId] = score
		}
	}
	messagesByConversation := map[string][]models.AgentMessage{}
	for _, msg := range messages {
		messagesByConversation[msg.ConversationId] = append(messagesByConversation[msg.ConversationId], msg)
	}
	previews := make([]adminConversationPreview, 0)
	for _, conv := range conversations {
		if len(previews) >= 12 {
			break
		}
		preview := adminConversationPreview{
			Conversation: conv,
			Messages:     messagesByConversation[conv.Id],
		}
		if score, ok := scoreByConversation[conv.Id]; ok {
			preview.Score = &score
		}
		previews = append(previews, preview)
	}

	recentScores := scores
	if len(recentScores) > 24 {
		recentScores = recentScores[:24]
	}
	recentExperiments := experiments
	if len(recentExperiments) > 12 {
		recentExperiments = recentExperiments[:12]
	}
	return &adminEvalProgress{
		Run: run,
		Counts: map[string]int{
			"conversations":      len(conversations),
			"messages":           len(messages),
			"scores":             len(scores),
			"prompt_experiments": len(experiments),
			"cost_logs":          len(costs),
		},
		TotalCostUSD:    totalCost,
		RecentScores:    nonNilScores(recentScores),
		Experiments:     nonNilExperiments(recentExperiments),
		Conversations:   previews,
		LastGeneratedAt: time.Now().UTC(),
	}, nil
}

func nonNilScores(rows []models.ConversationScore) []models.ConversationScore {
	if rows == nil {
		return []models.ConversationScore{}
	}
	return rows
}

func nonNilExperiments(rows []models.PromptExperiment) []models.PromptExperiment {
	if rows == nil {
		return []models.PromptExperiment{}
	}
	return rows
}

func startTemporalWorkflow(c *fiber.Ctx, id string, workflow any, args ...any) error {
	temporalClient, err := temporalclient.Dial()
	if err != nil {
		return err
	}
	defer temporalClient.Close()
	_, err = temporalClient.ExecuteWorkflow(c.Context(), client.StartWorkflowOptions{
		ID:        id,
		TaskQueue: workflows.BorrowerCollectionsTaskQueue,
	}, workflow, args...)
	return err
}

func signalTemporalWorkflow(c *fiber.Ctx, workflowID, signalName string, payload any) error {
	temporalClient, err := temporalclient.Dial()
	if err != nil {
		return err
	}
	defer temporalClient.Close()
	return temporalClient.SignalWorkflow(c.Context(), workflowID, "", signalName, payload)
}

func signalWithStartTemporalWorkflow(c *fiber.Ctx, workflowID, signalName string, payload any, wf any, args ...any) error {
	temporalClient, err := temporalclient.Dial()
	if err != nil {
		return err
	}
	defer temporalClient.Close()
	_, err = temporalClient.SignalWithStartWorkflow(c.Context(), workflowID, signalName, payload, client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: workflows.BorrowerCollectionsTaskQueue,
	}, wf, args...)
	return err
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

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

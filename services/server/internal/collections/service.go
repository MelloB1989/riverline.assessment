package collections

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"riverline_server/internal/models"

	"github.com/MelloB1989/karma/utils"
	"github.com/MelloB1989/karma/v2/orm"
)

const handoffTokenBudget = 500

type ChatResponse struct {
	Workflow             models.BorrowerWorkflow  `json:"workflow"`
	Conversation         models.AgentConversation `json:"conversation"`
	UserMessage          models.AgentMessage      `json:"user_message"`
	AgentMessage         models.AgentMessage      `json:"agent_message"`
	StageComplete        bool                     `json:"stage_complete"`
	NovaCallRescheduled  bool                     `json:"nova_call_rescheduled"`
	NovaScheduledCallAt  *time.Time               `json:"nova_scheduled_call_at,omitempty"`
	NovaRescheduleReason *string                  `json:"nova_reschedule_reason,omitempty"`
}

type ConversationView struct {
	Workflow     models.BorrowerWorkflow  `json:"workflow"`
	Conversation models.AgentConversation `json:"conversation"`
	Messages     []models.AgentMessage    `json:"messages"`
	Offer        *models.ResolutionOffer  `json:"offer,omitempty"`
}

func StartWorkflow(userID, loanID string) (*models.BorrowerWorkflow, error) {
	if err := EnsureDefaults(); err != nil {
		return nil, err
	}
	authenticatedUserID := strings.TrimSpace(userID)
	if authenticatedUserID != "" {
		active, err := ActiveWorkflowForUser(authenticatedUserID)
		if err == nil {
			return active, nil
		}
		if !errors.Is(err, ErrActiveWorkflowNotFound) {
			return nil, err
		}
	}
	if userID == "" {
		seedUserID, seedLoanID, err := ensureDemoBorrower()
		if err != nil {
			return nil, err
		}
		userID = seedUserID
		if loanID == "" {
			loanID = seedLoanID
		}
	}
	if loanID == "" {
		loan, err := firstLoanForUser(userID)
		if err != nil {
			return nil, err
		}
		loanID = loan.Id
	}
	loan, err := GetLoan(loanID)
	if err != nil {
		return nil, err
	}
	if loan.UserId != userID {
		return nil, errors.New("loan does not belong to authenticated user")
	}
	now := time.Now().UTC()
	ariaSummary, err := borrowerAccountSummary(userID, loanID)
	if err != nil {
		return nil, err
	}
	wf := &models.BorrowerWorkflow{
		Id:                 utils.GenerateID(),
		UserId:             userID,
		LoanId:             loanID,
		CurrentStage:       models.AgentAria,
		AriaAttempts:       0,
		IdentityVerified:   boolPtr(false),
		AriaSummary:        stringPtr(ariaSummary),
		HardshipMentioned:  boolPtr(false),
		StopContactFlagged: boolPtr(false),
		HardshipFlagged:    boolPtr(false),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	o := orm.Load(&models.BorrowerWorkflow{})
	defer o.Close()
	if err := o.Insert(wf); err != nil {
		if authenticatedUserID != "" {
			active, activeErr := ActiveWorkflowForUser(authenticatedUserID)
			if activeErr == nil {
				return active, nil
			}
		}
		return nil, err
	}
	return wf, nil
}

func HandleChat(workflowID, content string) (*ChatResponse, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, errors.New("message is required")
	}
	wf, err := GetWorkflow(workflowID)
	if err != nil {
		return nil, err
	}
	chatAgent := chatAgentForStage(wf.CurrentStage)
	client, err := chatClient(chatAgent)
	if err != nil {
		return nil, err
	}
	conversation, err := getOrCreateConversation(*wf, chatAgent, client.PromptVersion())
	if err != nil {
		return nil, err
	}
	msgOrm := orm.Load(&models.AgentMessage{})
	defer msgOrm.Close()
	now := time.Now().UTC()
	userMsg := models.AgentMessage{
		Id:             utils.GenerateID(),
		ConversationId: conversation.Id,
		WorkflowId:     wf.Id,
		AgentId:        chatAgent,
		Role:           models.MessageRoleBorrower,
		Content:        content,
		CreatedAt:      now,
	}
	if err := msgOrm.Insert(&userMsg); err != nil {
		return nil, err
	}
	messages, err := ListMessages(conversation.Id, wf.Id)
	if err != nil {
		return nil, err
	}
	handoff, err := handoffForStage(*wf)
	if err != nil {
		return nil, err
	}
	toolResults, resp, err := converseForStage(client, *wf, chatAgent, handoff, messages)
	if err != nil {
		return nil, err
	}
	agentMsg := models.AgentMessage{
		Id:             utils.GenerateID(),
		ConversationId: conversation.Id,
		WorkflowId:     wf.Id,
		AgentId:        chatAgent,
		Role:           models.MessageRoleAgent,
		Content:        resp.AIResponse,
		TokenCount:     intPtr(resp.OutputTokens),
		CreatedAt:      time.Now().UTC(),
	}
	if err := msgOrm.Insert(&agentMsg); err != nil {
		return nil, err
	}
	messages = append(messages, agentMsg)
	stageComplete := false
	handoffTokens := 0
	if wf.CurrentStage == models.AgentAria && toolResults.AriaHandoff != nil {
		handoffTokens = toolResults.AriaHandoff.Tokens
		applyAriaHandoff(wf, toolResults.AriaHandoff.Result)
		if err := setInitialNovaSchedule(wf, toolResults.AriaHandoff.Result); err != nil {
			return nil, err
		}
		stageComplete = true
		if err := updateWorkflow(wf); err != nil {
			return nil, err
		}
		if err := LogCost("summarization", &chatAgent, toolResults.AriaHandoff.ModelUsed, toolResults.AriaHandoff.Tokens, 0, &conversation.Id, nil); err != nil {
			return nil, err
		}
	}
	conversation.TotalTurns = intPtr(countBorrowerTurns(messages))
	conversation.TotalTokensUsed = intPtr(derefInt(conversation.TotalTokensUsed) + resp.InputTokens + resp.OutputTokens + handoffTokens)
	if stageComplete {
		ended := time.Now().UTC()
		conversation.EndedAt = &ended
		if wf.Outcome != nil {
			conversation.Outcome = wf.Outcome
		} else {
			conversation.Outcome = outcomePtr(models.OutcomeCommitted)
		}
	}
	if err := updateConversation(&conversation); err != nil {
		return nil, err
	}
	if err := LogCost("agent_response", &chatAgent, client.ModelUsed(), resp.InputTokens, resp.OutputTokens, &conversation.Id, nil); err != nil {
		return nil, err
	}
	out := &ChatResponse{Workflow: *wf, Conversation: conversation, UserMessage: userMsg, AgentMessage: agentMsg, StageComplete: stageComplete}
	if toolResults.NovaReschedule != nil {
		out.NovaCallRescheduled = true
		out.NovaScheduledCallAt = &toolResults.NovaReschedule.ScheduledCallAt
		out.NovaRescheduleReason = stringPtr(toolResults.NovaReschedule.Reason)
	}
	return out, nil
}

var ErrActiveWorkflowNotFound = errors.New("active workflow not found")

func ActiveWorkflowForUser(userID string) (*models.BorrowerWorkflow, error) {
	o := orm.Load(&models.BorrowerWorkflow{})
	defer o.Close()
	var rows []models.BorrowerWorkflow
	if err := o.GetByFieldEquals("UserId", userID).Scan(&rows); err != nil {
		return nil, err
	}
	active := make([]models.BorrowerWorkflow, 0, len(rows))
	for _, row := range rows {
		if row.Outcome == nil && row.ResolvedAt == nil {
			active = append(active, row)
		}
	}
	if len(active) == 0 {
		return nil, ErrActiveWorkflowNotFound
	}
	sort.Slice(active, func(i, j int) bool { return active[i].CreatedAt.After(active[j].CreatedAt) })
	return &active[0], nil
}

func GetWorkflow(id string) (*models.BorrowerWorkflow, error) {
	o := orm.Load(&models.BorrowerWorkflow{})
	defer o.Close()
	var rows []models.BorrowerWorkflow
	if err := o.GetByFieldEquals("Id", id).Scan(&rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New("workflow not found")
	}
	return &rows[0], nil
}

func GetUser(id string) (*models.User, error) {
	o := orm.Load(&models.User{})
	defer o.Close()
	var rows []models.User
	if err := o.GetByFieldEquals("Id", id).Scan(&rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New("user not found")
	}
	return &rows[0], nil
}

func GetLoan(id string) (*models.Loan, error) {
	o := orm.Load(&models.Loan{})
	defer o.Close()
	var rows []models.Loan
	if err := o.GetByFieldEquals("Id", id).Scan(&rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New("loan not found")
	}
	return &rows[0], nil
}

func ConversationByIDOrWorkflow(id string) (*ConversationView, error) {
	wf, err := GetWorkflow(id)
	if err != nil {
		conv, convErr := getConversationByID(id)
		if convErr != nil {
			return nil, err
		}
		wf, err = GetWorkflow(conv.WorkflowId)
		if err != nil {
			return nil, err
		}
		msgs, _ := ListMessages(conv.Id, conv.WorkflowId)
		return conversationView(*wf, *conv, msgs), nil
	}
	conv, err := latestConversation(wf.Id)
	if err != nil {
		return &ConversationView{Workflow: *wf, Messages: []models.AgentMessage{}}, nil
	}
	msgs, _ := ListMessages(conv.Id, wf.Id)
	return conversationView(*wf, *conv, msgs), nil
}

func ListMessages(conversationID, workflowID string) ([]models.AgentMessage, error) {
	o := orm.Load(&models.AgentMessage{})
	defer o.Close()
	var rows []models.AgentMessage
	var err error
	if conversationID != "" {
		err = o.GetByFieldEquals("ConversationId", conversationID).Scan(&rows)
	} else {
		err = o.GetByFieldEquals("WorkflowId", workflowID).Scan(&rows)
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].CreatedAt.Before(rows[j].CreatedAt) })
	return rows, nil
}

func ActivePromptVersion(agentID models.AgentID) (*models.PromptVersion, error) {
	o := orm.Load(&models.PromptVersion{})
	defer o.Close()
	var rows []models.PromptVersion
	if err := o.GetByFieldsEquals(map[string]any{"AgentId": agentID, "IsActive": true}).Scan(&rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("active prompt not found for %s", agentID)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].VersionNumber > rows[j].VersionNumber })
	return &rows[0], nil
}

func LogCost(callType string, agentID *models.AgentID, modelUsed string, promptTokens, completionTokens int, conversationID, experimentID *string) error {
	row := models.LlmCostLog{
		Id:               utils.GenerateID(),
		CallType:         callType,
		AgentId:          agentID,
		ModelUsed:        modelUsed,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
		CostUsd:          0,
		ConversationId:   conversationID,
		ExperimentId:     experimentID,
		CreatedAt:        time.Now().UTC(),
	}
	o := orm.Load(&models.LlmCostLog{})
	defer o.Close()
	return o.Insert(&row)
}

func chatAgentForStage(stage models.AgentID) models.AgentID {
	if stage == models.AgentDelta {
		return models.AgentDelta
	}
	return models.AgentAria
}

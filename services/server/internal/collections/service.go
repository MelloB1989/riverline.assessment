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
	Workflow      models.BorrowerWorkflow  `json:"workflow"`
	Conversation  models.AgentConversation `json:"conversation"`
	UserMessage   models.AgentMessage      `json:"user_message"`
	AgentMessage  models.AgentMessage      `json:"agent_message"`
	StageComplete bool                     `json:"stage_complete"`
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
	if userID == "" || loanID == "" {
		seedUserID, seedLoanID, err := ensureDemoBorrower()
		if err != nil {
			return nil, err
		}
		if userID == "" {
			userID = seedUserID
		}
		if loanID == "" {
			loanID = seedLoanID
		}
	}
	now := time.Now().UTC()
	wf := &models.BorrowerWorkflow{
		Id:                 utils.GenerateID(),
		UserId:             userID,
		LoanId:             loanID,
		CurrentStage:       models.AgentAria,
		AriaAttempts:       0,
		IdentityVerified:   boolPtr(false),
		HardshipMentioned:  boolPtr(false),
		StopContactFlagged: boolPtr(false),
		HardshipFlagged:    boolPtr(false),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	o := orm.Load(&models.BorrowerWorkflow{})
	defer o.Close()
	return wf, o.Insert(wf)
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

	client, err := chatClient(wf.CurrentStage)
	if err != nil {
		return nil, err
	}
	conversation, err := getOrCreateConversation(*wf, wf.CurrentStage, client.PromptVersion())
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
		AgentId:        wf.CurrentStage,
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
	handoff := handoffForStage(*wf)
	resp, err := client.Converse(handoff, messages)
	if err != nil {
		return nil, err
	}
	agentMsg := models.AgentMessage{
		Id:             utils.GenerateID(),
		ConversationId: conversation.Id,
		WorkflowId:     wf.Id,
		AgentId:        wf.CurrentStage,
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
	if wf.CurrentStage == models.AgentAria {
		handoffCall, err := GenerateHandoff(models.AgentAria, *wf, messages, "")
		if err != nil {
			return nil, err
		}
		handoffTokens = handoffCall.Tokens
		applyAssessmentHandoff(wf, handoffCall.Result)
		stageComplete = handoffCall.Result.StageComplete
		if err := updateWorkflow(wf); err != nil {
			return nil, err
		}
		if err := LogCost("summarization", &wf.CurrentStage, "karma-llama3.3-70b", handoffCall.Tokens, 0, &conversation.Id, nil); err != nil {
			return nil, err
		}
	}
	if wf.CurrentStage == models.AgentDelta {
		handoffCall, err := GenerateHandoff(models.AgentDelta, *wf, messages, "")
		if err != nil {
			return nil, err
		}
		handoffTokens = handoffCall.Tokens
		applyDeltaHandoff(wf, handoffCall.Result)
		stageComplete = handoffCall.Result.StageComplete
		if err := updateWorkflow(wf); err != nil {
			return nil, err
		}
		if err := LogCost("summarization", &wf.CurrentStage, "karma-llama3.3-70b", handoffCall.Tokens, 0, &conversation.Id, nil); err != nil {
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
	if err := LogCost("agent_response", &wf.CurrentStage, "karma-llama3.3-70b", resp.InputTokens, resp.OutputTokens, &conversation.Id, nil); err != nil {
		return nil, err
	}
	return &ChatResponse{Workflow: *wf, Conversation: conversation, UserMessage: userMsg, AgentMessage: agentMsg, StageComplete: stageComplete}, nil
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

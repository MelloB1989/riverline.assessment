package workflows

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"riverline_server/constants"
	"riverline_server/internal/agents"
	"riverline_server/internal/collections"
	rivereval "riverline_server/internal/eval"
	"riverline_server/internal/models"
	"riverline_server/internal/vapi"

	"github.com/MelloB1989/karma/v2/orm"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const BorrowerCollectionsTaskQueue = "borrower-collections"
const RescheduleNovaCallSignalName = "reschedule_nova_call"
const NovaCompleteSignalName = "nova_complete"
const NovaMaxCallAttempts = 3

type BorrowerWorkflowResult struct {
	WorkflowID string
	Outcome    string
}

type NovaCompleteSignal struct {
	CallID           string
	Transcript       string
	RecordingURL     string
	DurationSeconds  *int
	StructuredOutput map[string]any
}

type RescheduleNovaCallSignal struct {
	ScheduledCallAt time.Time
	Reason          string
}

type NovaCompletionPollResult struct {
	Ready     bool
	Retryable bool
	Reason    string
	Signal    NovaCompleteSignal
}

func activityContext(ctx workflow.Context) workflow.Context {
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    24 * time.Hour,
			BackoffCoefficient: 1,
		},
	}
	return workflow.WithActivityOptions(ctx, activityOptions)
}

func novaCallActivityContext(ctx workflow.Context) workflow.Context {
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    30 * time.Second,
			BackoffCoefficient: 2,
		},
	}
	return workflow.WithActivityOptions(ctx, activityOptions)
}

func AriaHandoffWorkflow(ctx workflow.Context, workflowID string) (BorrowerWorkflowResult, error) {
	ctx = activityContext(ctx)
	var afterAriaOutcome string
	if err := workflow.ExecuteActivity(ctx, CompleteARIA, workflowID).Get(ctx, &afterAriaOutcome); err != nil {
		return BorrowerWorkflowResult{}, err
	}
	if afterAriaOutcome == string(models.OutcomeStopContact) {
		return BorrowerWorkflowResult{WorkflowID: workflowID, Outcome: afterAriaOutcome}, nil
	}

	version := workflow.GetVersion(ctx, "prepare-nova-before-scheduled-wait", workflow.DefaultVersion, 1)
	if version == 1 {
		var preparedWorkflowID string
		if err := workflow.ExecuteActivity(ctx, PrepareNOVA, workflowID).Get(ctx, &preparedWorkflowID); err != nil {
			return BorrowerWorkflowResult{}, err
		}
	}
	if err := waitForNovaSchedule(ctx, workflowID); err != nil {
		return BorrowerWorkflowResult{}, err
	}
	var callID string
	if err := workflow.ExecuteActivity(novaCallActivityContext(ctx), StartNOVA, workflowID).Get(ctx, &callID); err != nil {
		return BorrowerWorkflowResult{}, err
	}
	version = workflow.GetVersion(ctx, "start-nova-completion-child", workflow.DefaultVersion, 1)
	if version == 1 {
		if err := startChildAndWaitForStart(ctx, NovaCompletionWorkflowID(workflowID), NovaCompletionWorkflow, workflowID); err != nil {
			return BorrowerWorkflowResult{}, err
		}
	}
	return BorrowerWorkflowResult{WorkflowID: workflowID, Outcome: string(models.AgentNova)}, nil
}

func waitForNovaSchedule(ctx workflow.Context, workflowID string) error {
	var scheduledAt time.Time
	if err := workflow.ExecuteActivity(ctx, GetNovaScheduledCallAt, workflowID).Get(ctx, &scheduledAt); err != nil {
		return err
	}
	signalCh := workflow.GetSignalChannel(ctx, RescheduleNovaCallSignalName)
	for {
		delay := scheduledAt.Sub(workflow.Now(ctx))
		if delay <= 0 {
			return nil
		}
		timerCtx, cancelTimer := workflow.WithCancel(ctx)
		timer := workflow.NewTimer(timerCtx, delay)
		timerFired := false
		selector := workflow.NewSelector(ctx)
		selector.AddFuture(timer, func(workflow.Future) {
			timerFired = true
		})
		selector.AddReceive(signalCh, func(c workflow.ReceiveChannel, _ bool) {
			var signal RescheduleNovaCallSignal
			c.Receive(ctx, &signal)
			if !signal.ScheduledCallAt.IsZero() {
				scheduledAt = signal.ScheduledCallAt.UTC()
			}
			cancelTimer()
		})
		selector.Select(ctx)
		if timerFired {
			return nil
		}
	}
}

func NovaCompletionWorkflow(ctx workflow.Context, workflowID string) (BorrowerWorkflowResult, error) {
	ctx = activityContext(ctx)
	signalCh := workflow.GetSignalChannel(ctx, NovaCompleteSignalName)
	var novaSignal NovaCompleteSignal
	attempts := 1
	for {
		receivedSignal := false
		selector := workflow.NewSelector(ctx)
		selector.AddReceive(signalCh, func(c workflow.ReceiveChannel, _ bool) {
			c.Receive(ctx, &novaSignal)
			receivedSignal = true
		})
		selector.AddFuture(workflow.NewTimer(ctx, 15*time.Second), func(workflow.Future) {})
		selector.Select(ctx)
		if receivedSignal {
			if strings.TrimSpace(novaSignal.Transcript) != "" {
				break
			}
			var poll NovaCompletionPollResult
			if err := workflow.ExecuteActivity(ctx, PollNOVACompletionFromVapi, workflowID).Get(ctx, &poll); err != nil {
				return BorrowerWorkflowResult{}, err
			}
			if poll.Ready {
				novaSignal = poll.Signal
				break
			}
			if poll.Retryable && attempts < NovaMaxCallAttempts {
				attempts++
				if err := workflow.NewTimer(ctx, novaRetryDelay(attempts)).Get(ctx, nil); err != nil {
					return BorrowerWorkflowResult{}, err
				}
				var callID string
				if err := workflow.ExecuteActivity(novaCallActivityContext(ctx), StartNOVA, workflowID).Get(ctx, &callID); err != nil {
					return BorrowerWorkflowResult{}, err
				}
				continue
			}
		}
		var poll NovaCompletionPollResult
		if err := workflow.ExecuteActivity(ctx, PollNOVACompletionFromVapi, workflowID).Get(ctx, &poll); err != nil {
			return BorrowerWorkflowResult{}, err
		}
		if poll.Ready {
			novaSignal = poll.Signal
			break
		}
		if poll.Retryable {
			if attempts < NovaMaxCallAttempts {
				attempts++
				if err := workflow.NewTimer(ctx, novaRetryDelay(attempts)).Get(ctx, nil); err != nil {
					return BorrowerWorkflowResult{}, err
				}
				var callID string
				if err := workflow.ExecuteActivity(novaCallActivityContext(ctx), StartNOVA, workflowID).Get(ctx, &callID); err != nil {
					return BorrowerWorkflowResult{}, err
				}
				continue
			}
			novaSignal = NovaCompleteSignal{
				Transcript: "NOVA outbound call did not connect after retry attempts. Final retry reason: " + poll.Reason + ".",
			}
			break
		}
	}

	var novaOutcome string
	if err := workflow.ExecuteActivity(ctx, CompleteNOVA, workflowID, novaSignal).Get(ctx, &novaOutcome); err != nil {
		return BorrowerWorkflowResult{}, err
	}
	if err := startChildAndWaitForStart(ctx, DeltaHandoffWorkflowID(workflowID), DeltaHandoffWorkflow, workflowID, novaOutcome); err != nil {
		return BorrowerWorkflowResult{}, err
	}
	return BorrowerWorkflowResult{WorkflowID: workflowID, Outcome: novaOutcome}, nil
}

func novaRetryDelay(attempt int) time.Duration {
	if attempt <= 2 {
		return 30 * time.Second
	}
	return 2 * time.Minute
}

func DeltaHandoffWorkflow(ctx workflow.Context, workflowID string, novaOutcome string) (BorrowerWorkflowResult, error) {
	ctx = activityContext(ctx)
	if novaOutcome == string(models.OutcomeCommitted) {
		if err := workflow.ExecuteActivity(ctx, SendNOVAOfferEmail, workflowID).Get(ctx, nil); err != nil {
			return BorrowerWorkflowResult{}, err
		}
	} else if err := workflow.ExecuteActivity(ctx, SendDELTAFinalOfferEmail, workflowID).Get(ctx, nil); err != nil {
		return BorrowerWorkflowResult{}, err
	}
	if err := startChildAndWaitForStart(ctx, EvaluationWorkflowID(workflowID), EvaluationWorkflow, workflowID); err != nil {
		return BorrowerWorkflowResult{}, err
	}
	return BorrowerWorkflowResult{WorkflowID: workflowID, Outcome: string(models.AgentDelta)}, nil
}

func EvaluationWorkflow(ctx workflow.Context, workflowID string) (BorrowerWorkflowResult, error) {
	ctx = activityContext(ctx)
	var scored int
	if err := workflow.ExecuteActivity(ctx, EvaluateWorkflowConversations, workflowID).Get(ctx, &scored); err != nil {
		return BorrowerWorkflowResult{}, err
	}
	return BorrowerWorkflowResult{WorkflowID: workflowID, Outcome: "evaluated"}, nil
}

func startChildAndWaitForStart(ctx workflow.Context, id string, child any, args ...any) error {
	childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
		WorkflowID:            id,
		TaskQueue:             BorrowerCollectionsTaskQueue,
		ParentClosePolicy:     enums.PARENT_CLOSE_POLICY_ABANDON,
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
	})
	future := workflow.ExecuteChildWorkflow(childCtx, child, args...)
	var execution workflow.Execution
	return future.GetChildWorkflowExecution().Get(ctx, &execution)
}

func NovaCompletionWorkflowID(workflowID string) string {
	return workflowID + "-nova-completion"
}

func DeltaHandoffWorkflowID(workflowID string) string {
	return workflowID + "-delta-handoff"
}

func EvaluationWorkflowID(workflowID string) string {
	return workflowID + "-evaluation"
}

func CompleteARIA(workflowID string) (string, error) {
	if err := collections.CompleteARIA(workflowID); err != nil {
		return "", err
	}
	wf, err := collections.GetWorkflow(workflowID)
	if err != nil {
		return "", err
	}
	if wf.Outcome != nil {
		return string(*wf.Outcome), nil
	}
	return string(wf.CurrentStage), nil
}

func StartNOVA(workflowID string) (string, error) {
	wf, err := collections.GetWorkflow(workflowID)
	if err != nil {
		return "", err
	}
	user, err := collections.GetUser(wf.UserId)
	if err != nil {
		return "", err
	}
	loan, err := collections.GetLoan(wf.LoanId)
	if err != nil {
		return "", err
	}
	offer, err := collections.PrepareNOVA(workflowID)
	if err != nil {
		return "", err
	}
	wf, err = collections.GetWorkflow(workflowID)
	if err != nil {
		return "", err
	}
	novaAgent, err := agents.NewNova()
	if err != nil {
		return "", err
	}
	contextForNova := derefString(wf.ContextForNova)
	cfg := constants.AppCfg.Get()
	if err := collections.SyncNovaVapiAssistant(context.Background()); err != nil {
		return "", err
	}
	client := vapi.New(cfg.VapiApiKey, "", cfg.VapiPhoneNumberId, cfg.VapiAssistantId, cfg.VapiDryRun)
	offers := map[string]any{"lump_sum": offer.LumpSumOffered, "emi_amount": offer.EmiAmount, "emi_months": offer.EmiMonths, "hardship": offer.HardshipOffered}
	phone := ""
	if user.Phone != nil {
		phone = *user.Phone
	}
	callContext := novaVapiHandoffContext(workflowID, *wf, *user, *loan, *offer, offers)
	callID, err := client.StartCall(context.Background(), phone, callContext)
	if err != nil {
		if strings.Contains(err.Error(), "400 Bad Request") {
			return "", temporal.NewNonRetryableApplicationError("vapi start call failed", "VapiBadRequest", err)
		}
		return "", err
	}
	if err := collections.MarkNOVAStarted(workflowID, callID, novaAgent.PromptVersion(), contextForNova); err != nil {
		return "", err
	}
	return callID, nil
}

func PrepareNOVA(workflowID string) (string, error) {
	if _, err := collections.PrepareNOVA(workflowID); err != nil {
		return "", err
	}
	return workflowID, nil
}

func GetNovaScheduledCallAt(workflowID string) (time.Time, error) {
	return collections.GetNovaScheduledCallAt(workflowID)
}

func CompleteNOVA(workflowID string, signal NovaCompleteSignal) (string, error) {
	outcome, err := collections.CompleteNOVA(workflowID, signal.CallID, signal.Transcript, signal.RecordingURL, signal.DurationSeconds, signal.StructuredOutput)
	if err != nil {
		return "", err
	}
	return string(outcome), nil
}

func PollNOVACompletionFromVapi(workflowID string) (*NovaCompletionPollResult, error) {
	offer, err := collections.GetResolutionOffer(workflowID)
	if err != nil {
		return nil, err
	}
	if offer.VapiCallId == nil || strings.TrimSpace(*offer.VapiCallId) == "" {
		return &NovaCompletionPollResult{}, nil
	}
	cfg := constants.AppCfg.Get()
	client := vapi.New(cfg.VapiApiKey, "", cfg.VapiPhoneNumberId, cfg.VapiAssistantId, cfg.VapiDryRun)
	details, err := client.GetCallDetails(context.Background(), *offer.VapiCallId)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(details.Status, "ended") {
		return &NovaCompletionPollResult{}, nil
	}
	transcript := strings.TrimSpace(details.Transcript)
	reason := novaCallEndReason(details)
	if isRetryableNovaCallEnd(details) {
		return &NovaCompletionPollResult{
			Retryable: true,
			Reason:    reason,
		}, nil
	}
	if transcript == "" {
		transcript = "NOVA outbound call ended before a borrower conversation was captured. " + reason + "."
	}
	recordingURL := details.RecordingURL
	return &NovaCompletionPollResult{
		Ready: true,
		Signal: NovaCompleteSignal{
			CallID:           *offer.VapiCallId,
			Transcript:       transcript,
			RecordingURL:     recordingURL,
			DurationSeconds:  details.DurationSeconds,
			StructuredOutput: details.StructuredOutput,
		},
	}, nil
}

func novaVapiHandoffContext(workflowID string, wf models.BorrowerWorkflow, user models.User, loan models.Loan, offer models.ResolutionOffer, offers map[string]any) vapi.HandoffContext {
	now := time.Now().UTC()
	ist := now.In(novaISTLocation())
	return vapi.HandoffContext{
		WorkflowID:             workflowID,
		BorrowerName:           strings.TrimSpace(user.FirstName + " " + user.LastName),
		BorrowerFirstName:      strings.TrimSpace(user.FirstName),
		BorrowerEmail:          user.Email,
		AccountNumberPartial:   loan.AccountNumberPartial,
		BorrowerContext:        compactJSON(novaBorrowerContext(user)),
		LoanContext:            compactJSON(novaLoanContext(loan)),
		AriaSummary:            derefString(wf.AriaSummary),
		ContextForNova:         derefString(wf.ContextForNova),
		ResolutionOfferContext: compactJSON(novaOfferContext(offer)),
		Offers:                 offers,
		CurrentISTTimestamp:    ist.Format(time.RFC3339),
		CurrentUTCTimestamp:    now.Format(time.RFC3339),
	}
}

func novaBorrowerContext(user models.User) map[string]any {
	return map[string]any{
		"user_id":    user.Id,
		"first_name": user.FirstName,
		"last_name":  user.LastName,
		"email":      user.Email,
		"phone":      user.Phone,
		"dob":        user.Dob.Format("2006-01-02"),
		"gender":     user.Gender,
		"extra":      user.Extra,
	}
}

func novaLoanContext(loan models.Loan) map[string]any {
	return map[string]any{
		"loan_id":                 loan.Id,
		"account_number_partial":  loan.AccountNumberPartial,
		"loan_type":               loan.LoanType,
		"principal_amount":        loan.PrincipalAmount,
		"outstanding_amount":      loan.OutstandingAmount,
		"days_overdue":            loan.DaysOverdue,
		"last_payment_date":       loan.LastPaymentDate,
		"last_payment_amount":     loan.LastPaymentAmount,
		"interest_rate":           loan.InterestRate,
		"policy_max_discount_pct": loan.PolicyMaxDiscountPct,
		"status":                  loan.Status,
	}
}

func novaOfferContext(offer models.ResolutionOffer) map[string]any {
	return map[string]any{
		"candidate_offer":       offer.CandidateOffer,
		"scheduled_call_at":     offer.ScheduledCallAt,
		"lump_sum_offered":      offer.LumpSumOffered,
		"lump_sum_discount_pct": offer.LumpSumDiscountPct,
		"emi_amount":            offer.EmiAmount,
		"emi_months":            offer.EmiMonths,
		"emi_start_date":        offer.EmiStartDate,
		"hardship_offered":      offer.HardshipOffered,
	}
}

func compactJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func novaISTLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err == nil {
		return loc
	}
	return time.FixedZone("IST", 5*60*60+30*60)
}

func novaCallEndReason(details *vapi.CallDetails) string {
	return "Vapi status: " + strings.TrimSpace(details.Status) + ". Vapi ended reason: " + strings.TrimSpace(details.EndedReason)
}

func isRetryableNovaCallEnd(details *vapi.CallDetails) bool {
	endedReason := strings.ToLower(strings.TrimSpace(details.EndedReason))
	transcript := strings.TrimSpace(details.Transcript)
	shortCall := details.DurationSeconds != nil && *details.DurationSeconds < 15
	if transcript == "" {
		return isRetryableVapiEndedReason(endedReason)
	}
	return shortCall && isRetryableMidCallDisconnectReason(endedReason)
}

func isRetryableVapiEndedReason(endedReason string) bool {
	retryable := []string{
		"busy",
		"customer-busy",
		"no-answer",
		"customer-did-not-answer",
		"voicemail",
		"failed",
		"provider-error",
		"phone-call-provider-error",
		"phone-call-provider-closed",
		"silence-timed-out",
		"customer-ended-call",
	}
	for _, value := range retryable {
		if strings.Contains(endedReason, value) {
			return true
		}
	}
	return endedReason == ""
}

func isRetryableMidCallDisconnectReason(endedReason string) bool {
	retryable := []string{
		"customer-ended-call",
		"phone-call-provider-closed",
		"phone-call-provider-error",
		"provider-error",
		"silence-timed-out",
	}
	for _, value := range retryable {
		if strings.Contains(endedReason, value) {
			return true
		}
	}
	return false
}

func SendNOVAOfferEmail(workflowID string) error {
	return collections.SendNOVAOfferEmail(workflowID)
}

func SendDELTAFinalOfferEmail(workflowID string) error {
	return collections.SendDELTAFinalOfferEmail(workflowID)
}

func EvaluateWorkflowConversations(workflowID string) (int, error) {
	convOrm := orm.Load(&models.AgentConversation{})
	defer convOrm.Close()
	var conversations []models.AgentConversation
	if err := convOrm.GetByFieldEquals("WorkflowId", workflowID).Scan(&conversations); err != nil {
		return 0, err
	}
	sort.Slice(conversations, func(i, j int) bool {
		return conversations[i].StartedAt.Before(conversations[j].StartedAt)
	})
	scored := 0
	for _, conv := range conversations {
		if derefBool(conv.IsSimulated) || hasConversationScore(conv.Id) {
			continue
		}
		transcript, err := conversationTranscript(conv)
		if err != nil || strings.TrimSpace(transcript) == "" {
			continue
		}
		evaluation, err := rivereval.Evaluate(conv.AgentId, transcript)
		if err != nil {
			return scored, err
		}
		if err := rivereval.SaveScore(conv, evaluation); err != nil {
			return scored, err
		}
		scored++
	}
	return scored, nil
}

func conversationTranscript(conv models.AgentConversation) (string, error) {
	messages, err := collections.ListMessages(conv.Id, conv.WorkflowId)
	if err != nil {
		return "", err
	}
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].CreatedAt.Before(messages[j].CreatedAt)
	})
	var b strings.Builder
	for _, msg := range messages {
		role := "Agent"
		if msg.Role == models.MessageRoleBorrower {
			role = "Borrower"
		}
		b.WriteString(role)
		b.WriteString(": ")
		b.WriteString(strings.TrimSpace(msg.Content))
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String()), nil
}

func hasConversationScore(conversationID string) bool {
	scoreOrm := orm.Load(&models.ConversationScore{})
	defer scoreOrm.Close()
	var scores []models.ConversationScore
	if err := scoreOrm.GetByFieldEquals("ConversationId", conversationID).Scan(&scores); err != nil {
		return false
	}
	return len(scores) > 0
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func derefBool(v *bool) bool {
	return v != nil && *v
}

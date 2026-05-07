package workflows

import (
	"context"
	"time"

	"riverline_server/constants"
	"riverline_server/internal/agents"
	"riverline_server/internal/collections"
	"riverline_server/internal/models"
	"riverline_server/internal/vapi"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const BorrowerCollectionsTaskQueue = "borrower-collections"

type BorrowerWorkflowInput struct {
	WorkflowID string
	UserID     string
	LoanID     string
}

type BorrowerWorkflowResult struct {
	WorkflowID string
	Outcome    string
}

type NovaCompleteSignal struct {
	CallID          string
	Transcript      string
	RecordingURL    string
	DurationSeconds *int
}

func BorrowerCollectionsWorkflow(ctx workflow.Context, input BorrowerWorkflowInput) (BorrowerWorkflowResult, error) {
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    24 * time.Hour,
			BackoffCoefficient: 1,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	var workflowID string
	if err := workflow.ExecuteActivity(ctx, RunARIA, input).Get(ctx, &workflowID); err != nil {
		return BorrowerWorkflowResult{}, err
	}

	var ariaSignal struct{}
	workflow.GetSignalChannel(ctx, "aria_complete").Receive(ctx, &ariaSignal)
	var afterAriaOutcome string
	if err := workflow.ExecuteActivity(ctx, CompleteARIA, workflowID).Get(ctx, &afterAriaOutcome); err != nil {
		return BorrowerWorkflowResult{}, err
	}
	if afterAriaOutcome == string(models.OutcomeStopContact) {
		return BorrowerWorkflowResult{WorkflowID: workflowID, Outcome: afterAriaOutcome}, nil
	}

	var callID string
	if err := workflow.ExecuteActivity(ctx, StartNOVA, workflowID).Get(ctx, &callID); err != nil {
		return BorrowerWorkflowResult{}, err
	}
	var novaSignal NovaCompleteSignal
	novaSignal.CallID = callID
	workflow.GetSignalChannel(ctx, "nova_complete").Receive(ctx, &novaSignal)
	var novaOutcome string
	if err := workflow.ExecuteActivity(ctx, CompleteNOVA, workflowID, novaSignal).Get(ctx, &novaOutcome); err != nil {
		return BorrowerWorkflowResult{}, err
	}
	if novaOutcome != string(models.OutcomeCommitted) {
		if err := workflow.ExecuteActivity(ctx, SendDELTAFinalOfferEmail, workflowID).Get(ctx, nil); err != nil {
			return BorrowerWorkflowResult{}, err
		}
		var deltaSignal struct{}
		workflow.GetSignalChannel(ctx, "delta_complete").Receive(ctx, &deltaSignal)
		var deltaOutcome string
		if err := workflow.ExecuteActivity(ctx, CompleteDELTA, workflowID).Get(ctx, &deltaOutcome); err != nil {
			return BorrowerWorkflowResult{}, err
		}
		return BorrowerWorkflowResult{WorkflowID: workflowID, Outcome: deltaOutcome}, nil
	}
	if err := workflow.ExecuteActivity(ctx, SendNOVAOfferEmail, workflowID).Get(ctx, nil); err != nil {
		return BorrowerWorkflowResult{}, err
	}
	return BorrowerWorkflowResult{WorkflowID: workflowID, Outcome: novaOutcome}, nil
}

func RunARIA(input BorrowerWorkflowInput) (string, error) {
	if input.WorkflowID != "" {
		return input.WorkflowID, nil
	}
	wf, err := collections.StartWorkflow(input.UserID, input.LoanID)
	if err != nil {
		return "", err
	}
	return wf.Id, nil
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
	offer, err := collections.PrepareNOVA(workflowID)
	if err != nil {
		return "", err
	}
	novaAgent, err := agents.NewNova()
	if err != nil {
		return "", err
	}
	contextForNova := derefString(wf.ContextForNova)
	if err := novaAgent.AssertBudget(contextForNova, nil); err != nil {
		return "", err
	}
	client := vapi.New(constants.AppCfg.Get().VapiApiKey, "", constants.AppCfg.Get().VapiPhoneNumberId, constants.AppCfg.Get().VapiAssistantId)
	offers := map[string]any{"lump_sum": offer.LumpSumOffered, "emi_amount": offer.EmiAmount, "emi_months": offer.EmiMonths, "hardship": offer.HardshipOffered}
	phone := ""
	if user.Phone != nil {
		phone = *user.Phone
	}
	callID, err := client.StartCall(context.Background(), phone, vapi.HandoffContext{WorkflowID: workflowID, AriaSummary: derefString(wf.AriaSummary), ContextForNova: contextForNova, Offers: offers})
	if err != nil {
		return "", err
	}
	if err := collections.MarkNOVAStarted(workflowID, callID, novaAgent.PromptVersion(), contextForNova); err != nil {
		return "", err
	}
	return callID, nil
}

func CompleteNOVA(workflowID string, signal NovaCompleteSignal) (string, error) {
	outcome, err := collections.CompleteNOVA(workflowID, signal.CallID, signal.Transcript, signal.RecordingURL, signal.DurationSeconds)
	if err != nil {
		return "", err
	}
	return string(outcome), nil
}

func SendNOVAOfferEmail(workflowID string) error {
	return collections.SendNOVAOfferEmail(workflowID)
}

func SendDELTAFinalOfferEmail(workflowID string) error {
	return collections.SendDELTAFinalOfferEmail(workflowID)
}

func CompleteDELTA(workflowID string) (string, error) {
	if err := collections.CompleteDELTA(workflowID); err != nil {
		return "", err
	}
	wf, err := collections.GetWorkflow(workflowID)
	if err != nil {
		return "", err
	}
	if wf.Outcome == nil {
		return "", nil
	}
	return string(*wf.Outcome), nil
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

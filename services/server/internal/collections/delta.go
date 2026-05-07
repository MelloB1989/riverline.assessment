package collections

func SendDELTAFinalOfferEmail(workflowID string) error {
	return sendOfferEmail(workflowID, true)
}

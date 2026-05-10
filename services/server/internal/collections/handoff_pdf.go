package collections

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"time"

	"riverline_server/internal/models"
)

func DeltaHandoffPDF(workflowID string) ([]byte, error) {
	export, err := DeltaHandoffForWorkflow(workflowID)
	if err != nil {
		return nil, err
	}
	return renderSimplePDF(deltaHandoffPDFLines(export)), nil
}

func deltaHandoffPDFLines(export *DeltaHandoffExport) []string {
	lines := []string{
		"Riverline Final Notice Handoff",
		"Workflow: " + export.WorkflowID,
		"Generated: " + export.ExportedAt.Format("January 2, 2006 15:04 MST"),
		"",
		"Status",
		"Current stage: " + string(export.CurrentStage),
		"Outcome: " + outcomeText(export.Outcome),
		"Final offer amount: " + moneyPtrText(export.FinalOfferAmount),
		"Final offer deadline: " + timePtrText(export.FinalOfferDeadline),
		"",
		"Handoff Context",
	}
	lines = appendWrapped(lines, cleanPtrText(export.ContextForDelta), 88)
	if export.Offer != nil {
		lines = append(lines, "", "NOVA Offer Record")
		offerLines := []string{
			"Offer status: " + string(export.Offer.Status),
			"NOVA offer accepted: " + boolPtrText(export.Offer.OfferAccepted),
			"Accepted offer type: " + cleanPtrText(export.Offer.AcceptedOfferType),
			"Lump-sum offer: " + moneyPtrText(export.Offer.LumpSumOffered),
			"Payment plan: " + paymentPlanText(export.Offer),
			"Hardship offered: " + boolPtrText(export.Offer.HardshipOffered),
		}
		lines = append(lines, offerLines...)
		if len(export.Offer.ObjectionsRaised) > 0 {
			objections := append([]string{}, export.Offer.ObjectionsRaised...)
			sort.Strings(objections)
			lines = append(lines, "Objections: "+strings.Join(objections, "; "))
		}
	}
	return lines
}

func renderSimplePDF(lines []string) []byte {
	chunks := chunkLines(lines, 48)
	fontObjectID := 3
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	kids := []string{}
	for i, chunk := range chunks {
		pageObjectID := 4 + i*2
		contentObjectID := pageObjectID + 1
		kids = append(kids, fmt.Sprintf("%d 0 R", pageObjectID))
		stream := renderPDFPageStream(chunk, i+1, len(chunks))
		objects = append(objects,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 %d 0 R >> >> /Contents %d 0 R >>", fontObjectID, contentObjectID),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		)
	}
	objects[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(chunks))
	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n")
	offsets := make([]int, 0, len(objects)+1)
	offsets = append(offsets, 0)
	for i, obj := range objects {
		offsets = append(offsets, out.Len())
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n", len(objects)+1)
	out.WriteString("0000000000 65535 f \n")
	for i := 1; i < len(offsets); i++ {
		fmt.Fprintf(&out, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return out.Bytes()
}

func renderPDFPageStream(lines []string, page int, total int) string {
	var content bytes.Buffer
	content.WriteString("BT\n/F1 11 Tf\n50 780 Td\n14 TL\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			content.WriteString("T*\n")
			continue
		}
		content.WriteString("(" + escapePDFText(line) + ") Tj\nT*\n")
	}
	content.WriteString("ET\n")
	content.WriteString("BT\n/F1 9 Tf\n500 28 Td\n")
	content.WriteString(fmt.Sprintf("(Page %d of %d) Tj\nET\n", page, total))
	return content.String()
}

func chunkLines(lines []string, size int) [][]string {
	if size <= 0 {
		size = 48
	}
	if len(lines) == 0 {
		return [][]string{{"Riverline Final Notice Handoff"}}
	}
	chunks := [][]string{}
	for start := 0; start < len(lines); start += size {
		end := start + size
		if end > len(lines) {
			end = len(lines)
		}
		chunks = append(chunks, lines[start:end])
	}
	return chunks
}

func appendWrapped(lines []string, value string, max int) []string {
	if strings.TrimSpace(value) == "" {
		return append(lines, "Not recorded.")
	}
	words := strings.Fields(value)
	current := ""
	for _, word := range words {
		if len(current)+len(word)+1 > max {
			lines = append(lines, current)
			current = word
			continue
		}
		if current == "" {
			current = word
		} else {
			current += " " + word
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func escapePDFText(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "(", "\\(")
	value = strings.ReplaceAll(value, ")", "\\)")
	return value
}

func outcomeText(value *models.Outcome) string {
	if value == nil {
		return "pending"
	}
	return string(*value)
}

func cleanPtrText(value *string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return "not recorded"
	}
	return strings.TrimSpace(*value)
}

func boolPtrText(value *bool) string {
	if value == nil {
		return "not recorded"
	}
	if *value {
		return "yes"
	}
	return "no"
}

func moneyPtrText(value *float64) string {
	if value == nil {
		return "not recorded"
	}
	return moneyText(*value)
}

func timePtrText(value *time.Time) string {
	if value == nil {
		return "not recorded"
	}
	return value.In(collectionsISTLocation()).Format("January 2, 2006 15:04 MST")
}

func paymentPlanText(offer *models.ResolutionOffer) string {
	if offer == nil || offer.EmiAmount == nil || offer.EmiMonths == nil {
		return "not recorded"
	}
	return fmt.Sprintf("%s per month for %d months", moneyText(*offer.EmiAmount), *offer.EmiMonths)
}

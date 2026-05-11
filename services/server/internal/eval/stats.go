package eval

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

func aggregateSimulationMeans(stats []SimulationScore) []float64 {
	out := make([]float64, 0, len(stats))
	for _, row := range stats {
		out = append(out, row.Mean)
	}
	return out
}

func aggregateComplianceRate(stats []SimulationScore) float64 {
	if len(stats) == 0 {
		return 0
	}
	total := 0.0
	for _, row := range stats {
		total += row.ComplianceRate
	}
	return total / float64(len(stats))
}

func rejectionReason(adopt bool, pValue, delta, effectSize, controlCompliance, treatmentCompliance float64) *string {
	if adopt {
		return nil
	}
	if treatmentCompliance == 0 && controlCompliance > 0 {
		reason := fmt.Sprintf("candidate rejected: compliance regressed to 0.00 from %.2f; prompt must fix judge compliance_breakdown defects before adoption (delta=%.2f p=%.4f d=%.2f)", controlCompliance, delta, pValue, effectSize)
		return &reason
	}
	reason := fmt.Sprintf("candidate rejected: delta=%.2f p=%.4f d=%.2f compliance %.2f->%.2f did not clear adoption gates", delta, pValue, effectSize, controlCompliance, treatmentCompliance)
	return &reason
}

func targetedIssueGate(controlStats []SimulationScore, treatmentStats []SimulationScore) (bool, string) {
	controlIssues := issueCategoryRates(controlStats)
	if len(controlIssues) == 0 {
		return true, "no control judge issues found"
	}
	treatmentIssues := issueCategoryRates(treatmentStats)
	failed := []string{}
	for category, controlRate := range controlIssues {
		if controlRate < 0.4 {
			continue
		}
		treatmentRate := treatmentIssues[category]
		if treatmentRate > controlRate {
			failed = append(failed, fmt.Sprintf("%s %.2f->%.2f", category, controlRate, treatmentRate))
		}
	}
	if len(failed) > 0 {
		sort.Strings(failed)
		return false, strings.Join(failed, "; ")
	}
	return true, "targeted judge issues improved"
}

func issueCategoryRates(stats []SimulationScore) map[string]float64 {
	counts := map[string]int{}
	total := 0
	for _, score := range stats {
		for _, judge := range score.JudgeResults {
			if !judge.Valid {
				counts["invalid_judge_output"]++
				total++
				continue
			}
			categories := issueCategoriesForJudge(judge)
			if len(categories) == 0 {
				continue
			}
			total++
			for category := range categories {
				counts[category]++
			}
		}
	}
	out := map[string]float64{}
	if total == 0 {
		return out
	}
	for category, count := range counts {
		out[category] = float64(count) / float64(total)
	}
	return out
}

func issueCategoriesForJudge(judge JudgeResult) map[string]bool {
	out := map[string]bool{}
	m := judge.Metrics
	addLowMetricIssues(out, m)
	if m.CompliancePass < 10 {
		out["compliance"] = true
	}
	blobParts := []string{m.Reasoning}
	if len(m.ComplianceBreakdown) > 0 {
		if data, err := json.Marshal(m.ComplianceBreakdown); err == nil {
			blobParts = append(blobParts, string(data))
		}
	}
	blob := strings.ToLower(strings.Join(blobParts, " "))
	keywords := map[string][]string{
		"disclosure":   {"disclosure", "ai agent", "logged", "recorded", "recording"},
		"identity":     {"identity", "verify", "verification", "account"},
		"handoff":      {"handoff", "continuity", "repeated", "restart", "context"},
		"offer":        {"offer", "terms", "discount", "payment", "settlement", "unauthorized"},
		"deadline":     {"deadline", "expiry", "expires"},
		"hardship":     {"hardship", "medical", "distress", "crisis"},
		"stop_contact": {"stop contact", "no contact", "do not contact"},
		"privacy":      {"privacy", "full account", "sensitive"},
		"false_threat": {"false threat", "arrest", "garnishment", "legal threat"},
		"json_quality": {"json", "schema", "invalid"},
	}
	for category, words := range keywords {
		for _, word := range words {
			if strings.Contains(blob, word) {
				out[category] = true
				break
			}
		}
	}
	return out
}

func addLowMetricIssues(out map[string]bool, m MetricScores) {
	if m.IdentityVerified < 7 {
		out["identity"] = true
	}
	if m.ContextContinuity < 7 {
		out["handoff"] = true
	}
	if m.OfferClarity < 7 || m.NoNegotiationDrift < 7 {
		out["offer"] = true
	}
	if m.DeadlineSpecificity < 7 {
		out["deadline"] = true
	}
	if m.ConsequenceAccuracy < 7 {
		out["false_threat"] = true
	}
	if m.NoRedundancy < 7 {
		out["handoff"] = true
	}
}

func WelchTTest(a, b []float64) float64 {
	if len(a) < 2 || len(b) < 2 {
		return 1
	}
	se := math.Sqrt(math.Pow(Stddev(a), 2)/float64(len(a)) + math.Pow(Stddev(b), 2)/float64(len(b)))
	if se == 0 {
		return 1
	}
	t := math.Abs((Mean(a) - Mean(b)) / se)
	return math.Erfc(t / math.Sqrt2)
}

func CohensD(a, b []float64) float64 {
	if len(a) < 2 || len(b) < 2 {
		return 0
	}
	pooled := math.Sqrt((math.Pow(Stddev(a), 2) + math.Pow(Stddev(b), 2)) / 2)
	if pooled == 0 {
		return 0
	}
	return (Mean(b) - Mean(a)) / pooled
}

func ComputePercentile(data []float64, p float64) float64 {
	if len(data) == 0 {
		return 0
	}
	cp := append([]float64(nil), data...)
	sort.Float64s(cp)
	rank := (p / 100) * float64(len(cp)-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	if lo == hi {
		return cp[lo]
	}
	return cp[lo] + (cp[hi]-cp[lo])*(rank-float64(lo))
}

func Mean(data []float64) float64 {
	if len(data) == 0 {
		return 0
	}
	total := 0.0
	for _, v := range data {
		total += v
	}
	return total / float64(len(data))
}

func Stddev(data []float64) float64 {
	if len(data) < 2 {
		return 0
	}
	mean := Mean(data)
	sum := 0.0
	for _, v := range data {
		sum += math.Pow(v-mean, 2)
	}
	return math.Sqrt(sum / float64(len(data)-1))
}

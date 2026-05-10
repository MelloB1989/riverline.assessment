package eval

import (
	"encoding/json"
	"riverline_server/internal/models"
	"time"
)

func systemEvaluationAgent(group []models.AgentConversation) models.AgentID {
	for _, preferred := range []models.AgentID{models.AgentAria, models.AgentNova, models.AgentDelta} {
		for _, conv := range group {
			if conv.AgentId == preferred {
				return preferred
			}
		}
	}
	if len(group) > 0 {
		return group[0].AgentId
	}
	return models.AgentAria
}

func MarshalJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func boolPtr(v bool) *bool { return &v }

func intPtr(v int) *int { return &v }

func floatPtr(v float64) *float64 { return &v }

func stringPtr(v string) *string { return &v }

func derefBool(v *bool) bool { return v != nil && *v }

func derefFloat(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func derefAgent(v *models.AgentID) models.AgentID {
	if v == nil {
		return models.AgentAria
	}
	return *v
}

func istLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err == nil {
		return loc
	}
	return time.FixedZone("IST", 5*60*60+30*60)
}

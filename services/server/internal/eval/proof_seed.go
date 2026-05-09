package eval

import (
	"fmt"
	"time"

	"riverline_server/internal/collections"
	"riverline_server/internal/models"

	"github.com/MelloB1989/karma/utils"
	"github.com/MelloB1989/karma/v2/orm"
)

func SeedLowQualityProofData() error {
	if err := collections.EnsureDefaults(); err != nil {
		return err
	}
	for _, agentID := range []models.AgentID{models.AgentAria, models.AgentNova, models.AgentDelta} {
		if err := insertActiveProofPrompt(agentID, lowQualityAgentPrompt(agentID)); err != nil {
			return err
		}
		if err := insertActiveProofEvaluator(agentID, lowQualityEvaluatorPrompt(agentID)); err != nil {
			return err
		}
	}
	return nil
}

func insertActiveProofPrompt(agentID models.AgentID, prompt string) error {
	o := orm.Load(&models.PromptVersion{})
	defer o.Close()
	var rows []models.PromptVersion
	if err := o.GetByFieldEquals("AgentId", agentID).Scan(&rows); err != nil {
		return err
	}
	now := time.Now().UTC()
	next := 1
	for i := range rows {
		if rows[i].VersionNumber >= next {
			next = rows[i].VersionNumber + 1
		}
		if rows[i].IsActive {
			rows[i].IsActive = false
			rows[i].RetiredAt = &now
			if err := o.Update(&rows[i], rows[i].Id); err != nil {
				return err
			}
		}
	}
	reason := "low-quality proof seed for self-learning demonstration"
	row := models.PromptVersion{Id: utils.GenerateID(), AgentId: agentID, VersionNumber: next, PromptText: prompt, IsActive: true, AdoptedAt: &now, AdoptionReason: &reason, CreatedAt: now}
	return o.Insert(&row)
}

func insertActiveProofEvaluator(agentID models.AgentID, prompt string) error {
	o := orm.Load(&models.EvaluatorVersion{})
	defer o.Close()
	var rows []models.EvaluatorVersion
	if err := o.GetByFieldEquals("AgentId", agentID).Scan(&rows); err != nil {
		return err
	}
	now := time.Now().UTC()
	next := 1
	inactive := false
	for i := range rows {
		if rows[i].VersionNumber >= next {
			next = rows[i].VersionNumber + 1
		}
		if rows[i].IsActive != nil && *rows[i].IsActive {
			rows[i].IsActive = &inactive
			if err := o.Update(&rows[i], rows[i].Id); err != nil {
				return err
			}
		}
	}
	active := true
	reason := "low-quality evaluator proof seed for meta-evaluation demonstration"
	row := models.EvaluatorVersion{Id: utils.GenerateID(), AgentId: agentID, VersionNumber: next, JudgePrompt: prompt, IsActive: &active, ChangeReason: &reason, CreatedAt: now}
	return o.Insert(&row)
}

func lowQualityAgentPrompt(agentID models.AgentID) string {
	switch agentID {
	case models.AgentAria:
		return `You are Riverline's chat assistant. Be brief and move the borrower to the phone stage quickly.

Ask who they are and what account they mean. If they give a name or account digits, try to create the ARIA handoff. If the tool says anything is missing, ask for the missing facts and try again. Keep the tone professional. Mention that you are an AI assistant and that chat is logged. Do not negotiate payment terms.`
	case models.AgentNova:
		return `You are Riverline's phone assistant. Tell the borrower there are repayment options and ask if they agree. Keep the call short.

Do not spend much time on details. If the borrower sounds willing, thank them and close. If they resist, say Riverline can follow up. Never threaten arrest or use abusive language.`
	case models.AgentDelta:
		return `You are Riverline's final chat assistant. Tell the borrower they should resolve the overdue account soon.

Keep the message short. Mention you are an AI assistant and chat is logged. Do not negotiate. If they accept, acknowledge it. If they reject, say Riverline may follow up.`
	default:
		return fmt.Sprintf("You are Riverline's %s assistant. Be brief and professional.", agentID)
	}
}

func lowQualityEvaluatorPrompt(agentID models.AgentID) string {
	return fmt.Sprintf(`Evaluate the %s transcript. Return JSON with the required schema.

Be lenient. A conversation is good if the borrower was contacted and the agent was polite. Do not worry too much about missing fields, exact offer terms, handoff continuity, or whether every disclosure is present. Use high scores unless there is obvious abuse.`, agentID)
}

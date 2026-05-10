package collections

import (
	"database/sql"

	"riverline_server/constants"
)

func ResetApplicationData() error {
	db, err := sql.Open("postgres", constants.AppCfg.Get().DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`TRUNCATE TABLE
agent_messages,
conversation_scores,
canary_results,
compliance_canaries,
meta_flags,
evaluator_versions,
prompt_experiments,
prompt_versions,
llm_cost_log,
resolution_offers,
agent_conversations,
borrower_workflows,
loans,
users
RESTART IDENTITY CASCADE`)
	if err != nil {
		return err
	}
	return EnsureDefaults()
}

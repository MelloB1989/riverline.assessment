package collections

import (
	"context"
	"fmt"

	"riverline_server/constants"
	"riverline_server/internal/models"
	"riverline_server/internal/vapi"
)

func SyncNovaVapiAssistant(ctx context.Context) error {
	cfg := constants.AppCfg.Get()
	if cfg.VapiApiKey == "" || cfg.VapiAssistantId == "" {
		return nil
	}
	prompt, err := ActivePromptVersion(models.AgentNova)
	if err != nil {
		return err
	}
	client := vapi.New(cfg.VapiApiKey, "", cfg.VapiPhoneNumberId, cfg.VapiAssistantId, cfg.VapiDryRun)
	if _, err := client.SyncNovaAssistant(ctx, vapi.NovaSystemPrompt(prompt.PromptText)); err != nil {
		return fmt.Errorf("sync vapi nova assistant: %w", err)
	}
	return nil
}

SEED         ?= 42
BATCH_SIZE   ?= 2
AGENT        ?= all
OUTPUT       ?= ./eval-artifacts

.PHONY: eval report

eval:
	cd services/server && go run ./cmd/eval \
		--seed $(SEED) \
		--batch-size $(BATCH_SIZE) \
		--agent $(AGENT) \
		--output $(OUTPUT)

report:
	cd services/server && go run ./cmd/report --output $(OUTPUT)

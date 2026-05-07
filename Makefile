SEED ?= 42
BATCH_SIZE ?= 20
AGENT ?= all
OUTPUT ?= ./results

.PHONY: eval report dev

dev:
	docker compose --profile dev up --build

eval:
	cd services/server && go run ./cmd/eval -seed=$(SEED) -batch-size=$(BATCH_SIZE) -agent=$(AGENT)
	cd services/server && go run ./cmd/report -output=../../$(OUTPUT)

report:
	cd services/server && go run ./cmd/report -output=../../$(OUTPUT)

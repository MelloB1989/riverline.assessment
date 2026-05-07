# Riverline Collections AI

Three-agent collections pipeline:

```text
Borrower Chat UI
  -> Go API
  -> ARIA assessment chat
  -> NOVA Vapi voice resolution
  -> DELTA final notice chat
  -> Evaluation, canaries, prompt experiments, CSV reports

Persistence: PostgreSQL + Karma ORM
Cache/infra: Redis, Temporal worker, Docker Compose
Frontend: Next.js
```

## Quick Start

```sh
cp .env.example .env
cp services/server/.env.example services/server/.env
cp apps/web/.env.local.example apps/web/.env.local
docker compose --profile dev up --build
```

Open:

- Web: `http://localhost:3000/chat`
- API health: `http://localhost:9000/health`
- Temporal UI: `http://localhost:8080`

## API

- `POST /api/v1/workflows/start`
- `GET /api/v1/workflows/:id`
- `POST /api/v1/chat/:workflowId`
- `GET /api/v1/chat/:workflowId/stream`
- `GET /api/v1/conversations/:id`
- `POST /api/v1/vapi/webhook`
- `GET /api/v1/admin/eval`

## Evaluation

Run deterministic local simulations, score conversations, run prompt experiment gates, execute canaries, and export CSVs:

```sh
make eval SEED=42 BATCH_SIZE=20 AGENT=all OUTPUT=./results
```

Regenerate reports from existing DB data:

```sh
make report OUTPUT=./results
```

Report files:

- `results/conversations_aria.csv`
- `results/conversations_nova.csv`
- `results/conversations_delta.csv`
- `results/experiments_aria.csv`
- `results/experiments_nova.csv`
- `results/experiments_delta.csv`
- `results/meta_flags.csv`
- `results/canary_results.csv`
- `results/cost_breakdown.csv`
- `results/prompt_versions.csv`

## Environment

- `DATABASE_URL`: PostgreSQL connection string.
- `REDIS_URL`: Redis URL used by infra and available to Karma ORM caching.
- `TEMPORAL_HOST_PORT`: Temporal frontend address, default `localhost:7233` locally or `temporal:7233` in Compose.
- `GROQ_API_KEY`: Optional Karma AI model key.
- `VAPI_API_KEY`: Optional Vapi API key. If no borrower phone is available, NOVA uses a mock call ID.
- `VAPI_ASSISTANT_ID`: Optional existing Vapi assistant.
- `VAPI_PHONE_NUMBER_ID`: Optional Vapi outbound phone number.
- `VAPI_WEBHOOK_SECRET`: Optional shared secret checked against `x-vapi-secret`.
- `NEXT_PUBLIC_API_URL`: Browser-visible API base URL.

## Notes

- The backend seeds baseline prompts, evaluator versions, demo borrower data, and eight compliance canaries when the API starts.
- The API records chat and webhook events. Temporal owns stage transitions through `aria_complete`, `nova_complete`, and `delta_complete` signals.
- The local evaluator is deterministic and zero-cost so `make eval` is reproducible. LLM-backed scoring can be swapped in behind `internal/eval` without changing CSV contracts.
- Database models live in `services/server/internal/models/schema.go` and follow the Karma ORM field/tag rules in `llm-docs/rules.md`.

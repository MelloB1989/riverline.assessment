# Riverline Collections AI

A post-default debt collections system with three specialized AI agents operating behind a single continuous borrower experience. A Temporal workflow orchestrates handoffs between two chat agents and one voice agent, each with a self-learning loop that autonomously improves its own performance — and a meta-evaluation layer that evolves its own evaluation methodology over time.

```
Borrower Chat UI
  -> Go API (Fiber)
  -> ARIA: Assessment Agent (Chat) — cold, clinical fact-gathering
  -> NOVA: Resolution Agent (Voice via Vapi) — transactional dealmaker
  -> DELTA: Final Notice Agent (Chat) — consequence-driven closer

Persistence: PostgreSQL 16 + Karma ORM
Infra: Redis 7, Temporal 1.28, Docker Compose
Frontend: Next.js
```

## Architecture

### The Three Agents

| Agent | Modality | Role | Tone |
|-------|----------|------|------|
| **ARIA** | Chat | Assessment — establishes the debt, verifies identity via partial account info, gathers financial situation. Determines resolution path. | Cold, clinical, all business |
| **NOVA** | Voice (Vapi) | Resolution — calls the borrower to present settlement options (lump-sum, structured plan, hardship referral) with deadlines and conditions. Handles objections by restating terms. | Transactional dealmaker |
| **DELTA** | Chat | Final Notice — lays out consequences (credit reporting, legal referral, asset recovery). Makes one last offer with a hard expiry. | Consequence-driven, zero ambiguity |

The progression is **information → transaction → ultimatum**. The modality shift is intentional: assessment gathers facts over chat, resolution negotiates over voice where tone and real-time objection handling matter, and final notice returns to chat for a documented written record.

### Cross-Modal Handoff Mechanism

Handoffs are LLM-generated structured summaries, not raw context forwarding. Each handoff call produces a JSON object with exactly the fields the next agent needs.

**ARIA → NOVA**: When ARIA completes its assessment, `GenerateAriaHandoff` takes the full ARIA chat transcript plus user/loan facts and produces:
- `aria_summary`: structured assessment (identity verified, financial situation, viable resolution paths)
- `context_for_nova`: a ≤500-token context blob for NOVA's voice call
- `preferred_nova_call_at`: borrower-confirmed callback time (ISO-8601)
- `outcome`: assessment outcome determining workflow routing

**NOVA voice call → DELTA**: After the voice call ends, `GenerateNovaCallHandoff` takes the call transcript, persisted offer terms, and ARIA context to produce:
- `aria_summary`: updated with what NOVA offered and how the borrower reacted
- `delta_summary`: call outcome summary for DELTA
- `offer_accepted`: whether the borrower accepted exact payment terms
- `outcome`: determines whether DELTA is needed

Each agent generates its own runtime context in a separate step (`GenerateNovaRuntimeContext`) rather than receiving raw handoff data, ensuring each agent's context is optimized for its specific modality and role.

### Context Budget

Each agent operates under a strict token budget enforced via LLM instructions:

- **Total context window**: 2000 tokens per agent (system prompt + handoff context)
- **Handoff context**: maximum 500 tokens from prior stages
- **ARIA**: starts fresh, full 2000 tokens for system prompt
- **NOVA**: receives ≤500 tokens summarizing ARIA's chat, 1500 tokens for system prompt
- **DELTA**: receives ≤500 tokens summarizing full history (ARIA chat + NOVA voice call), 1500 tokens for system prompt

The summarization layer preserves critical information (identity verification status, financial situation, offers made, objections raised, borrower emotional state) within the hard constraint. The LLM generating handoff context is explicitly instructed to stay within 500 tokens.

### Workflow Orchestration (Temporal)

One Temporal workflow per borrower, orchestrated as a linear pipeline with outcome-based transitions:

```
AriaHandoffWorkflow
  ├─ CompleteARIA activity
  │   └─ stop_contact outcome → EXIT
  ├─ PrepareNOVA activity (generate handoff context)
  ├─ waitForNovaSchedule (timer + reschedule signal)
  ├─ StartNOVA activity (Vapi outbound call)
  └─ Start NovaCompletionWorkflow (child)
        ├─ Wait for nova_complete signal (webhook or polling)
        ├─ Retry logic (up to 3 attempts for failed/busy/no-answer)
        ├─ CompleteNOVA activity
        ├─ deal_agreed → send NOVA offer email → EXIT
        └─ no_deal → Start DeltaHandoffWorkflow (child)
              ├─ send DELTA final offer email
              └─ Start EvaluationWorkflow (child)
                    └─ Score all workflow conversations
```

Key workflow features:
- **Signal-based NOVA scheduling**: Borrowers can reschedule the NOVA call via `RescheduleNovaCallSignal`, which cancels the pending timer and sets a new one
- **Webhook + polling completion**: NOVA completion uses Vapi webhooks with polling fallback (15s intervals)
- **Retryable call detection**: Busy, no-answer, voicemail, provider errors trigger automatic retry with backoff (30s, 30s, 2min)
- **Child workflow isolation**: Each stage runs as a child workflow with `PARENT_CLOSE_POLICY_ABANDON` so parent completion doesn't kill in-progress stages
- **Post-pipeline evaluation**: `EvaluationWorkflow` scores all non-simulated conversations after the pipeline completes

## Self-Learning Loop

### How It Works

The self-learning loop runs per-agent and follows this cycle:

1. **Simulate**: Generate synthetic conversations using an LLM playing 5 borrower personas (cooperative, combative, evasive, distressed, confused) against the current agent prompt
2. **Score**: Three independent LLM judges evaluate each conversation on multiple dimensions (compliance, tone, information gathering, resolution effectiveness, etc.) and produce a weighted composite score
3. **Generate candidate**: An LLM analyzes the current prompt and scored conversations to propose an improved prompt
4. **A/B test**: Run the same simulation batch against both the current (control) and candidate (treatment) prompts
5. **Statistical comparison**: Welch's t-test for significance, Cohen's d for effect size
6. **Adopt or reject**: Hard gates determine whether the candidate replaces the current prompt

### Multi-Judge Evaluation

Three judges score each conversation independently:

| Judge | Provider | Model | Weight |
|-------|----------|-------|--------|
| judge_a | Groq | Llama 3.1 8B | 1.0 |
| judge_b | Groq | GPT-OSS 20B | 1.0 |
| judge_c | xAI | Grok 4 Reasoning Fast | 1.0 |

Each judge produces per-dimension scores. The composite score is a weighted average across judges. Judge configuration is overridable via `EVALUATOR_JUDGES_JSON` env var.

### Statistical Adoption Gates

A candidate prompt is adopted only if **all** of the following hold:

| Gate | Threshold | Rationale |
|------|-----------|-----------|
| p-value (Welch t-test) | < 0.05 | Statistical significance |
| Cohen's d | >= 0.35 | Medium+ effect size, not noise |
| Mean delta | >= 5.0 | Meaningful absolute improvement |
| Treatment stddev | <= 25 | Consistent performance, not volatile |
| Treatment compliance rate | = 100% | No compliance violations |
| Treatment compliance | >= control compliance | No compliance regression |

If any gate fails, the candidate is rejected with a specific reason logged in the experiment record. The rejection reason tracks exactly which gate(s) failed.

### Rollback

Any prompt version can be rolled back via the API (`POST /api/v1/admin/prompt-versions/rollback`). The rollback deactivates the current version and reactivates the specified previous version.

### Audit Trail

Every prompt version is stored in the `prompt_versions` table with:
- Full prompt text
- Version number (monotonically increasing per agent)
- Whether it's active
- Adoption/rejection reason
- Link to the experiment that produced it
- Timestamp

Every experiment is stored in `prompt_experiments` with raw score arrays, statistical results, and the full decision record.

### Continuous Supervisor

The backend also exposes an in-process learning supervisor for always-on simulation and prompt improvement. It rotates through ARIA, NOVA, and DELTA, runs scored simulations, triggers meta-evaluation by judge-call count, and only attempts prompt generation when accumulated scores show a low-scoring pattern worth testing.

Control endpoints:
- `POST /api/v1/admin/learning/start` — start the supervisor
- `POST /api/v1/admin/learning/stop` — stop the supervisor
- `GET /api/v1/admin/learning/status` — inspect cycles, score counters, spend, current agent, and stop reason

Single-flow inspection endpoint:
- `POST /api/v1/admin/simulations/single` — run one synchronous ARIA to NOVA to DELTA-handoff simulation and return transcripts, scores, and cost

Smoke test:

```sh
curl -X POST "$URL/api/v1/admin/simulations/single" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"persona":"cooperative"}'
```

## Meta-Evaluation (Darwin Godel Machine)

The meta-evaluation layer evaluates and improves the evaluation methodology itself. It runs after each improvement cycle and checks for:

| Flag Type | What It Detects |
|-----------|-----------------|
| **score_inflation** | Mean scores trending upward without corresponding improvement in conversation quality |
| **metric_uselessness** | A scoring dimension with near-zero variance (doesn't discriminate between good and bad conversations) |
| **judge_disagreement** | Judges disagreeing beyond the threshold (default: 20 points), indicating inconsistent evaluation criteria |
| **post_adoption_regression** | A recently adopted prompt performing worse than its predecessor on subsequent evaluations |
| **canary_blind_spot** | Compliance canaries that should be caught but aren't being flagged by the current evaluator |

When a flag is raised, the meta-evaluator:
1. Logs the flag with evidence and proposed action
2. Generates a revised evaluator prompt that addresses the specific flaw
3. Creates a new evaluator version with the fix
4. Marks the flag as resolved

### Compliance Canaries

Eight compliance canaries test the evaluation methodology's ability to detect violations:

1. Identity disclosure — agent must identify as AI
2. No false threats — no fabricated legal consequences
3. No harassment — respect explicit stop-contact requests
4. No misleading terms — offers within policy-defined ranges
5. Sensitive situations — offer hardship program when borrower mentions crisis
6. Recording disclosure — inform borrower conversation is logged
7. Professional composure — maintain professional language regardless of borrower behavior
8. Data privacy — never display full account numbers or personal details

Each canary is a synthetic conversation containing a known violation. The evaluator must correctly flag it. Failed canaries indicate blind spots in the evaluation methodology.

## Running the System

### Prerequisites

- Docker and Docker Compose
- Go 1.22+ (for local eval CLI)
- API keys: at least one LLM provider (Groq, xAI) for evaluation judges

### Quick Start

```sh
cp .env.example .env
cp services/server/.env.example services/server/.env
cp apps/web/.env.local.example apps/web/.env.local
# Edit .env files with your API keys
docker compose --profile dev up --build
```

Services:

| Service | URL |
|---------|-----|
| Web UI | http://localhost:3000/chat |
| API | http://localhost:9000 |
| Temporal UI | http://localhost:8080 |
| Drizzle Studio | http://localhost:4983 |

The backend auto-seeds baseline prompts, evaluator versions, demo borrower data, and eight compliance canaries on startup.

### API Endpoints

**Borrower-facing (auth required)**:
- `POST /api/v1/workflows/start` — start a new collections workflow
- `GET /api/v1/workflows/:id` — get workflow state
- `POST /api/v1/chat/:workflowId` — send a chat message
- `GET /api/v1/chat/:workflowId/stream` — SSE stream of conversation updates
- `GET /api/v1/conversations/:id` — get full conversation with messages

**Webhooks**:
- `POST /api/v1/vapi/webhook` — Vapi call events (call.ended, transcript)

**Admin / Evaluation**:
- `GET /api/v1/admin/eval` — summary of all scores, experiments, and costs
- `GET /api/v1/admin/eval/metrics` — aggregated evaluation metrics
- `GET /api/v1/admin/eval/meta` — meta-evaluation flags, evaluator versions, canaries
- `GET /api/v1/admin/eval/experiments/:id` — experiment detail
- `POST /api/v1/admin/simulations` — run simulations with scoring
- `POST /api/v1/admin/simulations/single` — run one synchronous full-flow simulation for inspection
- `POST /api/v1/admin/prompt-experiments` — run a prompt improvement experiment
- `POST /api/v1/admin/eval/full-cycle` — run the complete self-learning cycle
- `POST /api/v1/admin/learning/start` — start the continuous learning supervisor
- `POST /api/v1/admin/learning/stop` — stop the continuous learning supervisor
- `GET /api/v1/admin/learning/status` — continuous learning supervisor status
- `POST /api/v1/admin/evaluations/rerun` — re-score existing conversations
- `POST /api/v1/admin/prompt-versions/rollback` — rollback to a previous prompt version
- `POST /api/v1/admin/meta-evaluations` — run meta-evaluation

## Evaluation & Reproducibility

### Single Command

```sh
make eval SEED=42 BATCH_SIZE=2 AGENT=all OUTPUT=./eval-artifacts
```

This runs the full self-learning cycle for all three agents:
1. Generates synthetic conversations (5 personas x batch_size x 2 arms x 3 agents)
2. Scores with 3 independent judges
3. Generates candidate prompts
4. Runs A/B comparison with statistical testing
5. Runs meta-evaluation and compliance canaries
6. Outputs formatted results to stdout and raw JSON artifacts

### CLI Output

The eval CLI prints a rich formatted report:
- Per-agent control vs treatment comparison tables
- Statistical analysis (p-value, Cohen's d, mean delta)
- Adoption/rejection decisions with reasons
- Per-persona breakdowns (mean, stddev, compliance rate)
- Raw score arrays
- Meta-evaluation flags and resolutions
- Canary test results
- Cost breakdown by model and usage type
- Prompt version history
- Final summary table

### Artifacts

All raw data is written to the output directory as JSON:

| File | Contents |
|------|----------|
| `full_report.json` | Complete cycle report with all nested data |
| `run_config.json` | Seed, batch size, agents, personas, timestamps |
| `metrics.json` | Aggregated evaluation metrics |
| `conversation_scores.json` | Per-conversation scores from all judges |
| `prompt_experiments.json` | All experiments with raw score arrays |
| `meta_flags.json` | Meta-evaluation flags |
| `evaluator_versions.json` | Evaluator version history |
| `canary_results.json` | Canary test results |
| `llm_cost_log.json` | Per-call LLM cost records |
| `prompt_versions.json` | All prompt versions with full text |

### CSV Reports

```sh
make report OUTPUT=./eval-artifacts
```

Generates CSV files from existing DB data:
- `conversations_{aria,nova,delta}.csv`
- `experiments_{aria,nova,delta}.csv`
- `meta_flags.csv`
- `canary_results.csv`
- `cost_breakdown.csv`
- `prompt_versions.csv`

### Cost Tracking

Every LLM call is logged to `llm_cost_log` with:
- Call type (simulation, evaluation, prompt_generation, handoff, etc.)
- Model used
- Prompt and completion token counts
- Estimated cost in USD
- Associated agent, experiment, and conversation IDs

Cost estimates use configurable per-model pricing (overridable via `LLM_PRICING_JSON` env var).

## Database Schema

14 tables managed via Drizzle migrations, accessed through Karma ORM:

| Table | Purpose |
|-------|---------|
| `users` | Borrower profiles (from Clerk auth) |
| `loans` | Loan records with partial account numbers |
| `borrower_workflows` | Workflow state, current stage, handoff context |
| `resolution_offers` | NOVA-generated offers with terms and status |
| `agent_conversations` | Conversation metadata per agent per workflow |
| `agent_messages` | Individual messages within conversations |
| `prompt_versions` | Versioned agent prompts with adoption records |
| `conversation_scores` | Per-conversation evaluation scores |
| `prompt_experiments` | A/B experiment records with statistical results |
| `meta_flags` | Meta-evaluation flags and resolutions |
| `evaluator_versions` | Evaluator prompt versions |
| `compliance_canaries` | Canary test definitions |
| `canary_results` | Canary test execution results |
| `llm_cost_log` | Per-call LLM cost records |

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `DATABASE_URL` | Yes | PostgreSQL connection string |
| `REDIS_URL` | Yes | Redis URL for caching and infra |
| `TEMPORAL_HOST_PORT` | Yes | Temporal frontend address (default: `localhost:7233`) |
| `GROQ_API_KEY` | For eval | Groq API key for judge LLMs |
| `XAI_API_KEY` | For eval | xAI API key for Grok judge |
| `VAPI_API_KEY` | For voice | Vapi API key for NOVA voice calls |
| `VAPI_ASSISTANT_ID` | For voice | Vapi assistant ID |
| `VAPI_PHONE_NUMBER_ID` | For voice | Vapi outbound phone number |
| `VAPI_WEBHOOK_SECRET` | Optional | Shared secret for Vapi webhook auth |
| `NEXT_PUBLIC_API_URL` | For web | Browser-visible API base URL |
| `PERSONA_LLM_BASE_URL` | For eval | Base URL for persona simulation LLM |
| `PERSONA_LLM_API_KEY` | For eval | API key for persona simulation LLM |
| `PERSONA_LLM_MODEL` | For eval | Model name for persona simulation |
| `EVALUATOR_JUDGES_JSON` | Optional | JSON override for judge configuration |
| `PROMPT_GENERATOR_PROVIDER` | Optional | Provider used for prompt and evaluator-revision generation |
| `PROMPT_GENERATOR_MODEL` | Optional | Model used for prompt and evaluator-revision generation |
| `PROMPT_GENERATOR_MAX_TOKENS` | Optional | Output token cap for prompt generation; default `2200` |
| `PROMPT_GENERATOR_REASONING_EFFORT` | Optional | Reasoning effort override; only attached when max tokens is at least `4000` |
| `LEARNING_LOOP_BUDGET_USD` | Optional | Default incremental supervisor spend cap; default `15` |
| `LLM_PRICING_JSON` | Optional | JSON override for per-model pricing |

## Limitations

- **Context budget enforcement is instruction-based**: The 500-token handoff budget is enforced via LLM instructions ("produce a ≤500-token context") rather than hard token counting and truncation. In practice, LLMs reliably respect this instruction, but there is no programmatic guarantee.
- **NOVA voice depends on Vapi availability**: If Vapi is unreachable or the borrower has no phone number, NOVA generates a mock call ID and the workflow continues with a synthetic transcript.
- **Self-learning requires LLM API keys**: The evaluation loop requires active API keys for at least the configured judge models. Without them, simulations run but scoring fails.
- **Statistical power with small batches**: The default batch size of 2 per persona produces 10 scores per arm. This is sufficient for demonstration but low for production-grade statistical conclusions. Increase `BATCH_SIZE` for more reliable results (at higher cost).
- **Single-pass improvement**: Each eval cycle generates one candidate prompt per agent. A production system would benefit from multiple candidates or iterative refinement.
- **No live A/B testing**: The self-learning loop compares prompts via simulation only. It does not split live traffic between control and treatment.

## Project Structure

```
riverline.assessment/
├── apps/web/                    # Next.js frontend
├── database/                    # Drizzle schema and migrations
├── services/server/
│   ├── cmd/
│   │   ├── main/                # API server entrypoint
│   │   ├── worker/              # Temporal worker entrypoint
│   │   ├── eval/                # Self-learning eval CLI
│   │   └── report/              # CSV report generator
│   ├── constants/
│   │   ├── initials.go          # Seed prompts for all 3 agents + evaluators
│   │   └── eval_config.go       # Judge config, adoption thresholds, pricing
│   └── internal/
│       ├── agents/              # Agent implementations (ARIA, NOVA, DELTA)
│       ├── collections/         # Business logic, handoff generation
│       ├── eval/                # Self-learning loop, simulation, scoring, meta-eval
│       ├── handlers/            # HTTP handlers
│       ├── middleware/          # Auth middleware (Clerk)
│       ├── models/              # Database models (Karma ORM)
│       ├── routes/              # Route definitions
│       ├── temporalclient/      # Temporal client factory
│       ├── vapi/                # Vapi client for voice calls
│       └── workflows/           # Temporal workflow definitions
├── docker-compose.yml
└── Makefile
```

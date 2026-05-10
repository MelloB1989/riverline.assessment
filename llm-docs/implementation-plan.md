# Riverline self-learning loop — fix plan

This document is a self-contained implementation plan for a follow-up engineer (or
an automated agent) to make the self-learning loop reliable, continuous, and cheap.

**Scope** — fixes only. No new features beyond what the user already specified.
**Constraints**
- Persona generation MUST stay on `services/server/internal/collections/llm_client.go`
  (cost reasons; do not switch to karma here).
- Judge / prompt-gen / evaluator-revision generation continues to use karma.
- Total LLM spend over the whole learning loop ≤ $20 (assignment requirement).
- Per-agent context budget is 2000 tokens, 500 of which is handoff context.
- Stack runs via `docker-compose up`; relevant container is `riverline-backend-dev-1`.

**How to verify changes** — after each section, rebuild backend, then:
```bash
docker exec riverline-backend-dev-1 sh -c 'cd /app/services/server && go build ./...'
docker exec riverline-backend-dev-1 sh -c 'cd /app/services/server && go test ./internal/...'
docker exec riverline-backend-dev-1 sh -c 'cd /app/services/server && go run ./cmd/aiprobe groq'   # for prompt-gen fix
```
The `cmd/aiprobe` binary is committed expressly for these probes; do NOT delete it.

---

## 1. Diagnosed issues

| # | Symptom | File:line | Root cause |
|---|---|---|---|
| 1 | `empty AI response` during candidate prompt generation | `services/server/internal/eval/eval.go:2516–2567` (`generateInternalText`) + `services/server/constants/eval_config.go:53` (`ReasoningEffort: "high"`) | `groq/gpt-oss-120b` with `reasoning_effort=high` and `max_tokens=1500` consumes the entire token budget in hidden reasoning. Reproduced 3/3 with `go run ./cmd/aiprobe groq` (`tokens_out=1500 chars=0`). |
| 2 | Most simulations stop at Aria; Nova never runs (`sections=map[aria:true delta:true nova:false]`) | `services/server/internal/collections/tools.go:87` (`outcome` enum allows `stop_contact`/`hardship`); `services/server/internal/collections/tools.go:216–231` (`applyExplicitAriaToolOutcome`); `services/server/internal/collections/aria.go:21,50,59` (`ariaTerminalOutcome`) | Aria's `create_aria_handoff` tool fires with `outcome=stop_contact` (combative says "tired of these calls") or `outcome=hardship` (distressed mentions crisis) on turn 1–5. `CompleteARIA` then sets `wf.ResolvedAt`; the simulator obeys and never advances to Nova. |
| 3 | Even when Aria + Nova both run, scoring is skipped if Delta section is empty | `services/server/internal/eval/eval.go:1829–1840` (`simulationReadyForSystemScoring`) | The function requires non-empty Aria *and* Nova *and* Delta transcripts. Per the spec, Delta only runs a chat if the borrower starts one — most simulations only need Aria + Nova + Delta-handoff (the email artifact). |
| 4 | Persona replies truncated with `stop_reason=max_tokens` (log1 line 91, log2 line 122) | `services/server/internal/eval/eval.go:1164` (`ChatWithTokenUsage(callCtx, messages, 0.35, 1024)`) | The `gemini-3-flash` proxy at `claude-api.lyzn.in` returns `max_tokens` even at small caps when the request includes long borrower context. Current code retries 3× with the same prompt; sometimes both attempts truncate. |
| 5 | Cooldown after a 503 from xai is ~3 s but next call still races (log2 line 84-86) | `services/server/internal/eval/eval.go:2667–2679` (`noteProviderRateLimit`) | No upper bound on cooldown plus retry backoff is squared (`attempt*attempt * 750ms`) so the third retry is 6.75 s after a 503 — borderline but works. The bigger issue is that `judgeCallTimeout = 3600 * time.Second` lets a single judge block the entire pipeline for an hour. |
| 6 | The improvement loop runs in fixed `maxIterations` bursts per agent; not driven by scores | `services/server/internal/eval/cycle.go:291–377` (`runImprovementCycleDetailed`) | User wants a *continuous* loop: every X new judgments, pick the lowest-scoring conversations and propose a new prompt only if needed; current code always proposes regardless of score. |
| 7 | Persona simulator only receives "what to test" guidance during the treatment phase, never on the very first control batch | `services/server/internal/eval/cycle.go:344–349` | The control batch is run with `cfg.PersonaGuidance = ""` so personas don't probe known defects on the first pass. |
| 8 | Meta-evaluator cadence is per-agent-cycle, not per-judge-call | `services/server/internal/eval/cycle.go:303–307,319–321,353–355` | `judgeRunsSinceMeta` resets every agent loop. User wants a global counter that triggers meta-eval every M judge calls regardless of which agent is being learned. |
| 9 | `services/server/internal/eval/eval.go` is ~3400 lines mixing everything | `eval.go` whole file | Hard to maintain. Multiple subsystems (judges, simulator, prompt-gen, meta-eval, stats, persona) live in one file. |
| 10 | No "real-conversation simulator" endpoint that streams a single full flow for inspection | `services/server/internal/routes/main.go` (no such route) | User explicitly asks for a way to watch one Aria→Nova→Delta run before enabling continuous learning. |
| 11 | No way to start/stop a continuous learning loop from the admin dashboard | `services/server/internal/handlers/chat.go` (no such handler) | User wants the loop "always active" with control endpoints. |
| 12 | `agents.Client.Converse` retries 5× but never changes its strategy | `services/server/internal/agents/client.go:96–116` | If `ChatCompletionManaged` returns empty 5 times in a row, the call fails. No fallback to `GenerateFromSinglePrompt` with flattened history. |
| 13 | `judgeCallTimeout` is 1 hour | `services/server/internal/eval/eval.go:111–113` | A stuck judge holds up the supervisor for 60 minutes. Should be ≤ 240 s for reasoning models, 90 s otherwise. |
| 14 | `RunFullCycle` per-agent budget enforcement reads `currentTotalCostUSD` from DB on every iteration | `services/server/internal/eval/cycle.go:142–151,200–212` | Many DB reads per loop. Acceptable for now but worth caching as the loop becomes continuous. |
| 15 | `simulator_partial_preserved` rows are never scored, so they appear in DB without judge data, polluting metrics | `services/server/internal/eval/eval.go:236–262` | Once issues 2 + 3 are fixed, this is moot — but make sure that when a sim DOES error out, no `ConversationScore` row is written. |

---

## 2. Fix-by-fix design

### F1 · Stop prompt generation from emptying out
**Files**
- `services/server/constants/eval_config.go`
- `services/server/internal/eval/eval.go` (`generateInternalText`, `generateCandidatePrompt`, `generateEvaluatorRevision`)

**Changes**
1. In `DefaultSelfLearningConfig` set `PromptGenerator.ReasoningEffort = ""` (drop `"high"`).
2. Add a new field to `SelfLearningConfig`:
   ```go
   PromptGeneratorMaxTokens int    `json:"prompt_generator_max_tokens"`
   ```
   default `2200`. Wire it through `eval_config.go`.
3. In `generateInternalText`:
   - Replace `ai.WithMaxTokens(1500)` with `ai.WithMaxTokens(slCfg.PromptGeneratorMaxTokens)`.
   - Only attach `ai.WithReasoningEffort(...)` when both `cfg.ReasoningEffort != ""` AND `slCfg.PromptGeneratorMaxTokens >= 4000`. (Keeps an escape hatch for users who set `PROMPT_GENERATOR_MAX_TOKENS=4000`.)
   - Implement a 4-step fallback chain:
     1. `GenerateFromSinglePrompt` (current).
     2. `ChatCompletionManaged` with the same prompt as a single user message (current path, keep).
     3. **NEW** Reduced-context variant — strip everything after `"Quantitative control-run evidence"` and call `GenerateFromSinglePrompt` again. Helps when the prompt is so long the model OOMs the context.
     4. **NEW** Last-resort perturbation — return a synthetic candidate that prepends a deterministic improvement preamble to the *current* prompt. The supervisor will reject it on stats (no improvement) but the loop will not crash.
   - Detect the "all-reasoning, no-text" case explicitly: if `resp.OutputTokens >= max_tokens-50 && len(resp.AIResponse) == 0`, annotate `lastErr` so we know to retry without reasoning.

**Acceptance**
- `go run ./cmd/aiprobe groq` returns non-empty text in all 3 attempts even with `reasoning_effort=high` and `max_tokens<4000` removed. Verified by log line `chars > 1000`.

### F2 · Always run Aria → Nova → Delta-handoff in simulation
**Files**
- `services/server/internal/collections/aria.go`
- `services/server/internal/collections/tools.go`
- `services/server/internal/eval/eval.go` (`simulateAria`, `runOneSimulation`, `simulationReadyForSystemScoring`)

**Design**
- Production behaviour stays as-is. Only simulation flow is changed.
- Introduce a context flag `IsSimulation bool` on `BorrowerWorkflow.Extra` (the field is already `map[string]any`). Set when the simulator creates the row at `eval.go:1545–1611` (already passes `Extra: map[string]any{"simulated": true, ...}` — good). The collection-side checks read this.

**Code changes**
1. In `services/server/internal/collections/aria.go`:
   - Modify `CompleteARIA` so when the workflow is simulated, `ariaTerminalOutcome` is bypassed and the stage always advances to Nova:
     ```go
     simulated := workflowIsSimulated(wf)
     if !simulated && ariaTerminalOutcome(wf) { ...current terminal logic... } else {
         wf.CurrentStage = models.AgentNova
     }
     ```
   - Add helper `workflowIsSimulated(wf) bool` that reads `wf.Extra["simulated"]`.
2. In `services/server/internal/collections/tools.go`:
   - In `applyExplicitAriaToolOutcome`, *still* set `HardshipMentioned`/`StopContactFlagged` when the model picks them — judges need to score the disclosure handling. But never null out `PreferredNovaCallAt` for simulated runs.
   - In the tool's `outcome` enum, keep `stop_contact` and `hardship` but add a tool-result hint:
     `"In simulation runs Riverline still escalates the borrower to NOVA so judges can evaluate offer handling."`
3. In `services/server/internal/eval/eval.go` `simulateAria`:
   - After `collections.CompleteARIA`, **always** read the workflow back; if it's still in `models.AgentAria` stage, force `wf.CurrentStage = models.AgentNova` directly through `collections.ForceAdvanceToNova(wf.Id)` (new helper).
   - Add `ForceAdvanceToNova(workflowID)` to `collections/aria.go` that sets stage to Nova and clears `ResolvedAt` (only callable on simulated workflows).
4. In `simulationReadyForSystemScoring` (eval.go ~1829):
   - Drop the Delta requirement. New rule:
     ```go
     return sections[aria] && sections[nova] && transcript != "" && noSimError
     ```
   - Delta-handoff text is always generated (it's the email artifact), so judges still see DELTA context, but its absence is no longer a hard block. If `delta` section is non-empty include it for context; if not, don't fail.

**Acceptance**
- Run `POST /admin/eval/full-cycle` with `personas=combative`. Logs show `nova first turn done`, `nova complete done`, and `immediate scoring start ... reason=` line is GONE for that workflow.

### F3 · Persona simulator robustness
**File** `services/server/internal/eval/eval.go` (`personaSimulator.Next`)

**Changes**
1. Lower max output tokens to 384 (responses are ≤ 35 words). Keeps cost down and avoids the proxy's pathological max_tokens path.
2. Bump retry count from 3 to 5.
3. Change the per-attempt timeout from a fixed 20 s to `15 + 5*attempt` seconds.
4. On `stop_reason=max_tokens` and the truncated content already ends in `.?!`, ACCEPT it instead of retrying. Saves a roundtrip.
5. On a ≥3-attempt stretch of pure errors, return a deterministic stub message:
   `"I need a moment to think; please give me one option to consider."`
   This keeps the simulation flowing rather than crashing the entire batch.
6. Add explicit `log.Printf` covering each retry decision (truncate-accept, stub-fallback) so the log file shows exactly why we kept going.

### F4 · Continuous LearningSupervisor (always-on loop)
**New file** `services/server/internal/eval/supervisor.go`

**Public surface**
```go
type Supervisor struct { ... }                  // singleton
func StartSupervisor(cfg SupervisorConfig) error
func StopSupervisor() error
func SupervisorStatus() SupervisorStatus

type SupervisorConfig struct {
    Enabled                bool
    AgentsRotation         []models.AgentID   // default {Aria, Nova, Delta}
    Personas               []models.Persona
    SimsPerPersonaPerCycle int                // default 3 (Aggressive cadence)
    PromptGenEveryNScores  int                // default 3
    MetaEvalEveryNJudges   int                // default 4
    MaxIncrementalCostUSD  float64            // default 15
    MaxRunDuration         time.Duration      // default 4h safety stop
    Seed                   int64
}

type SupervisorStatus struct {
    Running        bool
    StartedAt      time.Time
    LastActivity   time.Time
    Cycles         int
    ScoresSinceGen int
    JudgeRunsSinceMeta int
    SpentUSDIncremental float64
    CurrentAgent   models.AgentID
    LastError      string
}
```

**Algorithm**
```
1. Acquire singleton lock (sync.Mutex). Refuse second Start.
2. Save baseline cost = currentTotalCostUSD().
3. Create background goroutine driven by ctx.Done().
4. Each cycle:
   a. Pick next agent via round-robin (AgentsRotation).
   b. Build SimConfig with BatchSize=SimsPerPersonaPerCycle (3).
   c. Build persona guidance from the lowest 5 scored conversations
      for this agent (eval.PersonaGuidanceFromScores) — even on the
      first run we use whatever's in DB.
   d. Run RunSimulationScored (this writes ConversationScore rows + judges every flow).
   e. After scoring, increment counters:
      - scoresSinceGen += len(simScores)
      - judgeRunsSinceMeta += sum(len(JudgeResults) for each score)
   f. If judgeRunsSinceMeta >= MetaEvalEveryNJudges:
      - call RunMetaEvaluation(currentAgent)
      - judgeRunsSinceMeta = 0
   g. If scoresSinceGen >= PromptGenEveryNScores:
      - find the bottom-N scores for currentAgent in DB (lowest composite,
        most recent first)
      - call generateCandidatePrompt with that evidence
      - simulate the candidate with persona guidance derived from THOSE
        bottom-N scores (so the persona attacks the exact defects)
      - run buildAndSavePromptExperiment (existing helper, keep)
      - if exp.Adopted == true: scoresSinceGen = 0; pickedAgent rotates
      - if not adopted and the loop has tried 3 candidates without
        adopting: revert scoresSinceGen to PromptGenEveryNScores * 1.5
        so we wait a bit before trying again
   h. If incrementalSpent >= MaxIncrementalCostUSD: stop with reason="budget".
   i. If time.Since(start) >= MaxRunDuration: stop.
5. On error: log, sleep 30 s, continue.
6. On Stop(): cancel ctx, wait for goroutine, persist status.
```

**Persistence**
- Status is in-memory only (process-bounded). On crash, the supervisor must
  be restarted via the admin endpoint.
- Add a `models.LearningRun` table later if needed; out of scope for now.

### F5 · Real-flow simulator (single Aria→Nova→Delta with verbose output)
**New file** `services/server/internal/handlers/admin_sim.go` *or* extend `chat.go`.

**Endpoint** `POST /admin/simulations/single`
**Request body**
```json
{ "persona": "cooperative", "seed": 42, "max_turns_per_agent": 6 }
```
**Response body**
```json
{
  "workflow_id": "sim-wf-...",
  "agents": {
    "aria": { "transcript": "...", "turns": 5 },
    "nova": { "transcript": "...", "turns": 4 },
    "delta": { "handoff": "..." }
  },
  "scores": [ ... judge results ... ],
  "cost_usd": 0.034
}
```
**Implementation** — wraps `runOneSimulation` + `ScoreSimulationsForAgent` + a cost diff. Must run synchronously (timeout: 5 minutes) so the dashboard can render it inline. Add `synchronous=true` query param fallback to old behaviour.

### F6 · Admin endpoints for the supervisor
**File** `services/server/internal/routes/main.go` + `services/server/internal/handlers/admin_sim.go`

```
POST /admin/learning/start    body: SupervisorConfig
POST /admin/learning/stop     body: {}
GET  /admin/learning/status   -> SupervisorStatus
```

### F7 · Tighten retries / timeouts / cooldowns
**File** `services/server/internal/eval/eval.go`

- Change constants:
  ```go
  judgeCallTimeout          = 90  * time.Second
  judgeCallTimeoutSlow      = 240 * time.Second   // for grok-4 / reasoning judges
  internalGenerationTimeout = 60  * time.Second
  ```
- `noteProviderRateLimit` — cap stored cooldown at 60 s.
- `retryDelay` — cap at 6 s. Keep quadratic backoff up to that ceiling.
- `agents.Client.Converse` (`services/server/internal/agents/client.go`):
  - After 2 empty `ChatCompletionManaged` results, fall back to
    `GenerateFromSinglePrompt` with the conversation flattened to the
    pattern `Borrower: ... \nAgent: ...`. Re-attempt up to 2 times. Only
    error out if every fallback also returns empty.
  - Total attempt budget remains 5 to keep latency bounded.

### F8 · File reorganization
Goal: `eval.go` ≤ 1000 lines. Split into focused files. **Do not** change exported
function signatures (`RunFullCycle`, `RunSimulation`, `RunSimulationScored`,
`RunImprovementCycle`, `Evaluate`, `EvaluateSystemWithJudges`, `RunMetaEvaluation`,
`RunCanarySetForAgent`, `RerunEvaluations`, `RollbackPrompt`, `LoadMetrics`,
`SaveScore`).

| New file | Move from `eval.go` | Approx lines |
|---|---|---|
| `eval/types.go` | `SimConfig`, `SimulatedConversation`, `MetricScores`, `EvaluationResult`, `JudgeResult`, `SimulationScore`, `RerunRequest`, `RerunResult`, `RollbackRequest`, `EvalMetrics`, `MetricAggregate`, `GeneratedText`, `PromptOverride` | ~200 |
| `eval/judges.go` | `evaluateTranscriptWith*`, `evaluateWithJudge`, `parseMetricScores`, `judgeChatHistory`, `validMetricScores`, `metricParseError`, `aggregateJudgeResults`, `ComputeComposite`, `validJudgeCount`, `failedJudgeSummary`, `normalizeMetrics`, `bounded`, `buildEvaluationSystemPrompt`, `buildEvaluationUserPrompt`, `judgeTimeoutForProvider`, `shouldCacheJudgeUnavailable`, `reasoningEffort`, `providerSupportsReasoningEffort`, `judgeModelKey`, `extractJSONObject`, `evaluateSystemAgent` selector. | ~700 |
| `eval/simulate.go` | `runSimulationBatch`, `runOneSimulation`, `simulateAria`, `simulateNovaText`, `simulateDelta`, `personaSimulator`, persona prompt builders, `clientForSimulation`, `createSimulatedWorkflow`, `createSimConversation`, `insertMessage`, `finishConversation`, transcript helpers, `simulationReadyForSystemScoring`. | ~700 |
| `eval/prompt_gen.go` | `PromptGenerationEvidenceWithHistory`, `PersonaGuidanceFromScores`, `generateCandidatePrompt`, `generateInternalText`, `internalPromptOptimizerSystemPrompt`, `generateEvaluatorRevision`, `benchmarkEvaluatorRevision*`, AI call helpers (`generateFromSinglePromptWithTimeout`, `chatCompletionManagedWithTimeout`, retry/cooldown helpers, `isRetryableAICallErr`, `isTimeoutErr`, `isRateLimitErr`, `isNvidiaNIMProvider`, `noteProviderRateLimit`, `waitForProviderLimit`). | ~600 |
| `eval/meta.go` | `runMetaEvaluation`, `RunMetaEvaluation`, `RunCanarySet*`, `runCanarySetForAgent`, `recentSystemTranscriptsForAgent`, judge-disagreement helpers, `lowestMetricStddev`, `appendPtrMetric`, `judgeInvalidJSONStats`, `postAdoptionRegression`. | ~400 |
| `eval/cycle.go` (existing) | keep as-is, just imports adjusted. |
| `eval/loop.go` | NEW — `Supervisor`, `SupervisorConfig`, `SupervisorStatus`, `proposePromptForAgent`, `lowScoreEvidence`, `RunProductionLearningTick` (move existing). | ~400 |
| `eval/store.go` | DB helpers: `loadCostBreakdown`, `currentTotalCostUSD`, `loadAllPromptVersions`, `loadAllEvaluatorVersions`, `loadAllScores`, `scoresForAgent`, `nextPromptVersion`, `nextEvaluatorVersion`, `aggregateScoreRows`, `activeEvaluatorVersion`, `filterActiveEvaluators`, `saveCandidatePrompt`. | ~250 |
| `eval/stats.go` | `Mean`, `Stddev`, `WelchTTest`, `CohensD`, `ComputePercentile`, `aggregateSimulationMeans`, `aggregateComplianceRate`, `targetedIssueGate`, `issueCategoryRates`, `issueCategoriesForJudge`, `addLowMetricIssues`. | ~250 |
| `eval/util.go` | tiny pointer helpers (`floatPtr`, `intPtr`, `stringPtr`, `boolPtr`, `derefBool`, ...), `MarshalJSON`, `truncateForPrompt`, `previewText`, `stripSpeakerLabel`, `personaResponseComplete`, `istLocation`, `simulationSeed`, `defaultPersonas`. | ~150 |

**Mechanical instructions**
- Cut & paste only. No logic changes in this step except the ones called out
  above (F1, F2, F3, F7).
- After each split: `go build ./...` must succeed. Do not commit a half-split
  state.

### F9 · Cost-of-living cleanup (delete unused code)
After F8 split, delete:
- `groupByJudge` if no caller references it (verify via `grep -R groupByJudge`).
- `derefAgent` if unused.
- `metaEvaluationOptions` private struct if every caller passes the zero value.
- The fallback `RunCanarySet` (no-arg) if only `RunCanarySetForAgent` is wired
  to the admin route.
- `RunImprovementCycle` (one-shot) if nothing outside tests calls it after the
  supervisor takes over.

Run `go vet ./...` and `staticcheck ./...` (if available) at the end; remove
any unused-imports / dead-code reports.

### F10 · Tests
**File** `services/server/internal/eval/supervisor_test.go` (new) — test that:
- `StartSupervisor` is idempotent (second call returns "already running").
- `StopSupervisor` cancels the goroutine and returns within 2 s.
- The supervisor respects `MaxIncrementalCostUSD` (mock cost log to exceed budget,
  expect early stop with reason="budget").

**File** `services/server/internal/eval/simulate_test.go` (new):
- Stub `personaSimulator` (interface or test seam) returning fixed strings.
- Run `runOneSimulation` end-to-end and assert that for any persona Nova always
  produces ≥1 turn.

**File** `services/server/internal/eval/prompt_gen_test.go` (new):
- Mock the karma client behind a small interface so the test can return
  `chars=0, output_tokens=1500`.
- Assert `generateInternalText` returns a non-error response (synthetic
  perturbation) instead of `empty AI response`.

### F11 · Cost telemetry
**File** `services/server/internal/eval/store.go` (newly created)
- Make `currentTotalCostUSD` cache the result for 5 s. Continuous loop calls it
  on every cycle; we don't need real-time precision.
- New helper `IncrementalSpentUSD(baseline float64) float64` used by the
  supervisor.

### F12 · Documentation
Update `README.md`:
- Document the new endpoints (start/stop/status, single sim).
- Document the env knobs:
  - `PROMPT_GENERATOR_PROVIDER`
  - `PROMPT_GENERATOR_MODEL`
  - `PROMPT_GENERATOR_MAX_TOKENS` (NEW)
  - `LEARNING_LOOP_BUDGET_USD` (NEW; passed to the supervisor)
- Add a "smoke test" command:
  ```bash
  curl -X POST $URL/admin/simulations/single \
    -H "Authorization: Bearer $TOKEN" \
    -d '{"persona":"cooperative"}'
  ```

---

## 3. Implementation order

Do the steps in this order. Each step is independently buildable.

| Step | Files | Verifies |
|---|---|---|
| **A. Fix prompt-gen empties** (F1) | `constants/eval_config.go`, `eval/eval.go` (only `generateInternalText` and the prompt generator's option list) | `go run ./cmd/aiprobe groq` shows non-empty output. Manual `POST /admin/eval/full-cycle` no longer fails on the *prompt generation* step. |
| **B. Force Nova in simulation** (F2) | `collections/aria.go`, `collections/tools.go`, `eval/eval.go` (3 functions: `simulateAria`, `runOneSimulation`, `simulationReadyForSystemScoring`) | Re-run full cycle with all 5 personas. Logs show `nova first turn done` for every persona. |
| **C. Persona resilience** (F3) | `eval/eval.go` (`personaSimulator.Next`) | Re-run full cycle. No `simulation failed ... empty AI response`. |
| **D. Tighter timeouts/retries** (F7) | `eval/eval.go` constants + `agents/client.go` | Run full cycle. Total wall time per cycle < 4 minutes. |
| **E. File split** (F8) | move-only refactor across the new `eval/*.go` files | `go build ./...` and `go test ./...` pass identically before and after. |
| **F. Continuous supervisor** (F4) + admin endpoints (F6) + single-sim endpoint (F5) | `eval/loop.go`, `handlers/admin_sim.go`, `routes/main.go` | `POST /admin/learning/start` → `GET /admin/learning/status` shows `Running=true, Cycles>0` after a minute. |
| **G. Tests** (F10) | new `_test.go` files | `go test ./internal/eval/... -count=1 -timeout 60s`. |
| **H. Cleanup + docs** (F9, F11, F12) | dead-code removal, README | `go vet ./...` clean. |

---

## 4. Per-file change cheat sheet

The following table is a quick reference for the cheaper LLM. Each row is a
single mechanical change.

| File | Change |
|---|---|
| `services/server/constants/eval_config.go` | (a) Drop `ReasoningEffort: "high"` from the `PromptGenerator` literal in `DefaultSelfLearningConfig`. (b) Add field `PromptGeneratorMaxTokens int` to `SelfLearningConfig`; default 2200 (read from `os.Getenv("PROMPT_GENERATOR_MAX_TOKENS")` if set). (c) Pass through `cfg.PromptGenReasoningEffort` env override (optional). |
| `services/server/internal/eval/eval.go` (`generateInternalText`) | (a) Use `slCfg.PromptGeneratorMaxTokens` instead of hard-coded 1500. (b) Only attach reasoning when `slCfg.PromptGeneratorMaxTokens >= 4000` AND `cfg.ReasoningEffort != ""`. (c) Add fallback chain steps 3 + 4 from F1. (d) Detect "all-reasoning, no-text" case and log it. |
| `services/server/internal/eval/eval.go` (constants) | `judgeCallTimeout = 90 * time.Second`, `judgeCallTimeoutSlow = 240 * time.Second`, `internalGenerationTimeout = 60 * time.Second`. |
| `services/server/internal/eval/eval.go` (`noteProviderRateLimit`, `retryDelay`) | Cap cooldown at 60 s, retry delay at 6 s. |
| `services/server/internal/eval/eval.go` (`personaSimulator.Next`) | Lower max tokens to 384, retries to 5, per-attempt timeout `15+5*attempt` s, accept truncated content ending in `.?!`, deterministic stub fallback after total failure. |
| `services/server/internal/eval/eval.go` (`simulationReadyForSystemScoring`) | Drop the `sections[delta]` requirement. |
| `services/server/internal/eval/eval.go` (`simulateAria`) | After `collections.CompleteARIA`, force advance via `collections.ForceAdvanceToNova(wf.Id)` if the workflow is still on the Aria stage. |
| `services/server/internal/eval/eval.go` (`runOneSimulation`) | Only return early after Aria when `wf.CurrentStage` == Nova; if it's still Aria after the force-advance, log + skip but mark sim as `partial=true`. |
| `services/server/internal/agents/client.go` (`Converse`) | Add fallback to `GenerateFromSinglePrompt` after 2 empty `ChatCompletionManaged` returns; flatten history to `Role: msg` lines as the prompt. |
| `services/server/internal/collections/aria.go` | Add `workflowIsSimulated(wf)`, `ForceAdvanceToNova(workflowID string)`. Modify `CompleteARIA` to skip terminal logic when simulated. |
| `services/server/internal/collections/tools.go` | Update `applyExplicitAriaToolOutcome` to keep flags but never null out `PreferredNovaCallAt` for simulated workflows. Update tool description hint. |
| `services/server/internal/eval/loop.go` (NEW) | Implement `Supervisor`, `SupervisorConfig`, `SupervisorStatus`, `StartSupervisor`, `StopSupervisor`, `SupervisorStatus()`, internal `cycle()`. |
| `services/server/internal/handlers/admin_sim.go` (NEW) | Handlers: `AdminLearningStart`, `AdminLearningStop`, `AdminLearningStatus`, `AdminSingleSimulation`. |
| `services/server/internal/routes/main.go` | Register: `admin.Post("/learning/start", ...)`, `admin.Post("/learning/stop", ...)`, `admin.Get("/learning/status", ...)`, `admin.Post("/simulations/single", ...)`. |
| `services/server/cmd/aiprobe/main.go` | KEEP. Used to validate F1. |
| `README.md` | Document new endpoints, env vars, smoke test. |

---

## 5. Out-of-scope / NOT to be touched

- Voice integration (Vapi, voice handoff). Already working per repo state.
- Borrower-facing UI (Next.js `apps/web`) beyond what the admin dashboard
  needs to show start/stop. The dashboard already polls `GET /admin/eval/...`
  and that contract is preserved.
- Database schema. No migration required.
- Karma library internals. We work around its quirks; we do not patch it.

---

## 6. Verification checklist (final)

After all steps, run:
```bash
docker compose restart backend-dev
sleep 5

# Single-flow sanity
curl -X POST http://localhost:9000/admin/simulations/single \
  -H "Content-Type: application/json" \
  -d '{"persona":"distressed","seed":42}' \
  | jq '.agents | keys, .scores | length'
# Expect: ["aria","nova","delta"] and a non-zero score count.

# Continuous loop
curl -X POST http://localhost:9000/admin/learning/start \
  -H "Content-Type: application/json" \
  -d '{"sims_per_persona_per_cycle":3,"max_incremental_cost_usd":15}'
sleep 180
curl http://localhost:9000/admin/learning/status | jq
# Expect: Running=true, Cycles>=1, ScoresSinceGen reasonable.

curl -X POST http://localhost:9000/admin/learning/stop
```

If all three commands behave as described and the docker logs show no
`empty AI response`, no `simulation_error`, and at least one prompt experiment
row inserted with an adoption decision, the plan is satisfied.

# Self-Learning, Judges, and Meta-Evaluation

This document explains how the self-learning subsystem works in this repository: the control/treatment prompt experiments, independent control judges, meta-evaluator, compliance canaries, and continuous supervisor loop.

## Core Idea

The system treats prompt changes like experiments. A candidate prompt is not adopted because a model says it sounds better. It must beat the current active prompt on scored simulations, preserve compliance, and leave an audit trail.

The learning loop has four persistent records:

- `conversation_scores`: every scored transcript with metric values, judge details, compliance, prompt version, seed, and persona.
- `prompt_experiments`: every control-vs-treatment comparison with raw score arrays and adoption/rejection reason.
- `prompt_versions`: every active, rejected, or rolled-back prompt version.
- `llm_cost_log`: every LLM call used for simulation, judging, prompt generation, handoff generation, and evaluator revision.

## Control, Treatment, and Judges

There are two separate meanings of "control" in the evaluation code:

- Control arm: simulations run with the current active prompt version.
- Treatment arm: simulations run with a generated candidate prompt version.

The judges are independent LLM evaluators. They score the same transcript separately and return the same JSON schema. The default judge set is configured in `services/server/constants/eval_config.go`, and can be replaced with `EVALUATOR_JUDGES_JSON`.

The same judge set is used for both control and treatment runs, which keeps prompt experiments comparable and prevents a candidate from winning because it was judged by a different evaluator mix.

For each transcript, the judge layer:

1. Loads the active evaluator prompt for the target agent/system.
2. Calls each configured judge with the transcript and rubric.
3. Parses numeric metrics and compliance output.
4. Drops invalid/unusable judge results.
5. Computes a weighted aggregate across valid judges.
6. Stores the judge details in `conversation_scores.compliance_breakdown["judge_results"]`.
7. Logs token/cost records for each judge call.

The aggregate score includes dimensions for identity verification, information completeness, no repeated questions, tone, offer clarity, objection handling, commitment attempt, context continuity, consequence accuracy, deadline specificity, negotiation drift, and compliance. Compliance is weighted heavily: a compliance failure caps the composite score.

## Simulation Harness

The simulator creates synthetic borrowers across the five personas required by the assignment:

- cooperative
- combative
- evasive
- confused
- distressed

Each persona chats through ARIA, then the simulation advances into NOVA and DELTA handoff generation so judges can evaluate cross-stage continuity. This matters because a prompt can look good in isolation but still damage the borrower experience by dropping context, repeating questions, or making the next stage inconsistent.

The single-flow endpoint is useful for inspection before running a full cycle:

```sh
POST /api/v1/admin/simulations/single
```

Example body:

```json
{
  "persona": "cooperative",
  "seed": 42,
  "max_turns_per_agent": 6
}
```

The response includes the workflow id, ARIA transcript, NOVA transcript, DELTA handoff, scores, cost delta, and full simulation object.

## Prompt Improvement Loop

A normal improvement cycle runs as follows:

1. Ensure default prompts, evaluators, demo data, and canaries exist.
2. Select the active prompt version as the control prompt.
3. Load recent low-scoring simulations for evidence, or run a fresh control simulation batch if no evidence exists.
4. Build prompt-generation evidence from raw scores, judge reasoning, compliance breakdowns, and rejected candidate history.
5. Generate a candidate prompt for the agent. In the full cycle, candidate prompts are generated for ARIA, NOVA, and DELTA together so the treatment is tested as a full system.
6. Run treatment simulations with the candidate prompt override.
7. Score treatment transcripts with the same judge set.
8. Compare control and treatment score arrays.
9. Adopt or reject the candidate.
10. Store the experiment, prompt version, costs, and decision record.

The prompt generator is constrained with "agent truth" from `services/server/constants/agent_truth.go`. That truth describes what each agent can do, cannot do, and must preserve for compliance. Candidate prompts that ask agents to invent authority, violate policy, expose sensitive data, or bypass the handoff design should be penalized by judges and rejected by the adoption gates.

## Adoption Gates

The code uses Welch's t-test and Cohen's d so a prompt must clear both absolute and distribution-aware thresholds.

Current defaults:

| Gate                  | Default                                                  |
| --------------------- | -------------------------------------------------------- |
| `p_value`             | `< 0.35`                                                 |
| `cohens_d`            | `>= 0.15`                                                |
| `mean_delta`          | `>= 1.5`                                                 |
| treatment stddev      | `<= 25`                                                  |
| treatment compliance  | `>= MinComplianceRate`                                   |
| compliance regression | treatment compliance must be at least control compliance |

If the gates pass, the candidate is saved as active and the prior active prompt is deactivated. If any gate fails, the prompt version is saved as rejected with a rejection reason and the current prompt remains active.

Rollback is available through:

```sh
POST /api/v1/admin/prompt-versions/rollback
```

The rollback endpoint deactivates the current prompt for the agent and reactivates the requested previous version with a reason.

## Continuous Supervisor

The always-on supervisor lives in `services/server/internal/eval/supervisor.go`. It is controlled by:

- `POST /api/v1/admin/learning/start`
- `POST /api/v1/admin/learning/stop`
- `GET /api/v1/admin/learning/status`

Default behavior:

- Rotates through ARIA, NOVA, and DELTA.
- Runs three simulations per persona per cycle unless overridden.
- Builds persona guidance from the lowest-scoring recent conversations.
- Counts new scores and judge calls globally.
- Runs meta-evaluation every `meta_eval_every_n_judges` judge calls.
- Attempts prompt generation every `prompt_gen_every_n_scores` scores only when low scores, compliance failures, or high judge disagreement indicate improvement is needed.
- Stops on explicit stop, duration limit, cost budget, or a safety cycle cap.

The supervisor is intentionally process-local. It persists the important artifacts, but the running status itself is in memory.

## Meta-Evaluator

The meta-evaluator is the Darwin Godel Machine layer: it evaluates the evaluator. It asks whether the scoring method is still trustworthy.

`RunMetaEvaluation` loads recent scores from the current evaluator version and looks for these failure modes:

| Flag                       | What it means                                                               |
| -------------------------- | --------------------------------------------------------------------------- |
| `score_inflation`          | Scores are high with low variance, suggesting the evaluator is too lenient. |
| `metric_uselessness`       | A metric has near-zero variance and no longer discriminates quality.        |
| `judge_disagreement`       | Judges diverge beyond the configured threshold or return invalid output.    |
| `post_adoption_regression` | A newer adopted prompt performs worse than the prior version.               |

When a flag is created, the system:

1. Stores the flag and evidence in `meta_flags`.
2. Generates a revised evaluator prompt using the current evaluator prompt, the flag, evidence, proposed action, and agent truth.
3. Deactivates the old system evaluator version.
4. Inserts and activates a new `evaluator_versions` row.
5. Marks the flag resolved with the before/after evaluator version numbers.

The evaluator revision must preserve the score schema. It can make the rubric sharper, add anchors, clarify compliance boundaries, and reduce ambiguous judge behavior, but downstream code still expects the same JSON fields.

## Compliance Canaries

Compliance canaries are synthetic transcripts with known violations. They test whether the evaluator catches defects that must never be accepted:

- Missing AI identity disclosure.
- False legal threats.
- Continued contact after stop-contact.
- Misleading or unauthorized settlement terms.
- Failure to handle hardship or crisis correctly.
- Missing logging/recording disclosure.
- Unprofessional language.
- Privacy leaks such as full account numbers or personal details.

`RunCanarySetForAgent` evaluates each canary with the active evaluator. A canary passes when the checker result matches the expected failure. Failed canaries indicate an evaluator blind spot and provide evidence for meta-evaluation.

## Cost Control

Every LLM call is logged with call type, model, prompt tokens, completion tokens, total tokens, estimated USD cost, and links to conversation/experiment records where available.

Cost control is handled through:

- Small default batch sizes.
- Configurable judge set.
- Per-run budget checks in the full cycle.
- Incremental supervisor budget from `LEARNING_LOOP_BUDGET_USD`.
- Model pricing overrides through `LLM_PRICING_JSON`.

The assignment budget is $20 total. The default supervisor incremental cap is lower (`15`) so there is room for manual inspection and reruns.

## Reproducible Runs

Use a fixed seed and output directory:

```sh
make eval SEED=42 BATCH_SIZE=2 AGENT=all OUTPUT=./eval-artifacts
```

The output contains raw JSON artifacts for the full report, run config, scores, experiments, meta flags, evaluator versions, canary results, cost log, and prompt versions. CSV exports can be regenerated from database state with:

```sh
make report OUTPUT=./eval-artifacts
```

## Known Tradeoffs

- Small batches keep spend low but produce weak statistical power. The gates are demo-friendly and should be tightened for production.
- Simulation quality depends on the persona model and prompt. Bad personas can make the loop optimize against unrealistic borrowers.
- Meta-evaluator revisions are adopted directly after flag resolution. A stricter production design should benchmark evaluator candidates against a labeled validation set before activation.
- The current system evaluates full simulated flows and scored production conversations, but does not perform live traffic A/B testing.
- Handoff budgets are explicit in prompts and logs but need tokenizer-level hard enforcement for production-grade guarantees.

# Final Self-Learning Evidence

Full all-agent proof: `eval-artifacts/self-learning-proof` seed `66`. Prompt generator `groq/gpt-oss-120b`. Cost `$0.0773`.
Meta-evaluator proof: `eval-artifacts/meta-proof-2` seed `68`. Cumulative cost `$0.1013`.

## Agent Prompt Improvements
- `aria`: prompt `v2` -> `v3`, adopted `True`, mean `30.00` -> `30.00`, delta `+0.00`, chars `412` -> `6958`.
- `delta`: prompt `v2` -> `v3`, adopted `True`, mean `28.58` -> `30.00`, delta `+1.42`, chars `277` -> `4491`.
- `nova`: prompt `v2` -> `v3`, adopted `True`, mean `28.25` -> `30.00`, delta `+1.75`, chars `308` -> `5357`.

## Meta Evaluator Improvement
- `aria` evaluator: `v2` -> `v5`, accepted after benchmark improvement.
  Target flag: `judge_disagreement`. Disagreement `43` -> `0`; score `30` -> `30`; canary ran on evaluator `v5` and passed.
  Resolution: Created and activated evaluator version 5 after benchmark improvement.

## Artifacts
- `learning_proof.json`: full machine-readable proof with prompts, scores, costs, judges, canaries, and evaluator versions.
- `conversation_scores.csv`, `judge_scores.csv`, `prompt_experiments.csv`, `llm_cost_log.csv`: reproducible CSV artifacts.
- `final_self_learning_evidence.json`: combined all-agent + meta-evaluator proof.

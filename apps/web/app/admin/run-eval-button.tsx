"use client";

import * as React from "react";
import {
  Activity,
  DatabaseZap,
  Download,
  Loader2,
  MessageSquareText,
  RefreshCcw,
  ShieldAlert,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  loadAdminEvalProgressAction,
  resetAdminEvalDataAction,
  runAdminFullCycleAction,
  runAdminPromptExperimentAction,
  runAdminMetaEvaluationAction,
  exportAdminScoresAction,
  exportAdminExperimentsAction,
  type AdminConversationPreview,
  type AdminEvalProgress,
} from "./actions";

export default function RunEvalButton() {
  const [progress, setProgress] = React.useState<AdminEvalProgress | null>(null);
  const [runId, setRunId] = React.useState<string | null>(null);
  const [selectedConversationId, setSelectedConversationId] = React.useState<string | null>(null);
  const [status, setStatus] = React.useState<string>("Idle");
  const [isStarting, startEvalTransition] = React.useTransition();
  const [isResetting, resetTransition] = React.useTransition();
  const [isStartingPromptExp, startPromptExpTransition] = React.useTransition();
  const [isStartingMetaEval, startMetaEvalTransition] = React.useTransition();
  const didLoadInitialProgress = React.useRef(false);

  const selectedConversation =
    progress?.conversations.find((row) => row.conversation.id === selectedConversationId) ??
    progress?.conversations[0] ??
    null;

  React.useEffect(() => {
    if (didLoadInitialProgress.current) return;
    didLoadInitialProgress.current = true;
    let cancelled = false;
    loadAdminEvalProgressAction(runId).then((next) => {
      if (!cancelled && next) {
        setProgress(next);
        if (!selectedConversationId && next.conversations[0]) {
          setSelectedConversationId(next.conversations[0].conversation.id);
        }
      }
    });
    return () => {
      cancelled = true;
    };
  }, [runId, selectedConversationId]);

  React.useEffect(() => {
    const isActivelyRunning = progress?.run?.status === "running" || isStartingPromptExp || isStartingMetaEval;
    if (!runId && !isActivelyRunning) return;
    const interval = window.setInterval(async () => {
      const next = await loadAdminEvalProgressAction(runId);
      if (!next) return;
      setProgress(next);
      if (!selectedConversationId && next.conversations[0]) {
        setSelectedConversationId(next.conversations[0].conversation.id);
      }
      if (next.run?.status === "completed") {
        setStatus("Evaluation completed. Refreshing dashboard metrics...");
        window.clearInterval(interval);
        window.setTimeout(() => window.location.reload(), 1200);
      } else if (next.run?.status === "failed") {
        setStatus(`Evaluation failed: ${next.run.error ?? "unknown error"}`);
        window.clearInterval(interval);
      }
    }, 2500);
    return () => window.clearInterval(interval);
  }, [runId, progress?.run?.status, selectedConversationId, isStartingPromptExp, isStartingMetaEval]);

  const running = progress?.run?.status === "running" || isStarting || isStartingPromptExp || isStartingMetaEval;

  return (
    <section className="rounded-[2rem] border border-pink-300/15 bg-zinc-950/75 p-5 shadow-2xl shadow-pink-950/15 backdrop-blur-xl">
      <div className="flex flex-col gap-5 lg:flex-row lg:items-start lg:justify-between">
        <div>
          <p className="text-sm font-semibold uppercase tracking-[0.2em] text-pink-300">
            Evaluation control room
          </p>
          <h2 className="mt-2 text-2xl font-semibold text-pink-50">
            Regenerate, observe, and inspect self-learning runs
          </h2>
          <p className="mt-2 max-w-3xl text-sm leading-6 text-zinc-400">
            Evals run asynchronously. This panel polls persisted simulations, judge scores,
            cost logs, and message rows so you can watch each persona conversation as it is created.
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button
            type="button"
            disabled={running || isResetting}
            onClick={() => {
              if (!window.confirm("This clears app data and reseeds initial prompts, evaluator prompts, canaries, and seed users. Continue?")) {
                return;
              }
              setStatus("Resetting database data and reseeding initials...");
              resetTransition(async () => {
                const result = await resetAdminEvalDataAction();
                setStatus(result?.ok ? "Database reset and initials reseeded." : "Reset failed.");
                const next = await loadAdminEvalProgressAction(null);
                setProgress(next);
                setRunId(null);
                setSelectedConversationId(null);
              });
            }}
            variant="outline"
            className="rounded-full border-rose-300/30 bg-rose-500/10 text-rose-100 hover:bg-rose-400/15"
          >
            <DatabaseZap className="size-4" />
            {isResetting ? "Resetting..." : "Drop data & reseed"}
          </Button>
          <Button
            type="button"
            disabled={running || isResetting}
            onClick={() => {
              setStatus("Starting fresh asynchronous self-learning cycle...");
              startEvalTransition(async () => {
                const result = await runAdminFullCycleAction();
                if (!result) {
                  setStatus("Evaluation start failed.");
                  return;
                }
                setRunId(result.run_id);
                const next = await loadAdminEvalProgressAction(result.run_id);
                setProgress(next ?? {
                  run: result.run,
                  counts: { conversations: 0, messages: 0, scores: 0, prompt_experiments: 0, cost_logs: 0 },
                  total_cost_usd: 0,
                  recent_scores: [],
                  experiments: [],
                  conversations: [],
                  last_generated_at: new Date().toISOString(),
                });
                setStatus(result.existing ? "Existing evaluation run is still active." : "Evaluation run started.");
              });
            }}
            className="rounded-full bg-pink-500 hover:bg-pink-400"
          >
            {isStarting ? <Loader2 className="size-4 animate-spin" /> : <RefreshCcw className="size-4" />}
            {isStarting ? "Running eval..." : "Regenerate evals"}
          </Button>
          <Button
            type="button"
            disabled={running || isResetting}
            onClick={() => {
              setStatus("Running prompt experiments...");
              startPromptExpTransition(async () => {
                const result = await runAdminPromptExperimentAction();
                if (!result) {
                  setStatus("Prompt experiment failed.");
                  return;
                }
                setRunId(result.run_id);
                const next = await loadAdminEvalProgressAction(result.run_id);
                setProgress(next ?? {
                  run: result.run,
                  counts: { conversations: 0, messages: 0, scores: 0, prompt_experiments: 0, cost_logs: 0 },
                  total_cost_usd: 0,
                  recent_scores: [],
                  experiments: [],
                  conversations: [],
                  last_generated_at: new Date().toISOString(),
                });
                setStatus(result.existing ? "Existing evaluation run is still active." : "Prompt experiment run started.");
              });
            }}
            className="rounded-full bg-pink-600 hover:bg-pink-500"
          >
            {isStartingPromptExp ? <Loader2 className="size-4 animate-spin" /> : <Activity className="size-4" />}
            {isStartingPromptExp ? "Running..." : "Run prompt experiments"}
          </Button>
          <Button
            type="button"
            disabled={running || isResetting}
            onClick={() => {
              setStatus("Running meta evaluator...");
              startMetaEvalTransition(async () => {
                const result = await runAdminMetaEvaluationAction();
                if (!result) {
                  setStatus("Meta evaluator failed.");
                  return;
                }
                setRunId(result.run_id);
                const next = await loadAdminEvalProgressAction(result.run_id);
                setProgress(next ?? {
                  run: result.run,
                  counts: { conversations: 0, messages: 0, scores: 0, prompt_experiments: 0, cost_logs: 0 },
                  total_cost_usd: 0,
                  recent_scores: [],
                  experiments: [],
                  conversations: [],
                  last_generated_at: new Date().toISOString(),
                });
                setStatus(result.existing ? "Existing evaluation run is still active." : "Meta evaluator run started.");
              });
            }}
            className="rounded-full bg-purple-600 hover:bg-purple-500"
          >
            {isStartingMetaEval ? <Loader2 className="size-4 animate-spin" /> : <ShieldAlert className="size-4" />}
            {isStartingMetaEval ? "Running..." : "Run meta evaluator"}
          </Button>
          <div className="flex gap-2 border-l border-white/10 pl-2 ml-2">
            <Button
              type="button"
              onClick={async () => {
                setStatus("Exporting conversation scores...");
                const csv = await exportAdminScoresAction();
                if (!csv) {
                  setStatus("Export failed.");
                  return;
                }
                const blob = new Blob([csv], { type: "text/csv" });
                const url = window.URL.createObjectURL(blob);
                const a = document.createElement("a");
                a.href = url;
                a.download = "conversation_scores.csv";
                document.body.appendChild(a);
                a.click();
                a.remove();
                window.URL.revokeObjectURL(url);
                setStatus("Export complete.");
              }}
              variant="outline"
              className="rounded-full border-white/10 bg-white/5 text-zinc-300 hover:bg-white/10"
            >
              <Download className="size-4" />
              Scores CSV
            </Button>
            <Button
              type="button"
              onClick={async () => {
                setStatus("Exporting prompt experiments...");
                const csv = await exportAdminExperimentsAction();
                if (!csv) {
                  setStatus("Export failed.");
                  return;
                }
                const blob = new Blob([csv], { type: "text/csv" });
                const url = window.URL.createObjectURL(blob);
                const a = document.createElement("a");
                a.href = url;
                a.download = "prompt_experiments.csv";
                document.body.appendChild(a);
                a.click();
                a.remove();
                window.URL.revokeObjectURL(url);
                setStatus("Export complete.");
              }}
              variant="outline"
              className="rounded-full border-white/10 bg-white/5 text-zinc-300 hover:bg-white/10"
            >
              <Download className="size-4" />
              Experiments CSV
            </Button>
          </div>
        </div>
      </div>

      <div className="mt-5 grid gap-3 md:grid-cols-5">
        <LiveMetric label="Run" value={progress?.run?.status ?? "idle"} active={running} />
        <LiveMetric label="Conversations" value={String(progress?.counts.conversations ?? 0)} />
        <LiveMetric label="Messages" value={String(progress?.counts.messages ?? 0)} />
        <LiveMetric label="Scores" value={String(progress?.counts.scores ?? 0)} />
        <LiveMetric label="Spend" value={`$${(progress?.total_cost_usd ?? 0).toFixed(4)}`} />
      </div>

      <div className="mt-4 rounded-2xl border border-white/10 bg-black/20 p-4">
        <div className="flex flex-wrap items-center gap-2 text-sm text-zinc-400">
          <Activity className="size-4 text-pink-300" />
          <span>{status}</span>
          {progress?.run ? (
            <span className="ml-auto text-xs text-zinc-600">
              Run {progress.run.id} · started {new Date(progress.run.started_at).toLocaleTimeString()}
            </span>
          ) : null}
        </div>
        {progress?.run?.error ? (
          <p className="mt-2 flex items-center gap-2 text-sm text-rose-300">
            <ShieldAlert className="size-4" />
            {progress.run.error}
          </p>
        ) : null}
      </div>

      <div className="mt-5 grid gap-4 xl:grid-cols-[0.7fr_1.3fr]">
        <ConversationList
          conversations={progress?.conversations ?? []}
          selectedId={selectedConversation?.conversation.id ?? null}
          onSelect={setSelectedConversationId}
        />
        <ConversationMessages conversation={selectedConversation} />
      </div>
    </section>
  );
}

function LiveMetric({ label, value, active }: { label: string; value: string; active?: boolean }) {
  return (
    <div className="rounded-2xl border border-white/10 bg-white/[0.03] p-3">
      <p className="text-[10px] uppercase tracking-[0.16em] text-zinc-600">{label}</p>
      <p className="mt-1 flex items-center gap-2 font-semibold text-pink-50">
        {active ? <span className="size-2 animate-pulse rounded-full bg-emerald-300" /> : null}
        {value}
      </p>
    </div>
  );
}

function ConversationList({
  conversations,
  selectedId,
  onSelect,
}: {
  conversations: AdminConversationPreview[];
  selectedId: string | null;
  onSelect: (id: string) => void;
}) {
  return (
    <div className="rounded-2xl border border-white/10 bg-white/[0.03] p-4">
      <div className="mb-3 flex items-center justify-between">
        <p className="font-semibold text-pink-50">Live conversations</p>
        <MessageSquareText className="size-4 text-pink-300" />
      </div>
      <div className="max-h-[31rem] space-y-2 overflow-y-auto pr-1">
        {conversations.length === 0 ? (
          <p className="rounded-xl border border-dashed border-white/10 p-4 text-sm text-zinc-500">
            No conversations have been persisted yet.
          </p>
        ) : (
          conversations.map((row) => (
            <button
              key={row.conversation.id}
              type="button"
              onClick={() => onSelect(row.conversation.id)}
              className={`w-full rounded-xl border p-3 text-left transition ${
                row.conversation.id === selectedId
                  ? "border-pink-300/40 bg-pink-400/10"
                  : "border-white/10 bg-black/20 hover:border-pink-300/25"
              }`}
            >
              <div className="flex items-center justify-between gap-2">
                <p className="text-sm font-semibold uppercase tracking-[0.14em] text-pink-100">
                  {row.conversation.agent_id}
                </p>
                <span className="text-xs text-zinc-500">
                  {(row.messages ?? []).length} msg
                </span>
              </div>
              <p className="mt-1 truncate text-xs text-zinc-500">
                {row.conversation.persona_type ?? "persona"} · v{row.conversation.prompt_version} · {row.conversation.workflow_id}
              </p>
              {row.score ? (
                <p className="mt-2 text-xs text-zinc-400">
                  Score {row.score.composite_score.toFixed(1)} · compliance {row.score.compliance_passed ? "pass" : "fail"}
                </p>
              ) : (
                <p className="mt-2 text-xs text-zinc-600">Awaiting judge score</p>
              )}
            </button>
          ))
        )}
      </div>
    </div>
  );
}

function ConversationMessages({ conversation }: { conversation: AdminConversationPreview | null }) {
  if (!conversation) {
    return (
      <div className="rounded-2xl border border-dashed border-white/10 bg-white/[0.02] p-5 text-sm text-zinc-500">
        Select a persisted conversation to inspect the exchanged messages.
      </div>
    );
  }
  return (
    <div className="rounded-2xl border border-white/10 bg-black/20 p-4">
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="font-semibold text-pink-50">
            {conversation.conversation.agent_id.toUpperCase()} transcript
          </p>
          <p className="mt-1 text-xs text-zinc-500">
            {conversation.conversation.id} · {conversation.conversation.persona_type ?? "persona"} · seed {conversation.conversation.seed ?? "-"}
          </p>
        </div>
        <span className="rounded-full border border-white/10 bg-white/[0.04] px-3 py-1 text-xs text-zinc-400">
          {conversation.score ? `score ${conversation.score.composite_score.toFixed(1)}` : "unscored"}
        </span>
      </div>
      <div className="max-h-[31rem] space-y-3 overflow-y-auto pr-1">
        {(conversation.messages ?? []).length === 0 ? (
          <p className="rounded-xl border border-dashed border-white/10 p-4 text-sm text-zinc-500">
            This conversation row exists, but no messages have been inserted yet.
          </p>
        ) : (
          (conversation.messages ?? []).map((message) => (
            <div
              key={message.id}
              className={`rounded-2xl border p-3 ${
                message.role === "borrower"
                  ? "ml-6 border-cyan-300/20 bg-cyan-400/10"
                  : "mr-6 border-pink-300/20 bg-pink-400/10"
              }`}
            >
              <div className="mb-1 flex items-center justify-between gap-3 text-xs">
                <span className="font-semibold uppercase tracking-[0.14em] text-zinc-300">
                  {message.role === "borrower" ? "Persona borrower" : "Riverline agent"}
                </span>
                <span className="text-zinc-600">{new Date(message.created_at).toLocaleTimeString()}</span>
              </div>
              <p className="whitespace-pre-wrap text-sm leading-6 text-zinc-100">{message.content}</p>
            </div>
          ))
        )}
      </div>
    </div>
  );
}

import Link from "next/link";
import type React from "react";
import {
  Activity,
  ArrowUpRight,
  BarChart3,
  CheckCircle2,
  CircleAlert,
  DollarSign,
  FlaskConical,
  ShieldCheck,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  loadAdminEvalAction,
  loadAdminMetaAction,
  loadAdminMetricsAction,
  type AdminEvalSummary,
  type AgentId,
  type MetricAggregate,
  type PromptExperiment,
} from "./actions";

const agents: AgentId[] = ["aria", "nova", "delta"];

export default async function AdminPage() {
  const [summary, metrics, meta] = await Promise.all([
    loadAdminEvalAction(),
    loadAdminMetricsAction(),
    loadAdminMetaAction(),
  ]);

  if (!summary || !metrics || !meta) {
    return (
      <main className="relative min-h-screen overflow-hidden bg-background px-6 py-10 text-foreground">
        <Background />
        <div className="relative mx-auto max-w-3xl rounded-[2rem] border border-pink-300/15 bg-zinc-950/75 p-8 shadow-2xl shadow-pink-950/20 backdrop-blur-xl">
          <p className="text-sm font-semibold uppercase tracking-[0.2em] text-pink-300">
            Admin
          </p>
          <h1 className="mt-3 text-3xl font-semibold text-pink-50">
            Admin access required
          </h1>
          <p className="mt-3 text-sm leading-6 text-zinc-400">
            Your Clerk user must have `users.is_admin = true` before this page
            can read evaluation data.
          </p>
          <Button className="mt-6 rounded-full bg-pink-500 hover:bg-pink-400" asChild>
            <Link href="/">Return home</Link>
          </Button>
        </div>
      </main>
    );
  }

  const latestExperiments = [...summary.prompt_experiments].sort(sortNewest).slice(0, 8);
  const adopted = summary.prompt_experiments.filter((row) => row.adopted).length;
  const rejected = summary.prompt_experiments.length - adopted;
  const canariesPassed = summary.canary_results.filter((row) => row.correctly_flagged).length;
  const promptVersions = [...summary.prompt_versions].sort((a, b) =>
    a.agent_id.localeCompare(b.agent_id) || b.version_number - a.version_number,
  );
  const activePrompts = promptVersions.filter((row) => row.is_active);
  const totalTokens = summary.cost_log.reduce((sum, row) => sum + row.total_tokens, 0);

  return (
    <main className="relative min-h-screen overflow-hidden bg-background text-foreground">
      <Background />
      <div className="relative mx-auto flex max-w-7xl flex-col gap-8 px-4 py-6 md:px-8">
        <header className="flex flex-col gap-4 rounded-[2rem] border border-pink-300/15 bg-black/25 p-5 shadow-2xl shadow-pink-950/20 backdrop-blur-xl md:flex-row md:items-center md:justify-between">
          <div>
            <p className="text-sm font-semibold uppercase tracking-[0.2em] text-pink-300">
              Riverline admin
            </p>
            <h1 className="mt-2 text-3xl font-semibold tracking-[-0.04em] text-pink-50 md:text-5xl">
              Prompt evaluation dashboard
            </h1>
            <p className="mt-3 max-w-2xl text-sm leading-6 text-zinc-400">
              Quantitative view of persisted simulations, judge scores, prompt
              adoption decisions, evaluator revisions, canaries, and LLM cost.
            </p>
          </div>
          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              className="rounded-full border-pink-300/25 bg-white/[0.03] text-pink-100 hover:bg-pink-400/10"
              asChild
            >
              <Link href="/chat">Borrower chat</Link>
            </Button>
            <Button className="rounded-full bg-pink-500 hover:bg-pink-400" asChild>
              <Link href="/">Home</Link>
            </Button>
          </div>
        </header>

        <section className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
          <StatCard
            icon={<BarChart3 className="size-4" />}
            label="Scored conversations"
            value={fmtInt(metrics.total_scores)}
            detail={`${summary.conversation_scores.length} raw score rows`}
          />
          <StatCard
            icon={<FlaskConical className="size-4" />}
            label="Prompt experiments"
            value={fmtInt(summary.prompt_experiments.length)}
            detail={`${adopted} adopted / ${rejected} rejected`}
          />
          <StatCard
            icon={<ShieldCheck className="size-4" />}
            label="Canary checks"
            value={`${canariesPassed}/${summary.canary_results.length}`}
            detail="Correctly flagged known compliance cases"
          />
          <StatCard
            icon={<DollarSign className="size-4" />}
            label="LLM spend"
            value={fmtMoney(summary.total_cost_usd)}
            detail={`${fmtInt(totalTokens)} total tokens`}
          />
        </section>

        <section className="grid gap-4 lg:grid-cols-3">
          {agents.map((agent) => (
            <AgentScoreCard
              key={agent}
              agent={agent}
              aggregate={metrics.by_agent?.[agent]}
              promptRows={promptVersions.filter((row) => row.agent_id === agent)}
              experiments={summary.prompt_experiments.filter((row) => row.agent_id === agent)}
            />
          ))}
        </section>

        <section className="grid gap-4 xl:grid-cols-[1.25fr_0.75fr]">
          <Panel title="Prompt Adoption Timeline" subtitle="Control vs treatment score, compliance, and adoption reason.">
            <div className="space-y-3">
              {latestExperiments.length === 0 ? (
                <EmptyState text="No prompt experiments found in the database." />
              ) : (
                latestExperiments.map((experiment) => (
                  <ExperimentRow key={experiment.id} experiment={experiment} />
                ))
              )}
            </div>
          </Panel>

          <Panel title="Active Prompt Versions" subtitle="Current prompt rows serving production agents.">
            <div className="space-y-3">
              {activePrompts.map((prompt) => (
                <div
                  key={prompt.id}
                  className="rounded-2xl border border-white/10 bg-white/[0.03] p-4"
                >
                  <div className="flex items-center justify-between gap-3">
                    <p className="font-semibold uppercase tracking-[0.16em] text-pink-100">
                      {prompt.agent_id} v{prompt.version_number}
                    </p>
                    <span className="rounded-full border border-emerald-300/20 bg-emerald-400/10 px-2 py-1 text-xs text-emerald-200">
                      active
                    </span>
                  </div>
                  <p className="mt-2 line-clamp-3 text-sm leading-6 text-zinc-400">
                    {prompt.adoption_reason ?? prompt.prompt_text}
                  </p>
                </div>
              ))}
            </div>
          </Panel>
        </section>

        <section className="grid gap-4 xl:grid-cols-2">
          <Panel title="Meta Evaluator Health" subtitle="Flags, evaluator revisions, and why judge prompts changed.">
            <div className="space-y-3">
              {summary.meta_flags.length === 0 ? (
                <EmptyState text="No meta-evaluator flags found." />
              ) : (
                [...summary.meta_flags].sort(sortNewest).slice(0, 8).map((flag) => (
                  <div
                    key={flag.id}
                    className="rounded-2xl border border-white/10 bg-white/[0.03] p-4"
                  >
                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <p className="font-semibold text-pink-50">
                        {flag.agent_id?.toUpperCase() ?? "GLOBAL"} · {flag.flag_type}
                      </p>
                      <span
                        className={`rounded-full border px-2 py-1 text-xs ${
                          flag.resolved
                            ? "border-emerald-300/20 bg-emerald-400/10 text-emerald-200"
                            : "border-amber-300/20 bg-amber-400/10 text-amber-200"
                        }`}
                      >
                        {flag.resolved ? "resolved" : "open"}
                      </span>
                    </div>
                    <p className="mt-2 text-sm leading-6 text-zinc-400">
                      {flag.proposed_action ?? "No proposed action recorded."}
                    </p>
                    <p className="mt-2 text-xs text-zinc-500">
                      Evaluator {flag.evaluator_version_before ?? "-"} {"->"}{" "}
                      {flag.evaluator_version_after ?? "not adopted"}
                    </p>
                    {flag.resolution ? (
                      <p className="mt-2 text-xs leading-5 text-zinc-500">
                        {flag.resolution}
                      </p>
                    ) : null}
                  </div>
                ))
              )}
            </div>
          </Panel>

          <Panel title="Model Cost & Throughput" subtitle="Grouped by call type from persisted LLM cost logs.">
            <CostBreakdown summary={summary} />
          </Panel>
        </section>
      </div>
    </main>
  );
}

function Background() {
  return (
    <div className="pointer-events-none absolute inset-0 bg-[radial-gradient(circle_at_18%_14%,rgba(236,72,153,0.24),transparent_30%),radial-gradient(circle_at_86%_8%,rgba(217,70,239,0.18),transparent_28%),radial-gradient(circle_at_50%_100%,rgba(244,63,94,0.12),transparent_34%)]" />
  );
}

function StatCard({
  icon,
  label,
  value,
  detail,
}: {
  icon: React.ReactNode;
  label: string;
  value: string;
  detail: string;
}) {
  return (
    <div className="rounded-[1.75rem] border border-pink-300/15 bg-zinc-950/70 p-5 shadow-2xl shadow-pink-950/15 backdrop-blur-xl">
      <div className="flex items-center justify-between text-pink-300">
        {icon}
        <Activity className="size-4 opacity-50" />
      </div>
      <p className="mt-4 text-sm text-zinc-500">{label}</p>
      <p className="mt-1 text-3xl font-semibold text-pink-50">{value}</p>
      <p className="mt-2 text-xs text-zinc-500">{detail}</p>
    </div>
  );
}

function AgentScoreCard({
  agent,
  aggregate,
  promptRows,
  experiments,
}: {
  agent: AgentId;
  aggregate?: MetricAggregate;
  promptRows: { version_number: number; is_active: boolean }[];
  experiments: PromptExperiment[];
}) {
  const latest = [...experiments].sort(sortNewest)[0];
  const active = promptRows.find((row) => row.is_active);
  const score = aggregate?.mean ?? 0;
  return (
    <div className="rounded-[1.75rem] border border-pink-300/15 bg-zinc-950/70 p-5 shadow-2xl shadow-pink-950/15 backdrop-blur-xl">
      <div className="flex items-center justify-between">
        <p className="text-sm font-semibold uppercase tracking-[0.2em] text-pink-300">
          {agent}
        </p>
        <span className="rounded-full border border-white/10 bg-white/[0.04] px-2 py-1 text-xs text-zinc-400">
          active v{active?.version_number ?? "-"}
        </span>
      </div>
      <div className="mt-5">
        <div className="flex items-end justify-between">
          <p className="text-4xl font-semibold text-pink-50">{fmtScore(score)}</p>
          <p className="text-sm text-zinc-500">{fmtPct(aggregate?.compliance_rate ?? 0)} compliant</p>
        </div>
        <div className="mt-3 h-2 overflow-hidden rounded-full bg-white/10">
          <div
            className="h-full rounded-full bg-pink-500"
            style={{ width: `${Math.max(2, Math.min(100, score))}%` }}
          />
        </div>
      </div>
      <div className="mt-5 grid grid-cols-3 gap-2 text-xs">
        <MiniMetric label="N" value={fmtInt(aggregate?.n ?? 0)} />
        <MiniMetric label="Median" value={fmtScore(aggregate?.median ?? 0)} />
        <MiniMetric label="Delta" value={latest ? signed(latest.mean_delta) : "-"} />
      </div>
    </div>
  );
}

function ExperimentRow({ experiment }: { experiment: PromptExperiment }) {
  return (
    <div className="rounded-2xl border border-white/10 bg-white/[0.03] p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="font-semibold text-pink-50">
            {experiment.agent_id.toUpperCase()} v{experiment.control_version} {"->"} v
            {experiment.candidate_version}
          </p>
          <p className="mt-1 text-xs text-zinc-500">
            {new Date(experiment.created_at).toLocaleString()}
          </p>
        </div>
        <span
          className={`inline-flex items-center gap-1 rounded-full border px-2 py-1 text-xs ${
            experiment.adopted
              ? "border-emerald-300/20 bg-emerald-400/10 text-emerald-200"
              : "border-rose-300/20 bg-rose-400/10 text-rose-200"
          }`}
        >
          {experiment.adopted ? (
            <CheckCircle2 className="size-3" />
          ) : (
            <CircleAlert className="size-3" />
          )}
          {experiment.adopted ? "adopted" : "rejected"}
        </span>
      </div>
      <div className="mt-4 grid gap-3 md:grid-cols-4">
        <MiniMetric label="Control" value={fmtScore(experiment.control_mean)} />
        <MiniMetric label="Treatment" value={fmtScore(experiment.treatment_mean)} />
        <MiniMetric label="Delta" value={signed(experiment.mean_delta)} />
        <MiniMetric label="p-value" value={experiment.p_value.toFixed(4)} />
      </div>
      <div className="mt-4 grid gap-2 md:grid-cols-2">
        <Bar label="Control compliance" value={experiment.control_compliance_rate} />
        <Bar label="Treatment compliance" value={experiment.treatment_compliance_rate} />
      </div>
      <p className="mt-3 text-xs leading-5 text-zinc-500">
        {experiment.adopted
          ? `Adopted with effect size ${fmtNullable(experiment.cohens_d)} and score delta ${signed(experiment.mean_delta)}.`
          : (experiment.rejection_reason ?? "Rejected by adoption gate.")}
      </p>
    </div>
  );
}

function CostBreakdown({ summary }: { summary: AdminEvalSummary }) {
  const byType = new Map<string, { cost: number; tokens: number }>();
  for (const row of summary.cost_log) {
    const current = byType.get(row.call_type) ?? { cost: 0, tokens: 0 };
    current.cost += row.cost_usd;
    current.tokens += row.total_tokens;
    byType.set(row.call_type, current);
  }
  const rows = [...byType.entries()].sort((a, b) => b[1].cost - a[1].cost);
  const max = Math.max(...rows.map(([, row]) => row.cost), 0.001);
  return (
    <div className="space-y-3">
      {rows.length === 0 ? (
        <EmptyState text="No LLM cost rows found." />
      ) : (
        rows.map(([type, row]) => (
          <div key={type} className="rounded-2xl border border-white/10 bg-white/[0.03] p-4">
            <div className="flex items-center justify-between gap-3 text-sm">
              <p className="font-medium text-pink-50">{type}</p>
              <p className="text-zinc-400">{fmtMoney(row.cost)}</p>
            </div>
            <div className="mt-3 h-2 overflow-hidden rounded-full bg-white/10">
              <div
                className="h-full rounded-full bg-pink-500"
                style={{ width: `${Math.max(3, (row.cost / max) * 100)}%` }}
              />
            </div>
            <p className="mt-2 text-xs text-zinc-500">{fmtInt(row.tokens)} tokens</p>
          </div>
        ))
      )}
    </div>
  );
}

function Panel({
  title,
  subtitle,
  children,
}: {
  title: string;
  subtitle: string;
  children: React.ReactNode;
}) {
  return (
    <section className="rounded-[2rem] border border-pink-300/15 bg-zinc-950/70 p-5 shadow-2xl shadow-pink-950/15 backdrop-blur-xl">
      <div className="mb-5 flex items-start justify-between gap-4">
        <div>
          <h2 className="text-xl font-semibold text-pink-50">{title}</h2>
          <p className="mt-1 text-sm leading-6 text-zinc-500">{subtitle}</p>
        </div>
        <ArrowUpRight className="size-5 text-pink-300/70" />
      </div>
      {children}
    </section>
  );
}

function MiniMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-2xl border border-white/10 bg-black/20 p-3">
      <p className="text-[10px] uppercase tracking-[0.16em] text-zinc-600">{label}</p>
      <p className="mt-1 font-semibold text-pink-50">{value}</p>
    </div>
  );
}

function Bar({ label, value }: { label: string; value: number }) {
  return (
    <div>
      <div className="mb-1 flex items-center justify-between text-xs text-zinc-500">
        <span>{label}</span>
        <span>{fmtPct(value)}</span>
      </div>
      <div className="h-2 overflow-hidden rounded-full bg-white/10">
        <div className="h-full rounded-full bg-pink-500" style={{ width: `${value * 100}%` }} />
      </div>
    </div>
  );
}

function EmptyState({ text }: { text: string }) {
  return (
    <div className="rounded-2xl border border-dashed border-white/10 bg-white/[0.02] p-5 text-sm text-zinc-500">
      {text}
    </div>
  );
}

function sortNewest<T extends { created_at: string }>(a: T, b: T) {
  return new Date(b.created_at).getTime() - new Date(a.created_at).getTime();
}

function fmtInt(value: number) {
  return new Intl.NumberFormat("en-US").format(value);
}

function fmtScore(value: number) {
  return value.toFixed(1);
}

function fmtMoney(value: number) {
  return `$${value.toFixed(4)}`;
}

function fmtPct(value: number) {
  return `${(value * 100).toFixed(0)}%`;
}

function signed(value: number) {
  return `${value >= 0 ? "+" : ""}${value.toFixed(2)}`;
}

function fmtNullable(value?: number | null) {
  return typeof value === "number" ? value.toFixed(2) : "-";
}

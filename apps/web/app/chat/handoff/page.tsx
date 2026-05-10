import Link from "next/link";
import { ArrowLeft, Download, FileText, ShieldCheck } from "lucide-react";

import { Button } from "@/components/ui/button";
import { loadDeltaHandoffPdfAction } from "../actions";

type HandoffPageProps = {
  searchParams: Promise<{ workflowId?: string }>;
};

export default async function HandoffPage({ searchParams }: HandoffPageProps) {
  const params = await searchParams;
  const workflowId = params.workflowId?.trim() ?? "";
  const pdfHref = workflowId ? await loadDeltaHandoffPdfAction(workflowId) : null;

  return (
    <main className="relative min-h-screen overflow-hidden bg-background px-4 py-6 text-foreground md:px-8">
      <div className="pointer-events-none absolute inset-0 bg-[radial-gradient(circle_at_16%_10%,rgba(236,72,153,0.18),transparent_30%),radial-gradient(circle_at_85%_20%,rgba(217,70,239,0.16),transparent_28%)]" />
      <div className="relative mx-auto flex max-w-5xl flex-col gap-5">
        <header className="flex flex-col gap-4 rounded-[2rem] border border-pink-300/15 bg-black/30 p-5 shadow-2xl shadow-pink-950/20 backdrop-blur-xl md:flex-row md:items-center md:justify-between">
          <div>
            <p className="inline-flex items-center gap-2 text-xs font-semibold uppercase tracking-[0.22em] text-pink-300">
              <FileText className="size-4" />
              Delta handoff PDF
            </p>
            <h1 className="mt-3 text-3xl font-semibold tracking-[-0.04em] text-pink-50 md:text-5xl">
              Final notice record
            </h1>
            <p className="mt-3 max-w-2xl text-sm leading-6 text-zinc-400">
              A borrower-ready PDF built from the persisted Delta workflow state
              and latest resolution offer.
            </p>
          </div>
          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              className="rounded-full border-pink-300/25 bg-white/[0.03] text-pink-100 hover:bg-pink-400/10"
              asChild
            >
              <Link href="/chat">
                <ArrowLeft className="size-4" />
                Back to chat
              </Link>
            </Button>
            {pdfHref ? (
              <Button className="rounded-full bg-pink-500 text-white hover:bg-pink-400" asChild>
                <a href={pdfHref} download={`riverline-delta-handoff-${workflowId}.pdf`}>
                  <Download className="size-4" />
                  Download PDF
                </a>
              </Button>
            ) : null}
          </div>
        </header>

        {pdfHref ? (
          <section className="rounded-[2rem] border border-pink-300/15 bg-zinc-950/75 p-5 shadow-2xl shadow-pink-950/20 backdrop-blur-xl">
            <div className="mb-4 flex flex-wrap items-center gap-3 text-xs text-zinc-400">
              <span className="inline-flex items-center gap-1.5 text-pink-100">
                <ShieldCheck className="size-3.5 text-pink-300" />
                Available for workflow {workflowId}
              </span>
            </div>
            <iframe
              src={pdfHref}
              title="Delta handoff PDF"
              className="h-[68vh] w-full rounded-[1.5rem] border border-white/10 bg-white"
            />
          </section>
        ) : (
          <section className="rounded-[2rem] border border-pink-300/15 bg-zinc-950/75 p-6 shadow-2xl shadow-pink-950/20 backdrop-blur-xl">
            <p className="text-sm font-semibold uppercase tracking-[0.2em] text-pink-300">
              Handoff unavailable
            </p>
            <h2 className="mt-3 text-2xl font-semibold text-pink-50">
              Delta has not produced a downloadable handoff yet.
            </h2>
            <p className="mt-3 max-w-2xl text-sm leading-6 text-zinc-400">
              The export becomes available after the workflow reaches Delta or
              Delta final-offer data has been persisted from the Nova outcome.
            </p>
            <Button className="mt-6 rounded-full bg-pink-500 hover:bg-pink-400" asChild>
              <Link href="/chat">Return to chat</Link>
            </Button>
          </section>
        )}
      </div>
    </main>
  );
}

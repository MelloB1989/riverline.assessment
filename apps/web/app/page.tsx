import Link from "next/link";
import type React from "react";
import { auth } from "@clerk/nextjs/server";
import { BarChart3, Bot, PhoneCall, ShieldCheck } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

export default async function Home() {
  const { userId } = await auth();

  return (
    <div className="relative flex min-h-screen flex-col overflow-hidden bg-background">
      <div className="pointer-events-none absolute inset-0 bg-[radial-gradient(circle_at_18%_14%,rgba(236,72,153,0.24),transparent_30%),radial-gradient(circle_at_86%_8%,rgba(217,70,239,0.18),transparent_28%),radial-gradient(circle_at_50%_100%,rgba(244,63,94,0.12),transparent_34%)]" />
      <header className="relative border-b border-pink-300/10 bg-black/20 backdrop-blur-xl">
        <div className="mx-auto flex w-full max-w-6xl items-center justify-between px-6 py-4">
          <div className="flex items-center gap-3">
            <span className="flex h-11 w-11 items-center justify-center rounded-2xl bg-pink-500 text-lg font-semibold text-white shadow-[0_0_34px_rgba(236,72,153,0.5)]">
              R
            </span>
            <div>
              <p className="text-sm font-semibold text-pink-50">Riverline</p>
              <p className="text-xs text-zinc-500">
                AI borrower resolution workflow
              </p>
            </div>
          </div>
          <div className="flex items-center gap-3">
            {userId ? (
              <>
                <Button
                  variant="ghost"
                  className="hidden text-pink-100 hover:bg-pink-400/10 sm:inline-flex"
                  asChild
                >
                  <Link href="/admin">Admin</Link>
                </Button>
                <Button asChild>
                  <Link href="/chat">Open Chat</Link>
                </Button>
              </>
            ) : (
              <>
                <Button
                  variant="ghost"
                  className="text-pink-100 hover:bg-pink-400/10"
                  asChild
                >
                  <Link href="/sign-in">Log in</Link>
                </Button>
                <Button
                  className="rounded-full bg-pink-500 text-white shadow-[0_0_28px_rgba(236,72,153,0.35)] hover:bg-pink-400"
                  asChild
                >
                  <Link href="/sign-up">Sign up</Link>
                </Button>
              </>
            )}
          </div>
        </div>
      </header>

      <main className="relative flex flex-1 flex-col items-center justify-center px-6 py-16">
        <div className="mx-auto grid w-full max-w-6xl gap-10 lg:grid-cols-[1.1fr_0.9fr]">
          <div className="space-y-6">
            <p className="text-sm font-semibold uppercase tracking-[0.2em] text-pink-300">
              Collections intelligence
            </p>
            <h1 className="max-w-3xl text-4xl font-semibold leading-tight text-pink-50 md:text-6xl">
              One borrower experience across chat, voice, and final notice.
            </h1>
            <p className="max-w-xl text-lg leading-8 text-zinc-400">
              Riverline coordinates ARIA assessment chat, NOVA resolution calls,
              and DELTA final notice handling while preserving context,
              compliance disclosures, prompt versions, and evaluation evidence.
            </p>
            <div className="flex flex-wrap gap-3">
              {userId ? (
                <>
                  <Button
                    size="lg"
                    className="rounded-full bg-pink-500 text-white shadow-[0_0_34px_rgba(236,72,153,0.4)] hover:bg-pink-400"
                    asChild
                  >
                    <Link href="/chat">Continue borrower chat</Link>
                  </Button>
                  <Button
                    size="lg"
                    variant="outline"
                    className="rounded-full border-pink-300/30 bg-white/[0.03] text-pink-100 hover:bg-pink-400/10"
                    asChild
                  >
                    <Link href="/admin">View eval dashboard</Link>
                  </Button>
                </>
              ) : (
                <>
                  <Button
                    size="lg"
                    className="rounded-full bg-pink-500 text-white shadow-[0_0_34px_rgba(236,72,153,0.4)] hover:bg-pink-400"
                    asChild
                  >
                    <Link href="/sign-in">Log in</Link>
                  </Button>
                  <Button
                    size="lg"
                    variant="outline"
                    className="rounded-full border-pink-300/30 bg-white/[0.03] text-pink-100 hover:bg-pink-400/10"
                    asChild
                  >
                    <Link href="/sign-up">Create account</Link>
                  </Button>
                </>
              )}
            </div>
          </div>

          <div className="space-y-6">
            <Card className="border-pink-300/15 bg-zinc-950/70 shadow-2xl shadow-pink-950/20 backdrop-blur-xl">
              <CardHeader>
                <CardTitle className="text-pink-50">
                  Production workflow
                </CardTitle>
                <CardDescription>
                  The borrower sees one Riverline assistant; internally each
                  stage has its own prompt, context, and evaluator.
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-4">
                <Feature
                  icon={<Bot className="size-4" />}
                  title="ARIA chat"
                  body="Verifies identity safely, gathers assessment facts, and schedules NOVA without exposing internal context."
                />
                <Feature
                  icon={<PhoneCall className="size-4" />}
                  title="NOVA voice"
                  body="Receives a bounded offer context, presents payment options, and produces a structured call outcome."
                />
                <Feature
                  icon={<ShieldCheck className="size-4" />}
                  title="DELTA final notice"
                  body="Handles post-call chat with final offer, deadline, and audit-quality written continuity."
                />
              </CardContent>
            </Card>
            <Card className="border-pink-300/15 bg-zinc-950/70 shadow-2xl shadow-pink-950/20 backdrop-blur-xl">
              <CardHeader>
                <CardTitle className="text-pink-50">
                  Self-learning visibility
                </CardTitle>
                <CardDescription>
                  Admins can inspect prompt experiments, judge scores, adoption
                  decisions, meta-evaluator flags, canaries, and cost.
                </CardDescription>
              </CardHeader>
              <CardContent>
                <Button
                  variant="secondary"
                  className="w-full rounded-full border border-pink-300/15 bg-pink-400/10 text-pink-100 hover:bg-pink-400/20"
                  asChild
                >
                  <Link href={userId ? "/admin" : "/sign-in"}>
                    <BarChart3 className="size-4" />
                    Open quantitative eval dashboard
                  </Link>
                </Button>
              </CardContent>
            </Card>
          </div>
        </div>
      </main>
    </div>
  );
}

function Feature({
  icon,
  title,
  body,
}: {
  icon: React.ReactNode;
  title: string;
  body: string;
}) {
  return (
    <div className="rounded-2xl border border-white/10 bg-white/[0.03] p-4">
      <p className="flex items-center gap-2 text-sm font-semibold text-pink-50">
        <span className="text-pink-300">{icon}</span>
        {title}
      </p>
      <p className="mt-2 text-sm leading-6 text-zinc-400">{body}</p>
    </div>
  );
}

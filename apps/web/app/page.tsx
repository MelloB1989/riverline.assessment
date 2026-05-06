import Link from "next/link";
import { auth } from "@clerk/nextjs/server";

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
                Secure, guided chat experiences.
              </p>
            </div>
          </div>
          <div className="flex items-center gap-3">
            {userId ? (
              <Button asChild>
                <Link href="/chat">Open Chat</Link>
              </Button>
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
        <div className="mx-auto grid w-full max-w-6xl gap-10 lg:grid-cols-[1.15fr_0.85fr]">
          <div className="space-y-6">
            <p className="text-sm font-semibold uppercase tracking-[0.2em] text-pink-300">
              Riverline Chat
            </p>
            <h1 className="max-w-3xl text-4xl font-semibold leading-tight text-pink-50 md:text-6xl">
              Dark, focused AI chat with a little heat.
            </h1>
            <p className="max-w-xl text-lg leading-8 text-zinc-400">
              Riverline brings your team into a focused chat experience with
              guided prompts, shared context, source-aware answers, and a
              polished interface built for real work.
            </p>
            <div className="flex flex-wrap gap-3">
              {userId ? (
                <Button
                  size="lg"
                  className="rounded-full bg-pink-500 text-white shadow-[0_0_34px_rgba(236,72,153,0.4)] hover:bg-pink-400"
                  asChild
                >
                  <Link href="/chat">Continue to chat</Link>
                </Button>
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
                <CardTitle className="text-pink-50">Why Riverline</CardTitle>
                <CardDescription>
                  A crisp, modern chat hub powered by Clerk authentication.
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="rounded-2xl border border-pink-300/20 bg-pink-400/10 p-4">
                  <p className="text-sm font-semibold text-pink-100">
                    Seamless login
                  </p>
                  <p className="text-sm text-zinc-400">
                    Sign in or sign up in seconds, then jump straight into the
                    conversation.
                  </p>
                </div>
                <div className="rounded-2xl border border-white/10 bg-white/[0.03] p-4">
                  <p className="text-sm font-semibold text-pink-50">
                    Full chat workspace
                  </p>
                  <p className="text-sm text-zinc-400">
                    Threads, suggestions, reasoning, sources, and a rich prompt
                    composer.
                  </p>
                </div>
                <div className="rounded-2xl border border-white/10 bg-white/[0.03] p-4">
                  <p className="text-sm font-semibold text-pink-50">
                    Privacy-first
                  </p>
                  <p className="text-sm text-zinc-400">
                    Clerk handles your authentication with secure best
                    practices.
                  </p>
                </div>
              </CardContent>
            </Card>
            <Card className="border-pink-300/15 bg-zinc-950/70 shadow-2xl shadow-pink-950/20 backdrop-blur-xl">
              <CardHeader>
                <CardTitle className="text-pink-50">Get started</CardTitle>
                <CardDescription>
                  Choose a path and you’ll be chatting in minutes.
                </CardDescription>
              </CardHeader>
              <CardContent className="flex flex-col gap-3">
                <Button
                  variant="secondary"
                  className="rounded-full border border-pink-300/15 bg-pink-400/10 text-pink-100 hover:bg-pink-400/20"
                  asChild
                >
                  <Link href="/sign-in">I already have an account</Link>
                </Button>
                <Button
                  variant="outline"
                  className="rounded-full border-pink-300/25 bg-white/[0.03] text-pink-100 hover:bg-pink-400/10"
                  asChild
                >
                  <Link href="/sign-up">I need to create one</Link>
                </Button>
              </CardContent>
            </Card>
          </div>
        </div>
      </main>
    </div>
  );
}

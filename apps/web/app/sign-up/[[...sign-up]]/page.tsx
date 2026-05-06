import { SignUp } from "@clerk/nextjs";

export default function SignUpPage() {
  return (
    <div className="relative flex min-h-screen items-center justify-center overflow-hidden bg-background px-6 py-16">
      <div className="absolute inset-0 bg-[radial-gradient(circle_at_50%_0%,rgba(236,72,153,0.28),transparent_38%),radial-gradient(circle_at_100%_100%,rgba(168,85,247,0.14),transparent_34%)]" />
      <div className="relative w-full max-w-md">
        <SignUp
          path="/sign-up"
          routing="path"
          signInUrl="/sign-in"
          fallbackRedirectUrl="/chat"
          signInFallbackRedirectUrl="/chat"
          appearance={{
            elements: {
              card: "rounded-3xl border border-pink-300/20 bg-zinc-950/85 shadow-2xl shadow-pink-950/30 backdrop-blur-xl",
              headerTitle: "text-2xl font-semibold text-pink-50",
              headerSubtitle: "text-zinc-400",
              socialButtonsBlockButton:
                "border-pink-300/20 bg-white/[0.03] text-zinc-100 hover:bg-pink-400/10",
              formFieldLabel: "text-zinc-300",
              formButtonPrimary:
                "rounded-full bg-pink-500 text-white hover:bg-pink-400",
              formFieldInput:
                "rounded-xl border-pink-300/20 bg-zinc-950 text-zinc-50 focus:ring-pink-400",
              footerActionText: "text-zinc-400",
              footerActionLink: "text-pink-300 hover:text-pink-200",
            },
          }}
        />
      </div>
    </div>
  );
}

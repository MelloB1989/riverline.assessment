"use client";

import { Sparkle } from "lucide-react";

import { cn } from "@/lib/utils";

function Suggestion({
  className,
  children,
  ...props
}: React.ComponentProps<"button">) {
  return (
    <button
      type="button"
      className={cn(
        "inline-flex items-center gap-2 rounded-full border border-pink-300/20 bg-pink-400/10 px-3 py-2 text-sm text-pink-100 transition hover:border-pink-200/50 hover:bg-pink-400/20",
        className
      )}
      {...props}
    >
      <Sparkle className="size-3.5 text-pink-300" />
      {children}
    </button>
  );
}

export { Suggestion };

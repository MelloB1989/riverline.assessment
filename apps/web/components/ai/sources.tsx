"use client";

import { ExternalLink } from "lucide-react";

import { cn } from "@/lib/utils";

type Source = {
  title: string;
  url: string;
};

function Sources({
  sources,
  className,
}: {
  sources: Source[];
  className?: string;
}) {
  return (
    <div className={cn("flex flex-wrap gap-2", className)}>
      {sources.map((source) => (
        <a
          key={source.url}
          href={source.url}
          className="inline-flex items-center gap-2 rounded-full border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs text-zinc-300 transition hover:border-pink-300/40 hover:bg-pink-400/10 hover:text-pink-100"
        >
          {source.title}
          <ExternalLink className="size-3" />
        </a>
      ))}
    </div>
  );
}

export { Sources, type Source };

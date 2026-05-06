"use client";

import * as React from "react";
import { ArrowDown } from "lucide-react";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

function Conversation({
  className,
  children,
}: React.ComponentProps<"div">) {
  const viewportRef = React.useRef<HTMLDivElement>(null);
  const [isPinned, setIsPinned] = React.useState(true);

  React.useEffect(() => {
    if (!isPinned) return;
    viewportRef.current?.scrollTo({
      top: viewportRef.current.scrollHeight,
      behavior: "smooth",
    });
  }, [children, isPinned]);

  const handleScroll = () => {
    const viewport = viewportRef.current;
    if (!viewport) return;

    const distanceFromBottom =
      viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight;
    setIsPinned(distanceFromBottom < 96);
  };

  return (
    <div className={cn("relative min-h-0 flex-1", className)}>
      <div
        ref={viewportRef}
        onScroll={handleScroll}
        className="h-full overflow-y-auto scroll-smooth px-4 py-6 [scrollbar-color:rgba(244,114,182,0.45)_transparent] md:px-8"
      >
        <div className="mx-auto flex w-full max-w-4xl flex-col gap-6">
          {children}
        </div>
      </div>
      {!isPinned ? (
        <Button
          size="icon"
          variant="secondary"
          className="absolute bottom-4 left-1/2 size-10 -translate-x-1/2 rounded-full border border-pink-400/30 bg-zinc-950/90 text-pink-100 shadow-[0_0_30px_rgba(236,72,153,0.25)] backdrop-blur"
          onClick={() => {
            viewportRef.current?.scrollTo({
              top: viewportRef.current.scrollHeight,
              behavior: "smooth",
            });
          }}
        >
          <ArrowDown className="size-4" />
          <span className="sr-only">Scroll to bottom</span>
        </Button>
      ) : null}
    </div>
  );
}

function ConversationEmpty({
  className,
  ...props
}: React.ComponentProps<"div">) {
  return (
    <div
      className={cn(
        "mx-auto grid min-h-[42vh] max-w-2xl place-items-center text-center",
        className
      )}
      {...props}
    />
  );
}

export { Conversation, ConversationEmpty };

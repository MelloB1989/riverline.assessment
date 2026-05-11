"use client";

import * as React from "react";
import { type ConversationScore } from "./actions";

const SYSTEM_COLOR = "#f472b6";

function fmtInt(n: number) {
  return new Intl.NumberFormat("en-US").format(n);
}

export default function ScoreTrendChart({ scores }: { scores: ConversationScore[] }) {
  const [selectedPersona, setSelectedPersona] = React.useState<string>("All");

  const personas = React.useMemo(() => {
    const set = new Set<string>();
    for (const score of scores) {
      if (score.persona_type) {
        set.add(score.persona_type);
      }
    }
    return ["All", ...Array.from(set).sort()];
  }, [scores]);

  const filteredScores = React.useMemo(() => {
    if (selectedPersona === "All") return scores;
    return scores.filter((s) => s.persona_type === selectedPersona);
  }, [scores, selectedPersona]);

  const rows = [...filteredScores].sort(
    (a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime()
  );

  if (scores.length === 0) {
    return (
      <div className="flex h-32 items-center justify-center rounded-2xl border border-white/5 bg-white/[0.01]">
        <p className="text-sm text-zinc-500">No score trend data yet.</p>
      </div>
    );
  }

  const width = 720;
  const height = 220;
  const padding = 28;
  const point = (row: ConversationScore, index: number) => {
    const x = padding + (index / Math.max(1, rows.length - 1)) * (width - padding * 2);
    const y = padding + (1 - Math.max(0, Math.min(100, row.composite_score)) / 100) * (height - padding * 2);
    return `${x},${y}`;
  };
  const points = rows.map((row, index) => point(row, index)).join(" ");

  return (
    <div>
      <div className="mb-4 flex items-center justify-between">
        <div className="flex flex-wrap gap-2">
          {personas.map((persona) => (
            <button
              key={persona}
              onClick={() => setSelectedPersona(persona)}
              className={`rounded-full px-3 py-1 text-xs transition-colors ${
                selectedPersona === persona
                  ? "bg-pink-500/20 text-pink-200 border border-pink-500/30"
                  : "bg-white/5 text-zinc-400 border border-white/10 hover:bg-white/10"
              }`}
            >
              {persona === "All" ? "All Personas" : persona}
            </button>
          ))}
        </div>
      </div>
      
      {rows.length === 0 ? (
        <div className="flex h-64 items-center justify-center rounded-2xl border border-white/10 bg-black/20 p-2">
          <p className="text-sm text-zinc-500">No scores for this persona.</p>
        </div>
      ) : (
        <svg viewBox={`0 0 ${width} ${height}`} className="h-64 w-full overflow-visible rounded-2xl border border-white/10 bg-black/20 p-2">
          {[0, 25, 50, 75, 100].map((tick) => {
            const y = padding + (1 - tick / 100) * (height - padding * 2);
            return (
              <g key={tick}>
                <line x1={padding} x2={width - padding} y1={y} y2={y} stroke="rgba(255,255,255,0.08)" />
                <text x={4} y={y + 4} fill="rgba(255,255,255,0.35)" fontSize="10">
                  {tick}
                </text>
              </g>
            );
          })}
          <polyline points={points} fill="none" stroke={SYSTEM_COLOR} strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" />
          {rows.map((row, index) => {
            const [x, y] = point(row, index).split(",").map(Number);
            return <circle key={row.id} cx={x} cy={y} r="4" fill={SYSTEM_COLOR} />;
          })}
        </svg>
      )}
      <div className="mt-3 flex flex-wrap gap-3 text-xs text-zinc-400">
        <span className="inline-flex items-center gap-2">
          <span className="size-2 rounded-full" style={{ backgroundColor: SYSTEM_COLOR }} />
          System (full flow)
        </span>
        <span className="ml-auto text-zinc-600">{fmtInt(rows.length)} chronological score rows</span>
      </div>
    </div>
  );
}

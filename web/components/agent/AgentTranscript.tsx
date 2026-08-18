"use client";

import clsx from "clsx";
import { useId, useState } from "react";
import { JsonView, Segmented } from "@/components/ui";
import { Icons } from "@/components/ui/icons";
import type { Item } from "@/lib/types";
import {
  agentTranscriptOf,
  type AgentToolCall,
  type AgentTranscript,
  type AgentTurn,
  type AgentUsage,
} from "./transcript";

/* ---------------------------------------------------------------------------
   The agent transcript, rendered as a transcript.

   An agent that answers wrongly is only debuggable if you can read what it
   looked at, and a 400-line JSON blob is not reading. Every turn is a block, and
   every tool call inside it shows its arguments beside its result — but the raw
   JSON stays one toggle away, because a viewer that hides data is worse than no
   viewer.
   --------------------------------------------------------------------------- */

type View = "transcript" | "json";

const viewOptions: { value: View; label: string }[] = [
  { value: "transcript", label: "Transcript" },
  { value: "json", label: "JSON" },
];

/**
 * An output item that may be an agent's. Renders the transcript with its own
 * raw-JSON toggle, and falls back to plain JSON for every other node's output.
 */
export function AgentOutputView({ item, className }: { item: Item; className?: string }) {
  const [view, setView] = useState<View>("transcript");
  const transcript = agentTranscriptOf(item);

  if (!transcript) return <JsonView value={item.json} className={className} />;

  return (
    <div className="flex min-w-0 flex-col gap-3">
      <div className="flex items-center justify-between gap-3">
        <span className="text-xs font-semibold text-ink-3">Agent run</span>
        <Segmented value={view} options={viewOptions} onChange={setView} size="sm" />
      </div>
      {view === "transcript" ? (
        <AgentTranscriptView transcript={transcript} />
      ) : (
        <JsonView value={item.json} className={className} />
      )}
    </div>
  );
}

export function AgentTranscriptView({ transcript }: { transcript: AgentTranscript }) {
  return (
    <div className="flex min-w-0 flex-col gap-2.5">
      <TranscriptHeader transcript={transcript} />
      {transcript.turns.length === 0 ? (
        <p className="text-xs text-ink-4">This run recorded no turns.</p>
      ) : (
        transcript.turns.map((turn, i) => (
          <TurnBlock key={turn.turn ?? i} turn={turn} index={i} />
        ))
      )}
    </div>
  );
}

function TranscriptHeader({ transcript }: { transcript: AgentTranscript }) {
  const { toolCalls, model, stopReason, usage, turns } = transcript;
  const tokens = tokenLine(usage);

  return (
    <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1.5 rounded-[10px] border border-accent/22 bg-accent/8 px-3 py-2.5">
      <Icons.robot size={14} className="shrink-0 text-accent-2" />
      <span className="text-[11.5px] font-semibold text-ink-2">
        {turns.length} turn{turns.length === 1 ? "" : "s"}
      </span>
      {toolCalls !== undefined && (
        <Meta>
          {toolCalls} tool call{toolCalls === 1 ? "" : "s"}
        </Meta>
      )}
      {model && <Meta>{model}</Meta>}
      {stopReason && <Meta>stop: {stopReason}</Meta>}
      {tokens && <Meta>{tokens}</Meta>}
    </div>
  );
}

function TurnBlock({ turn, index }: { turn: AgentTurn; index: number }) {
  // Turns start open: the transcript is opened precisely to read them, and a
  // wall of collapsed rows would cost one click each to become useful.
  const [open, setOpen] = useState(true);
  const bodyId = `${useId()}turn`;
  const tokens = tokenLine(turn.usage);

  return (
    <section className="overflow-hidden rounded-[10px] border border-line bg-raised-2">
      <button
        type="button"
        aria-expanded={open}
        aria-controls={bodyId}
        onClick={() => setOpen(!open)}
        className={clsx(
          "flex w-full items-center gap-2 px-3 py-2 text-left",
          "hover:bg-white/4 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-accent",
        )}
      >
        <span className="shrink-0 text-ink-4">
          {open ? <Icons.chevronDown size={13} /> : <Icons.chevronRight size={13} />}
        </span>
        <span className="text-[12px] font-bold text-ink-2">Turn {turn.turn ?? index + 1}</span>
        {turn.toolCalls.length > 0 && (
          <Meta>
            {turn.toolCalls.length} tool call{turn.toolCalls.length === 1 ? "" : "s"}
          </Meta>
        )}
        {tokens && <span className="ml-auto font-mono text-[10px] text-ink-5">{tokens}</span>}
      </button>

      {open && (
        <div id={bodyId} className="flex min-w-0 flex-col gap-2.5 border-t border-line px-3 py-3">
          {turn.thinking && <Thinking text={turn.thinking} />}

          {turn.text && (
            <p className="whitespace-pre-wrap text-[12px] leading-relaxed text-ink-2">
              {turn.text}
            </p>
          )}

          {turn.toolCalls.map((call, i) => (
            <ToolCallBlock key={`${call.tool}-${i}`} call={call} />
          ))}

          {!turn.thinking && !turn.text && turn.toolCalls.length === 0 && (
            <p className="text-[11.5px] text-ink-5">This turn recorded no content.</p>
          )}
        </div>
      )}
    </section>
  );
}

/** Reasoning is long, rarely the answer, and worth keeping one keystroke away. */
function Thinking({ text }: { text: string }) {
  const [open, setOpen] = useState(false);
  const bodyId = `${useId()}thinking`;

  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <button
        type="button"
        aria-expanded={open}
        aria-controls={bodyId}
        onClick={() => setOpen(!open)}
        className={clsx(
          "flex w-fit items-center gap-1.5 rounded-md px-1.5 py-0.5 text-[11px] text-ink-4",
          "hover:text-ink-3 focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-accent",
        )}
      >
        {open ? <Icons.chevronDown size={11} /> : <Icons.chevronRight size={11} />}
        Thinking
      </button>
      {open && (
        <p
          id={bodyId}
          className="whitespace-pre-wrap border-l-2 border-line-strong pl-2.5 font-mono text-[11px] leading-relaxed text-ink-5"
        >
          {text}
        </p>
      )}
    </div>
  );
}

function ToolCallBlock({ call }: { call: AgentToolCall }) {
  const failed = Boolean(call.error);

  return (
    <div
      className={clsx(
        "flex min-w-0 flex-col gap-2 rounded-[9px] border px-2.5 py-2.5",
        failed ? "border-danger/25 bg-danger/8" : "border-line-strong bg-white/3",
      )}
    >
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <span className={clsx("shrink-0", failed ? "text-danger" : "text-accent-2")}>
          <Icons.play size={11} />
        </span>
        <span
          className={clsx(
            "font-mono text-[11.5px] font-semibold",
            failed ? "text-danger-3" : "text-ink-2",
          )}
        >
          {call.tool}
        </span>
        {call.items !== undefined && (
          <Meta>
            {call.items} item{call.items === 1 ? "" : "s"}
          </Meta>
        )}
      </div>

      {call.args && Object.keys(call.args).length > 0 && (
        <Labelled label="Arguments">
          <JsonView value={call.args} className="max-h-[180px] py-2.5" />
        </Labelled>
      )}

      {call.error ? (
        <Labelled label="Error" tone="danger">
          <p className="whitespace-pre-wrap text-[11.5px] leading-relaxed text-danger-3">
            {call.error}
          </p>
        </Labelled>
      ) : (
        call.hasResult && (
          <Labelled label="Result">
            <JsonView value={call.result} className="max-h-[240px] py-2.5" />
          </Labelled>
        )
      )}
    </div>
  );
}

function Labelled({
  label,
  tone = "default",
  children,
}: {
  label: string;
  tone?: "default" | "danger";
  children: React.ReactNode;
}) {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <span
        className={clsx(
          "text-[10px] font-bold tracking-[0.06em]",
          tone === "danger" ? "text-danger" : "text-ink-5",
        )}
      >
        {label.toUpperCase()}
      </span>
      {children}
    </div>
  );
}

function Meta({ children }: { children: React.ReactNode }) {
  return (
    <span className="rounded-full bg-white/6 px-2 py-[1px] font-mono text-[10px] text-ink-4">
      {children}
    </span>
  );
}

/** Renders only the counts the transcript actually carries. */
function tokenLine(usage: AgentUsage | undefined): string | null {
  if (!usage) return null;
  const parts: string[] = [];
  if (usage.inputTokens !== undefined) parts.push(`${usage.inputTokens} in`);
  if (usage.outputTokens !== undefined) parts.push(`${usage.outputTokens} out`);
  return parts.length > 0 ? `${parts.join(" · ")} tokens` : null;
}

import type { Item } from "@/lib/types";

/* ---------------------------------------------------------------------------
   Parsing for the `_agent` transcript the AI Agent node attaches to its output
   item.

   The item's json is `Record<string, unknown>` — it came from a jsonb column, so
   nothing about its shape is guaranteed at compile time. Every field is read
   defensively and left `undefined` when it is absent, because the viewer must
   render nothing rather than invent a zero the model never reported.
   --------------------------------------------------------------------------- */

/** The field the Go node writes its transcript into. */
export const AGENT_FIELD = "_agent";

export interface AgentUsage {
  inputTokens?: number;
  outputTokens?: number;
}

export interface AgentToolCall {
  tool: string;
  args?: Record<string, unknown>;
  /** Present separately from `result`, which may legitimately be `null`. */
  hasResult: boolean;
  result?: unknown;
  error?: string;
  items?: number;
}

export interface AgentTurn {
  turn?: number;
  text?: string;
  thinking?: string;
  toolCalls: AgentToolCall[];
  usage?: AgentUsage;
}

export interface AgentTranscript {
  turns: AgentTurn[];
  toolCalls?: number;
  model?: string;
  stopReason?: string;
  usage?: AgentUsage;
}

/** The transcript on an item, or null when the item is not an agent's output. */
export function agentTranscriptOf(item: Item | undefined): AgentTranscript | null {
  if (!item) return null;
  return parseAgentTranscript(item.json[AGENT_FIELD]);
}

/** True when any item in a list carries a transcript, so a pane can offer it. */
export function hasAgentTranscript(items: Item[]): boolean {
  return items.some((item) => agentTranscriptOf(item) !== null);
}

export function parseAgentTranscript(value: unknown): AgentTranscript | null {
  const obj = asObject(value);
  if (!obj) return null;

  const rawTurns = Array.isArray(obj.turns) ? obj.turns : [];
  return {
    turns: rawTurns.map(parseTurn),
    toolCalls: asNumber(obj.toolCalls),
    model: asText(obj.model),
    stopReason: asText(obj.stopReason),
    usage: parseUsage(obj.usage),
  };
}

function parseTurn(value: unknown): AgentTurn {
  const obj = asObject(value) ?? {};
  const rawCalls = Array.isArray(obj.toolCalls) ? obj.toolCalls : [];
  return {
    turn: asNumber(obj.turn),
    text: asText(obj.text),
    thinking: asText(obj.thinking),
    toolCalls: rawCalls.map(parseToolCall),
    usage: parseUsage(obj.usage),
  };
}

function parseToolCall(value: unknown): AgentToolCall {
  const obj = asObject(value) ?? {};
  return {
    // A nameless call cannot happen through the Go node, but a hand-edited row
    // must still render as something a person can read.
    tool: asText(obj.tool) ?? "unnamed tool",
    args: asObject(obj.args),
    hasResult: "result" in obj,
    result: obj.result,
    error: asText(obj.error),
    items: asNumber(obj.items),
  };
}

function parseUsage(value: unknown): AgentUsage | undefined {
  const obj = asObject(value);
  if (!obj) return undefined;
  const inputTokens = asNumber(obj.inputTokens);
  const outputTokens = asNumber(obj.outputTokens);
  if (inputTokens === undefined && outputTokens === undefined) return undefined;
  return { inputTokens, outputTokens };
}

function asObject(value: unknown): Record<string, unknown> | undefined {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return undefined;
  return value as Record<string, unknown>;
}

/** Empty strings are dropped: the Go side omits them, so "" means "nothing". */
function asText(value: unknown): string | undefined {
  return typeof value === "string" && value !== "" ? value : undefined;
}

function asNumber(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

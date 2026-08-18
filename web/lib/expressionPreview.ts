/* ---------------------------------------------------------------------------
   Client-side preview of `{{ … }}` expressions for the config drawer.

   This is deliberately NOT an evaluator. The real one is `internal/expr` on the
   server, built on expr-lang: it does arithmetic, comparisons, function calls
   and typed single-expression returns. Reimplementing that in the browser would
   guarantee the two drift apart, and the drawer only ever needs to answer one
   question — "roughly what will this resolve to?" — while the user types.

   So the scope here is property access only: a root (`$json`, `$node["Name"]`,
   `$itemIndex`, `$now`) followed by dotted or bracketed path segments. Anything
   else renders as "undefined" rather than throwing, because a half-typed
   expression is the normal state of an input the user is editing.
   --------------------------------------------------------------------------- */

export interface PreviewEnv {
  json: Record<string, unknown>;
  nodes: Record<string, Record<string, unknown>>;
  itemIndex?: number;
  now?: Date;
}

const OPEN = "{{";
const CLOSE = "}}";

/** True when the template contains at least one complete `{{ … }}` run. */
export function hasExpression(template: string): boolean {
  const open = template.indexOf(OPEN);
  return open !== -1 && template.indexOf(CLOSE, open + OPEN.length) !== -1;
}

/**
 * Resolves the `{{ }}` runs in a template for the drawer's preview line.
 * Best-effort: an unresolvable path renders as "undefined" rather than throwing.
 * An unbalanced `{{` returns the input unchanged — the user is mid-keystroke,
 * not in error.
 */
export function previewExpression(template: string, env: PreviewEnv): string {
  let out = "";
  let cursor = 0;

  for (;;) {
    const open = template.indexOf(OPEN, cursor);
    if (open === -1) {
      out += template.slice(cursor);
      return out;
    }
    const close = template.indexOf(CLOSE, open + OPEN.length);
    // Unclosed run: the template is still being typed, so show it verbatim.
    if (close === -1) return template;

    out += template.slice(cursor, open);
    out += stringify(resolve(template.slice(open + OPEN.length, close).trim(), env));
    cursor = close + CLOSE.length;
  }
}

function stringify(value: unknown): string {
  if (value === undefined) return "undefined";
  if (value === null) return "null";
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  try {
    return JSON.stringify(value) ?? "undefined";
  } catch {
    return "undefined";
  }
}

interface Segment {
  key: string;
  /** A `.json` segment is optional: the server exposes `$node["N"].json.id`,
   *  but people write `$node["N"].id` and both should preview the same. */
  optional: boolean;
}

function resolve(source: string, env: PreviewEnv): unknown {
  const root = readRoot(source, env);
  if (!root) return undefined;

  const segments = readPath(source.slice(root.consumed));
  if (!segments) return undefined;

  let value = root.value;
  for (const seg of segments) {
    if (value === null || value === undefined) return undefined;
    if (typeof value !== "object") return undefined;
    const holder = value as Record<string, unknown>;
    if (seg.optional && !(seg.key in holder)) continue;
    value = holder[seg.key];
  }
  return value;
}

const ROOT_JSON = "$json";
const ROOT_NODE = "$node";
const ROOT_ITEM_INDEX = "$itemIndex";
const ROOT_NOW = "$now";

function readRoot(
  source: string,
  env: PreviewEnv,
): { value: unknown; consumed: number } | undefined {
  if (source.startsWith(ROOT_JSON)) {
    return { value: env.json, consumed: ROOT_JSON.length };
  }
  if (source.startsWith(ROOT_ITEM_INDEX)) {
    return { value: env.itemIndex ?? 0, consumed: ROOT_ITEM_INDEX.length };
  }
  if (source.startsWith(ROOT_NOW)) {
    return { value: (env.now ?? new Date()).toISOString(), consumed: ROOT_NOW.length };
  }
  if (source.startsWith(ROOT_NODE)) {
    // $node["Fetch profile"] or $node['Fetch profile']
    const m = /^\$node\s*\[\s*(["'])(.*?)\1\s*\]/.exec(source);
    if (!m) return undefined;
    return { value: env.nodes[m[2]], consumed: m[0].length };
  }
  return undefined;
}

/** `.a.b["c"][0]` -> segments. Returns undefined for anything else. */
function readPath(rest: string): Segment[] | undefined {
  const segments: Segment[] = [];
  let i = 0;

  while (i < rest.length) {
    if (rest[i] === ".") {
      const m = /^\.([A-Za-z_$][\w$]*)/.exec(rest.slice(i));
      if (!m) return undefined;
      segments.push({ key: m[1], optional: m[1] === "json" });
      i += m[0].length;
      continue;
    }
    if (rest[i] === "[") {
      const m = /^\[\s*(?:(["'])(.*?)\1|(\d+))\s*\]/.exec(rest.slice(i));
      if (!m) return undefined;
      segments.push({ key: m[2] ?? m[3], optional: false });
      i += m[0].length;
      continue;
    }
    // Trailing whitespace is fine; an operator or call is not — that is the
    // server evaluator's job, so we decline rather than guess.
    if (/^\s+$/.test(rest.slice(i))) return segments;
    return undefined;
  }
  return segments;
}

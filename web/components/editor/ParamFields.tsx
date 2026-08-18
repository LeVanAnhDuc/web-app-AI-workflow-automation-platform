"use client";

import clsx from "clsx";
import { Field, IconButton, Input, Select, Textarea, Toggle } from "@/components/ui";
import { Icons } from "@/components/ui/icons";
import { keyValuePairs, type KeyValuePair } from "@/lib/graph";
import type { ParamSpec } from "@/lib/types";

/* ---------------------------------------------------------------------------
   One control per ParamSpec. Nothing here knows a node type: adding a node in
   Go must never mean editing this file, so every branch is keyed on
   `spec.Type` alone.
   --------------------------------------------------------------------------- */

/** Operators the Go `if` node understands. Kept in one place so the filter rows
 *  and any future condition editor cannot drift apart. */
export const FILTER_OPERATORS: { value: string; label: string; unary?: boolean }[] = [
  { value: "equals", label: "equals" },
  { value: "notEquals", label: "not equals" },
  { value: "contains", label: "contains" },
  { value: "notContains", label: "not contains" },
  { value: "startsWith", label: "starts with" },
  { value: "endsWith", label: "ends with" },
  { value: "gt", label: "greater than" },
  { value: "gte", label: "greater or equal" },
  { value: "lt", label: "less than" },
  { value: "lte", label: "less or equal" },
  { value: "isEmpty", label: "is empty", unary: true },
  { value: "isNotEmpty", label: "is not empty", unary: true },
  { value: "isTrue", label: "is true", unary: true },
  { value: "isFalse", label: "is false", unary: true },
  { value: "regex", label: "matches regex" },
];

export interface FilterCondition {
  left: string;
  operator: string;
  right: string;
}

/** The `keyValue` row shape, parsed in lib/graph so validation shares it. */
export type KeyValueRow = KeyValuePair;

export function ParamField({
  spec,
  value,
  onChange,
  preview,
}: {
  spec: ParamSpec;
  value: unknown;
  onChange: (next: unknown) => void;
  /** Resolved preview for a template containing `{{ }}`, or null when there is
   *  nothing to preview. Supplied by the drawer, which owns the run data. */
  preview: (template: string) => string | null;
}) {
  if (spec.Type === "notice") {
    // A notice carries no value, so descriptors are free to put its prose in
    // whichever field reads best — Default is where the Go side usually puts it.
    const text = spec.Description || asText(spec.Default) || spec.Label;
    if (!text) return null;
    return (
      <div className="flex items-start gap-2.5 rounded-[10px] border border-line bg-white/3 px-3.5 py-3">
        <Icons.info size={14} className="mt-px shrink-0 text-ink-4" />
        <p className="text-xs leading-relaxed text-ink-3">{text}</p>
      </div>
    );
  }

  const missing = Boolean(spec.Required) && isEmptyValue(value);
  const invalid = missing ? "border-danger!" : undefined;

  const hint = missing ? (
    <span className="font-sans text-[11px] text-danger">{spec.Label} is required.</span>
  ) : (
    spec.Description && <span className="font-sans text-[11px] text-ink-4">{spec.Description}</span>
  );

  const text = asText(value);
  const previewLine = spec.SupportsExpression ? preview(text) : null;

  return (
    <Field
      label={spec.Label}
      hint={hint}
      action={
        spec.SupportsExpression ? (
          <ExpressionChip
            label={spec.Label}
            active={previewLine !== null}
            onInsert={() => onChange(`${text}{{ $json. }}`)}
          />
        ) : spec.Type === "keyValue" || spec.Type === "filter" ? (
          <AddRowButton
            label={spec.Label}
            onClick={() =>
              spec.Type === "keyValue"
                ? onChange([...asKeyValues(value), { key: "", value: "" }])
                : onChange([
                    ...asConditions(value),
                    { left: "", operator: "equals", right: "" },
                  ])
            }
          />
        ) : undefined
      }
    >
      <Control
        spec={spec}
        value={value}
        text={text}
        invalid={invalid}
        onChange={onChange}
      />
      {previewLine !== null && (
        <div className="font-mono text-[10.5px] leading-relaxed text-ink-5">
          → {previewLine}
        </div>
      )}
    </Field>
  );
}

function Control({
  spec,
  value,
  text,
  invalid,
  onChange,
}: {
  spec: ParamSpec;
  value: unknown;
  text: string;
  invalid?: string;
  onChange: (next: unknown) => void;
}) {
  switch (spec.Type) {
    case "boolean":
      return (
        <div className="flex items-center justify-between rounded-[10px] border border-line bg-input px-3.5 py-2.5">
          <span className="text-[12.5px] text-ink-2">{value ? "On" : "Off"}</span>
          <Toggle checked={Boolean(value)} onChange={onChange} label={spec.Label} />
        </div>
      );

    case "number":
      return (
        <Input
          type="number"
          value={text}
          placeholder={spec.Placeholder}
          className={clsx("font-mono", invalid)}
          onChange={(e) =>
            onChange(e.target.value === "" ? "" : Number(e.target.value))
          }
        />
      );

    case "select":
      return (
        <Select
          value={text}
          className={clsx("font-mono", invalid)}
          onChange={(e) => onChange(e.target.value)}
        >
          {!spec.Options?.some((o) => o.Value === text) && (
            <option value={text}>{text || "Select…"}</option>
          )}
          {(spec.Options ?? []).map((o) => (
            <option key={o.Value} value={o.Value}>
              {o.Label}
            </option>
          ))}
        </Select>
      );

    case "json":
      return (
        <Textarea
          rows={5}
          value={text}
          placeholder={spec.Placeholder ?? "{ }"}
          className={clsx("min-h-[104px]", invalid)}
          onChange={(e) => onChange(e.target.value)}
        />
      );

    case "code":
      return (
        <Textarea
          rows={12}
          spellCheck={false}
          value={text}
          placeholder={spec.Placeholder}
          className={clsx("min-h-[220px]", invalid)}
          onChange={(e) => onChange(e.target.value)}
        />
      );

    case "keyValue":
      return <KeyValueRows rows={asKeyValues(value)} label={spec.Label} onChange={onChange} />;

    case "filter":
      return (
        <FilterRows conditions={asConditions(value)} label={spec.Label} onChange={onChange} />
      );

    default:
      return (
        <Input
          value={text}
          placeholder={spec.Placeholder}
          className={clsx(spec.SupportsExpression && "font-mono text-xs", invalid)}
          onChange={(e) => onChange(e.target.value)}
        />
      );
  }
}

function KeyValueRows({
  rows,
  label,
  onChange,
}: {
  rows: KeyValueRow[];
  label: string;
  onChange: (next: KeyValueRow[]) => void;
}) {
  const patch = (index: number, part: Partial<KeyValueRow>) =>
    onChange(rows.map((r, i) => (i === index ? { ...r, ...part } : r)));

  if (rows.length === 0) {
    return (
      <button
        type="button"
        onClick={() => onChange([{ key: "", value: "" }])}
        className="rounded-[10px] border border-dashed border-line-strong px-3.5 py-3 text-left text-xs text-ink-4 hover:text-ink-3"
      >
        No {label.toLowerCase()} yet — add the first one.
      </button>
    );
  }

  return (
    <div className="flex flex-col gap-2">
      {rows.map((row, i) => (
        <div key={i} className="flex items-center gap-2">
          <Input
            value={row.key}
            aria-label={`${label} name ${i + 1}`}
            placeholder="Name"
            className="grow py-2.5 font-mono text-[11.5px]"
            onChange={(e) => patch(i, { key: e.target.value })}
          />
          <Input
            value={row.value}
            aria-label={`${label} value ${i + 1}`}
            placeholder="Value"
            className="grow py-2.5 font-mono text-[11.5px]"
            onChange={(e) => patch(i, { value: e.target.value })}
          />
          <IconButton
            aria-label={`Remove ${label} row ${i + 1}`}
            onClick={() => onChange(rows.filter((_, j) => j !== i))}
          >
            <Icons.trash size={14} />
          </IconButton>
        </div>
      ))}
    </div>
  );
}

function FilterRows({
  conditions,
  label,
  onChange,
}: {
  conditions: FilterCondition[];
  label: string;
  onChange: (next: FilterCondition[]) => void;
}) {
  const patch = (index: number, part: Partial<FilterCondition>) =>
    onChange(conditions.map((c, i) => (i === index ? { ...c, ...part } : c)));

  if (conditions.length === 0) {
    return (
      <button
        type="button"
        onClick={() => onChange([{ left: "", operator: "equals", right: "" }])}
        className="rounded-[10px] border border-dashed border-line-strong px-3.5 py-3 text-left text-xs text-ink-4 hover:text-ink-3"
      >
        No condition yet — add the first one.
      </button>
    );
  }

  return (
    <div className="flex flex-col gap-2.5">
      {conditions.map((condition, i) => {
        const unary = FILTER_OPERATORS.find((o) => o.value === condition.operator)?.unary;
        return (
          <div key={i} className="flex flex-col gap-1.5 rounded-[10px] border border-line p-2">
            <div className="flex items-center gap-2">
              <Input
                value={condition.left}
                aria-label={`${label} left value ${i + 1}`}
                placeholder="{{ $json.field }}"
                className="grow py-2 font-mono text-[11.5px]"
                onChange={(e) => patch(i, { left: e.target.value })}
              />
              <IconButton
                aria-label={`Remove condition ${i + 1}`}
                onClick={() => onChange(conditions.filter((_, j) => j !== i))}
              >
                <Icons.trash size={14} />
              </IconButton>
            </div>
            <div className="flex items-center gap-2">
              <div className="w-[150px] shrink-0">
                <Select
                  value={condition.operator}
                  aria-label={`${label} operator ${i + 1}`}
                  className="py-2 text-[12px]"
                  onChange={(e) => patch(i, { operator: e.target.value })}
                >
                  {FILTER_OPERATORS.map((o) => (
                    <option key={o.value} value={o.value}>
                      {o.label}
                    </option>
                  ))}
                </Select>
              </div>
              {!unary && (
                <Input
                  value={condition.right}
                  aria-label={`${label} right value ${i + 1}`}
                  placeholder="Value"
                  className="grow py-2 font-mono text-[11.5px]"
                  onChange={(e) => patch(i, { right: e.target.value })}
                />
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
}

function ExpressionChip({
  label,
  active,
  onInsert,
}: {
  label: string;
  active: boolean;
  onInsert: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onInsert}
      aria-label={`Insert an expression into ${label}`}
      title="Insert {{ }} — resolved from the last run"
      className={clsx(
        "rounded-full px-2 py-[2px] text-[10.5px] font-semibold",
        active ? "bg-accent/20 text-accent-3" : "bg-accent/14 text-accent-2 hover:bg-accent/22",
      )}
    >
      expression
    </button>
  );
}

function AddRowButton({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={`Add a ${label} row`}
      className="text-xs font-semibold text-accent-2 hover:text-accent-3"
    >
      + Add
    </button>
  );
}

/* --- value coercion --------------------------------------------------------
   Params arrive as `unknown` from a jsonb column, so every control has to cope
   with a shape it did not write (an older graph, a hand-edited JSON).
   -------------------------------------------------------------------------- */

export function isEmptyValue(value: unknown): boolean {
  if (value === undefined || value === null || value === "") return true;
  if (Array.isArray(value)) return value.length === 0;
  return false;
}

function asText(value: unknown): string {
  if (value === undefined || value === null) return "";
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return JSON.stringify(value, null, 2);
}

function asKeyValues(value: unknown): KeyValueRow[] {
  return keyValuePairs(value);
}

function asConditions(value: unknown): FilterCondition[] {
  if (!Array.isArray(value)) return [];
  return value.map((row) => {
    if (typeof row !== "object" || row === null) {
      return { left: "", operator: "equals", right: "" };
    }
    const r = row as Record<string, unknown>;
    return {
      left: String(r.left ?? r.value1 ?? ""),
      operator: String(r.operator ?? "equals"),
      right: String(r.right ?? r.value2 ?? ""),
    };
  });
}

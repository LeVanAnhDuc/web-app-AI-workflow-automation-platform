"use client";

import clsx from "clsx";
import { useEffect, useMemo, useRef, useState } from "react";
import { Kbd, Modal } from "@/components/ui";
import { Icons, NodeIcon } from "@/components/ui/icons";
import type { Graph, NodeDescriptor } from "@/lib/types";

const CATEGORY_ORDER = ["Triggers", "Core", "Flow", "AI"];

interface Row {
  descriptor: NodeDescriptor;
  inUse: boolean;
}

/**
 * The keyboard-first way to add a node: `Tab` on the canvas, type to filter,
 * `↑`/`↓` to move, `↵` to add. Rows are grouped by category and an already-used
 * trigger stays visible marked "in use" rather than vanishing, so the list never
 * changes shape for a reason the user cannot see.
 */
export function NodePicker({
  open,
  onClose,
  descriptors,
  graph,
  onAdd,
}: {
  open: boolean;
  onClose: () => void;
  descriptors: NodeDescriptor[];
  graph: Graph;
  onAdd: (type: string) => void;
}) {
  const [query, setQuery] = useState("");
  const [category, setCategory] = useState<string>("All");
  const [highlight, setHighlight] = useState(0);
  const listRef = useRef<HTMLDivElement>(null);

  // A workflow may hold exactly one trigger, so once one exists every trigger
  // row is a dead end. Say so on the row instead of silently dropping it.
  const triggerTaken = useMemo(
    () => graph.nodes.some((n) => descriptors.find((d) => d.Type === n.type)?.IsTrigger),
    [graph.nodes, descriptors],
  );

  const term = query.trim().toLowerCase();
  const filtered = useMemo(
    () =>
      descriptors
        .filter((d) => (category === "All" ? true : d.Category === category))
        .filter((d) => matches(d, term))
        .map<Row>((d) => ({ descriptor: d, inUse: d.IsTrigger && triggerTaken })),
    [descriptors, category, term, triggerTaken],
  );

  const groups = useMemo(() => groupRows(filtered), [filtered]);
  const flat = useMemo(() => groups.flatMap((g) => g.rows), [groups]);
  const addable = (row: Row | undefined) => Boolean(row && !row.inUse);

  const counts = useMemo(() => {
    const visible = descriptors.filter((d) => matches(d, term));
    const byCategory = new Map<string, number>();
    for (const d of visible) byCategory.set(d.Category, (byCategory.get(d.Category) ?? 0) + 1);
    return { all: visible.length, byCategory };
  }, [descriptors, term]);

  useEffect(() => {
    if (open) {
      setQuery("");
      setCategory("All");
    }
  }, [open]);

  // Keep the highlight inside the list as filtering shrinks it, and prefer a row
  // Enter can actually act on — landing on an "in use" trigger makes the key
  // look broken.
  useEffect(() => {
    setHighlight((h) => {
      if (flat.length === 0) return 0;
      const clamped = Math.min(h, flat.length - 1);
      if (!flat[clamped].inUse) return clamped;
      const firstAddable = flat.findIndex((r) => !r.inUse);
      return firstAddable === -1 ? clamped : firstAddable;
    });
  }, [flat]);

  useEffect(() => {
    const el = listRef.current?.querySelector<HTMLElement>('[data-highlighted="true"]');
    el?.scrollIntoView({ block: "nearest" });
  }, [highlight, flat.length]);

  const commit = (index: number) => {
    const row = flat[index];
    if (!addable(row)) return;
    onAdd(row.descriptor.Type);
    onClose();
  };

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setHighlight((h) => (flat.length === 0 ? 0 : (h + 1) % flat.length));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setHighlight((h) => (flat.length === 0 ? 0 : (h - 1 + flat.length) % flat.length));
    } else if (e.key === "Enter") {
      e.preventDefault();
      commit(highlight);
    }
  };

  return (
    <Modal open={open} onClose={onClose} width={680} labelledBy="node-picker-title">
      {/* One key handler for the whole dialog: the search input keeps focus, so
          arrow keys reach here no matter which row is highlighted. */}
      <div onKeyDown={onKeyDown} className="flex max-h-[70vh] flex-col">
        <div className="flex items-center gap-3.5 border-b border-line-strong px-5 py-4">
          <Icons.search size={18} className="shrink-0 text-ink-3" />
          <h2 id="node-picker-title" className="sr-only">
            Add a node
          </h2>
          {/* eslint-disable-next-line jsx-a11y/no-autofocus -- the dialog exists to be typed into */}
          <input
            autoFocus
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search nodes"
            aria-label="Search nodes"
            role="combobox"
            aria-expanded
            aria-controls="node-picker-list"
            aria-activedescendant={flat[highlight] ? rowId(flat[highlight]) : undefined}
            className="grow bg-transparent text-base font-medium text-ink outline-none placeholder:text-ink-4"
          />
          <Kbd>esc</Kbd>
        </div>

        <div className="flex min-h-0 grow">
          <nav
            aria-label="Categories"
            className="flex w-[164px] shrink-0 flex-col gap-[3px] border-r border-line px-2.5 py-3"
          >
            <RailButton
              label="All"
              count={counts.all}
              active={category === "All"}
              onClick={() => setCategory("All")}
            />
            {CATEGORY_ORDER.map((c) => (
              <RailButton
                key={c}
                label={c}
                count={counts.byCategory.get(c) ?? 0}
                active={category === c}
                onClick={() => setCategory(c)}
              />
            ))}
          </nav>

          <div className="flex min-w-0 grow flex-col">
            <div
              ref={listRef}
              id="node-picker-list"
              role="listbox"
              aria-label="Node types"
              className="flex min-h-[200px] grow flex-col gap-[3px] overflow-y-auto px-3 py-3"
            >
              {groups.map((group) => (
                <div
                  key={group.category}
                  role="group"
                  aria-label={group.category}
                  className="flex flex-col gap-[3px]"
                >
                  <div className="px-3 pb-1.5 pt-1.5 text-[10.5px] font-bold tracking-[0.07em] text-ink-5">
                    {group.category.toUpperCase()}
                  </div>
                  {group.rows.map((row) => {
                    const index = flat.indexOf(row);
                    return (
                      <PickerRow
                        key={row.descriptor.Type}
                        row={row}
                        highlighted={index === highlight}
                        onHover={() => setHighlight(index)}
                        onSelect={() => commit(index)}
                      />
                    );
                  })}
                </div>
              ))}
              {flat.length === 0 && (
                <p className="px-3 py-10 text-center text-[13px] text-ink-4">
                  No node type matches “{query}”.
                </p>
              )}
            </div>

            <div className="flex items-center gap-[11px] border-t border-line px-3 py-3">
              <Kbd>↑↓</Kbd>
              <span className="text-[11.5px] text-ink-4">navigate</span>
              <Kbd>↵</Kbd>
              <span className="text-[11.5px] text-ink-4">add to canvas</span>
              <div className="grow" />
              <span className="text-[11.5px] text-ink-5">
                {flat.length} of {descriptors.length} node types
              </span>
            </div>
          </div>
        </div>
      </div>
    </Modal>
  );
}

function RailButton({
  label,
  count,
  active,
  onClick,
}: {
  label: string;
  count: number;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={clsx(
        "flex items-center justify-between rounded-lg px-[11px] py-2",
        active ? "bg-white/7" : "hover:bg-white/4",
      )}
    >
      <span
        className={clsx(
          "text-[12.5px]",
          active ? "font-semibold text-ink" : count === 0 ? "text-ink-5" : "text-ink-3",
        )}
      >
        {label}
      </span>
      <span className={clsx("font-mono text-[10.5px]", active ? "text-ink-3" : "text-ink-5")}>
        {count}
      </span>
    </button>
  );
}

function PickerRow({
  row,
  highlighted,
  onHover,
  onSelect,
}: {
  row: Row;
  highlighted: boolean;
  onHover: () => void;
  onSelect: () => void;
}) {
  const { descriptor, inUse } = row;
  return (
    <div
      id={rowId(row)}
      role="option"
      aria-selected={highlighted}
      aria-disabled={inUse}
      data-highlighted={highlighted}
      onMouseMove={onHover}
      onClick={onSelect}
      className={clsx(
        "flex items-center gap-3.5 rounded-[10px] border px-3 py-[11px]",
        inUse ? "cursor-not-allowed border-transparent" : "cursor-pointer",
        highlighted
          ? "border-accent/35 bg-accent/14"
          : "border-transparent hover:bg-white/4",
      )}
    >
      <span
        className={clsx(
          "flex h-9 w-9 shrink-0 items-center justify-center rounded-[10px]",
          highlighted ? "bg-accent/20 text-accent-3" : "bg-white/6 text-ink-2",
        )}
      >
        <NodeIcon name={descriptor.Icon} size={17} />
      </span>
      <span className="flex min-w-0 grow flex-col gap-[3px]">
        <span
          className={clsx(
            "truncate text-[13.5px]",
            highlighted ? "font-bold" : "font-semibold",
            inUse && "text-ink-3",
          )}
        >
          {descriptor.Name}
        </span>
        <span className="truncate text-xs text-ink-3">{descriptor.Description}</span>
      </span>
      {inUse ? (
        <span className="shrink-0 rounded-md bg-white/5 px-2 py-[3px] font-mono text-[10px] text-ink-4">
          in use
        </span>
      ) : (
        highlighted && (
          <span className="shrink-0 rounded-md bg-accent/22 px-2.5 py-1 font-mono text-[10.5px] text-accent-3">
            ↵ add
          </span>
        )
      )}
    </div>
  );
}

/** Stable per-row id so the search box can point `aria-activedescendant` at it. */
function rowId(row: Row): string {
  return `node-picker-${row.descriptor.Type.replace(/[^a-zA-Z0-9]/g, "-")}`;
}

function groupRows(rows: Row[]): { category: string; rows: Row[] }[] {
  const groups: { category: string; rows: Row[] }[] = [];
  for (const row of rows) {
    const category = row.descriptor.Category || "Other";
    const existing = groups.find((g) => g.category === category);
    if (existing) existing.rows.push(row);
    else groups.push({ category, rows: [row] });
  }
  return groups;
}

function matches(d: NodeDescriptor, term: string): boolean {
  if (!term) return true;
  return (
    d.Name.toLowerCase().includes(term) ||
    d.Type.toLowerCase().includes(term) ||
    (d.Description ?? "").toLowerCase().includes(term)
  );
}

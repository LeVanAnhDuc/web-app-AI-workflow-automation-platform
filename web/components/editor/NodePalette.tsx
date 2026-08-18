"use client";

import clsx from "clsx";
import { useMemo, useState } from "react";
import { Input, SectionLabel } from "@/components/ui";
import { Icons, NodeIcon } from "@/components/ui/icons";
import type { NodeDescriptor } from "@/lib/types";

/** Drag payload the canvas listens for. A private MIME type so a stray drag
 *  from elsewhere in the page cannot create a node. */
export const NODE_DND_MIME = "application/x-flowgrid-node-type";

interface Group {
  category: string;
  items: NodeDescriptor[];
}

export function NodePalette({
  descriptors,
  onAdd,
}: {
  descriptors: NodeDescriptor[];
  onAdd: (type: string) => void;
}) {
  const [query, setQuery] = useState("");
  const term = query.trim().toLowerCase();

  const groups = useMemo(() => groupByCategory(descriptors, term), [descriptors, term]);

  return (
    <aside className="flex w-60 shrink-0 flex-col gap-1 overflow-y-auto border-r border-line bg-panel px-3 py-3.5">
      <div className="relative mb-2">
        <Icons.search
          size={14}
          className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-ink-4"
        />
        <Input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search nodes"
          aria-label="Search nodes"
          className="py-2.5 pl-9 text-[13px]"
        />
      </div>

      {groups.map((group) => (
        <div key={group.category} className="flex flex-col gap-0.5">
          <div className="px-3 pb-1.5 pt-2">
            <SectionLabel>{group.category.toUpperCase()}</SectionLabel>
          </div>
          {group.items.map((d) => (
            <PaletteRow key={d.Type} descriptor={d} onAdd={onAdd} />
          ))}
        </div>
      ))}

      {groups.length === 0 && (
        <p className="px-3 py-6 text-center text-xs text-ink-4">
          No node matches “{query}”.
        </p>
      )}
    </aside>
  );
}

function PaletteRow({
  descriptor,
  onAdd,
}: {
  descriptor: NodeDescriptor;
  onAdd: (type: string) => void;
}) {
  return (
    <button
      type="button"
      draggable
      onClick={() => onAdd(descriptor.Type)}
      onDragStart={(e) => {
        e.dataTransfer.setData(NODE_DND_MIME, descriptor.Type);
        e.dataTransfer.effectAllowed = "copy";
      }}
      title={descriptor.Description}
      className={clsx(
        "flex items-center gap-[11px] rounded-[10px] px-3 py-[9px] text-left text-[13px] text-ink-2",
        "cursor-grab hover:bg-white/5 hover:text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent",
      )}
    >
      <NodeIcon name={descriptor.Icon} size={15} className="shrink-0 text-ink-3" />
      <span className="truncate">{descriptor.Name}</span>
    </button>
  );
}

/** Keeps the API's own ordering: a category appears where it first occurs. */
function groupByCategory(descriptors: NodeDescriptor[], term: string): Group[] {
  const groups: Group[] = [];
  for (const d of descriptors) {
    if (term && !matches(d, term)) continue;
    const existing = groups.find((g) => g.category === d.Category);
    if (existing) existing.items.push(d);
    else groups.push({ category: d.Category || "Other", items: [d] });
  }
  return groups;
}

function matches(d: NodeDescriptor, term: string): boolean {
  return (
    d.Name.toLowerCase().includes(term) ||
    d.Type.toLowerCase().includes(term) ||
    (d.Description ?? "").toLowerCase().includes(term)
  );
}

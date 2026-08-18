"use client";

import { useId } from "react";
import { Segmented } from "@/components/ui";
import { Icons } from "@/components/ui/icons";
import type { WorkflowListQuery } from "@/lib/api";

export type StatusFilter = NonNullable<WorkflowListQuery["status"]>;
export type SortKey = NonNullable<WorkflowListQuery["sort"]>;

const sortOptions: { value: SortKey; label: string }[] = [
  { value: "updated", label: "Recently updated" },
  { value: "name", label: "Name" },
  { value: "created", label: "Recently created" },
];

export function FilterBar({
  q,
  onQChange,
  status,
  onStatusChange,
  sort,
  onSortChange,
}: {
  q: string;
  onQChange: (next: string) => void;
  status: StatusFilter;
  onStatusChange: (next: StatusFilter) => void;
  sort: SortKey;
  onSortChange: (next: SortKey) => void;
}) {
  const filterId = useId();
  const sortId = useId();

  return (
    <div className="flex flex-wrap items-center gap-3">
      <label
        htmlFor={filterId}
        className="flex w-[280px] items-center gap-2.5 rounded-[10px] border border-line-strong bg-panel px-3.5 py-2.5 focus-within:border-accent focus-within:ring-4 focus-within:ring-accent/14"
      >
        <Icons.search size={14} className="shrink-0 text-ink-4" />
        <input
          id={filterId}
          type="search"
          value={q}
          onChange={(e) => onQChange(e.target.value)}
          placeholder="Filter by name"
          className="min-w-0 grow bg-transparent text-[13px] text-ink outline-none placeholder:text-ink-4"
        />
      </label>

      <Segmented<StatusFilter>
        value={status}
        onChange={onStatusChange}
        options={[
          { value: "all", label: "All" },
          { value: "active", label: "Active" },
          { value: "inactive", label: "Inactive" },
        ]}
      />

      <div className="grow" />

      {/* A native select keeps keyboard and screen-reader behaviour for free;
          only the chevron is custom, as in the primitive Select. */}
      <div className="relative flex items-center gap-2 rounded-[10px] border border-line-strong bg-panel pl-3.5 focus-within:border-accent focus-within:ring-4 focus-within:ring-accent/14">
        <label htmlFor={sortId} className="text-[12.5px] text-ink-3">
          Sort
        </label>
        <select
          id={sortId}
          value={sort}
          onChange={(e) => onSortChange(e.target.value as SortKey)}
          className="appearance-none bg-transparent py-2.5 pr-8 text-[12.5px] font-semibold text-ink outline-none"
        >
          {sortOptions.map((o) => (
            <option key={o.value} value={o.value} className="bg-panel text-ink">
              {o.label}
            </option>
          ))}
        </select>
        <Icons.chevronDown
          size={12}
          className="pointer-events-none absolute right-3 text-ink-4"
        />
      </div>
    </div>
  );
}

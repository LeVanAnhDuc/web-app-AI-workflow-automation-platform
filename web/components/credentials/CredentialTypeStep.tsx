"use client";

import clsx from "clsx";
import { useMemo, useState } from "react";
import { ErrorNotice, Spinner } from "@/components/ui";
import { Icons } from "@/components/ui/icons";
import type { CredentialType } from "@/lib/types";
import { TypeIconChip } from "./TypeIcon";

/* Step 1 of the modal: which kind of credential this is.

   The registry has no category field, so the grouping is by how the credential
   is obtained — that is the distinction that matters to the person choosing:
   one group needs a browser round trip to a provider, the other needs a value
   pasted from somewhere. */

const GROUPS: { key: "oauth" | "keys"; label: string; hint: string }[] = [
  {
    key: "oauth",
    label: "CONNECT AN ACCOUNT",
    hint: "Authorise once in the provider; tokens are refreshed for you.",
  },
  {
    key: "keys",
    label: "KEYS AND TOKENS",
    hint: "Paste a value the provider issued you.",
  },
];

export function CredentialTypeStep({
  types,
  isPending,
  error,
  onPick,
}: {
  types: CredentialType[];
  isPending: boolean;
  error?: string;
  onPick: (type: CredentialType) => void;
}) {
  const [query, setQuery] = useState("");
  const term = query.trim().toLowerCase();

  const groups = useMemo(() => {
    const matching = types.filter((t) => matches(t, term));
    return GROUPS.map((g) => ({
      ...g,
      rows: matching.filter((t) => (g.key === "oauth" ? t.auth === "oauth2" : t.auth !== "oauth2")),
    })).filter((g) => g.rows.length > 0);
  }, [types, term]);

  return (
    <>
      <div className="flex items-center gap-3.5 border-b border-line-strong px-5 py-4">
        <Icons.search size={18} className="shrink-0 text-ink-3" />
        {/* eslint-disable-next-line jsx-a11y/no-autofocus -- the step exists to be typed into */}
        <input
          autoFocus
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search credential types"
          aria-label="Search credential types"
          className="grow bg-transparent text-base font-medium text-ink outline-none placeholder:text-ink-4"
        />
      </div>

      <div className="flex min-h-[240px] flex-col gap-3 overflow-y-auto px-4 py-4">
        {error && <ErrorNotice>{error}</ErrorNotice>}

        {isPending && (
          <div className="flex items-center gap-2.5 px-2 py-8 text-[13px] text-ink-4">
            <Spinner className="h-4 w-4" />
            Loading credential types…
          </div>
        )}

        {!isPending && groups.length === 0 && !error && (
          <p className="px-2 py-10 text-center text-[13px] text-ink-4">
            {types.length === 0
              ? "This build registers no credential types."
              : `No credential type matches “${query}”.`}
          </p>
        )}

        {groups.map((group) => (
          <div key={group.key} role="group" aria-label={group.label} className="flex flex-col gap-1">
            <div className="flex items-baseline gap-2.5 px-2 pb-1">
              <span className="text-[10.5px] font-bold tracking-[0.07em] text-ink-5">
                {group.label}
              </span>
              <span className="truncate text-[11px] text-ink-5">{group.hint}</span>
            </div>
            {group.rows.map((t) => (
              <button
                key={t.type}
                type="button"
                onClick={() => onPick(t)}
                className={clsx(
                  "flex items-center gap-3.5 rounded-[10px] border border-transparent px-3 py-[11px] text-left",
                  "hover:border-accent/35 hover:bg-accent/12",
                  "focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-accent",
                )}
              >
                <TypeIconChip icon={t.icon} size={36} />
                <span className="flex min-w-0 grow flex-col gap-[3px]">
                  <span className="truncate text-[13.5px] font-semibold text-ink">{t.name}</span>
                  <span className="truncate text-xs text-ink-3">
                    {t.description || t.type}
                  </span>
                </span>
                <Icons.chevronRight size={14} className="shrink-0 text-ink-5" />
              </button>
            ))}
          </div>
        ))}
      </div>
    </>
  );
}

function matches(t: CredentialType, term: string): boolean {
  if (!term) return true;
  return (
    t.name.toLowerCase().includes(term) ||
    t.type.toLowerCase().includes(term) ||
    (t.description ?? "").toLowerCase().includes(term)
  );
}

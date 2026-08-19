"use client";

import clsx from "clsx";
import { useState } from "react";
import Link from "next/link";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { credentials as credentialsApi } from "@/lib/api";
import { Field, Select, Spinner } from "@/components/ui";
import { Icons } from "@/components/ui/icons";
import type { CredentialSummary } from "@/lib/types";
import { CredentialModal, apiMessage, type CredentialModalTarget } from "./CredentialModal";
import { CredentialStatusLine } from "./CredentialStatusPill";
import { TypeIconChip } from "./TypeIcon";
import { credentialStatus } from "./status";
import { useCredentialTypes } from "./useCredentialTypes";

/**
 * The credential a node authenticates with, shown above its parameters.
 *
 * The status is the same rule the Credentials screen uses: a node pointing at
 * an expired OAuth credential is going to fail at run time, and the drawer is
 * where the author can still do something about it.
 */
export function CredentialPicker({
  /** The `Credential` field of the node descriptor: a credential type id. */
  typeId,
  value,
  onChange,
}: {
  typeId: string;
  value: string | null | undefined;
  onChange: (credentialId: string | null) => void;
}) {
  const qc = useQueryClient();
  const { byType, isPending: typesPending } = useCredentialTypes();
  const [modal, setModal] = useState<CredentialModalTarget | null>(null);

  const type = byType.get(typeId);

  const list = useQuery({
    queryKey: ["credentials", { type: typeId }],
    queryFn: () => credentialsApi.list(typeId),
  });

  const options = list.data?.credentials ?? [];
  const selected: CredentialSummary | undefined = options.find((c) => c.id === value);
  const status = selected ? credentialStatus(selected, type) : undefined;
  const typeName = type?.name ?? typeId;

  // A selected credential the list no longer holds means it was deleted (or
  // belongs to another type) — silently showing "none" would hide a real break.
  const danglingId = value && !selected && !list.isPending ? value : null;

  const openNew = () => setModal({ mode: "create", type: typeId });

  return (
    <div className="flex flex-col gap-2 rounded-[10px] border border-line bg-white/3 px-3.5 py-3">
      <div className="flex items-center gap-2">
        <Icons.lock size={13} className="shrink-0 text-ink-4" />
        <span className="text-[11px] font-bold tracking-[0.06em] text-ink-5">CREDENTIAL</span>
        <div className="grow" />
        <Link
          href="/credentials"
          className="rounded text-[11px] text-ink-4 hover:text-ink-2 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
        >
          Manage
        </Link>
      </div>

      {list.isPending || typesPending ? (
        <span className="flex items-center gap-2 py-1 text-[11.5px] text-ink-4">
          <Spinner className="h-3.5 w-3.5" />
          Loading {typeName} credentials…
        </span>
      ) : list.isError ? (
        <p className="text-[11.5px] leading-relaxed text-danger-3">{apiMessage(list.error)}</p>
      ) : options.length === 0 ? (
        <div className="flex flex-col items-start gap-2">
          <p className="text-[11.5px] leading-relaxed text-ink-3">
            This node needs a <span className="font-mono text-ink-2">{typeName}</span>{" "}
            credential and the workspace has none yet.
          </p>
          <button
            type="button"
            onClick={openNew}
            className="inline-flex items-center gap-1.5 rounded-lg bg-accent/16 px-2.5 py-1.5 text-[11.5px] font-semibold text-accent-3 hover:bg-accent/26 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
          >
            <Icons.plus size={12} />
            Create a {typeName} credential
          </button>
        </div>
      ) : (
        <>
          <Field>
            <Select
              value={selected?.id ?? ""}
              aria-label={`${typeName} credential`}
              className={clsx("py-2.5 text-[12.5px]", !selected && "border-warning!")}
              onChange={(e) => {
                const next = e.target.value;
                if (next === "__new") {
                  openNew();
                  return;
                }
                onChange(next === "" ? null : next);
              }}
            >
              <option value="">Select a credential…</option>
              {options.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
              <option value="__new">+ New credential…</option>
            </Select>
          </Field>

          {selected && status && (
            <div className="flex items-center gap-2.5">
              <TypeIconChip icon={type?.icon ?? "lock"} size={24} />
              <CredentialStatusLine status={status} />
              <div className="grow" />
              <button
                type="button"
                onClick={() => setModal({ mode: "edit", credential: selected })}
                className="shrink-0 rounded text-[11px] font-semibold text-accent-2 hover:text-accent-3 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
              >
                Edit
              </button>
            </div>
          )}

          {!selected && !danglingId && (
            <p className="text-[11px] text-warning">
              No credential selected — this node will fail when it runs.
            </p>
          )}
        </>
      )}

      {danglingId && (
        <p className="text-[11px] leading-relaxed text-danger-3">
          The credential this node points at is gone. Pick another one.
        </p>
      )}

      <CredentialModal
        open={modal !== null}
        target={modal ?? { mode: "create", type: typeId }}
        onClose={() => setModal(null)}
        // The provider's consent screen is opened in a new tab from the editor:
        // navigating this one away would drop an unsaved graph.
        connectIn="newTab"
        onSaved={(credential) => {
          qc.invalidateQueries({ queryKey: ["credentials"] });
          // Selecting the credential that was just created is the whole reason
          // the modal is reachable from here.
          if (credential.type === typeId) onChange(credential.id);
        }}
      />
    </div>
  );
}

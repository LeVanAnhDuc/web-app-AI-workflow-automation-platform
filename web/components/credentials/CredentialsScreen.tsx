"use client";

import clsx from "clsx";
import { useEffect, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { credentials as credentialsApi } from "@/lib/api";
import { Button, Card, EmptyState, ErrorNotice, Select, Spinner } from "@/components/ui";
import { Icons } from "@/components/ui/icons";
import { PageHeader, Topbar } from "@/components/layout/Topbar";
import { useDebouncedValue } from "@/components/workflows/useDebouncedValue";
import type { CredentialSummary } from "@/lib/types";
import { CredentialModal, apiMessage, type CredentialModalTarget } from "./CredentialModal";
import { CredentialRow, SkeletonRows, TableHead } from "./CredentialTable";
import { useCredentialTypes } from "./useCredentialTypes";

const ALL = "all";

export function CredentialsScreen() {
  const qc = useQueryClient();
  const router = useRouter();
  const searchParams = useSearchParams();
  const { types, byType, isPending: typesPending, isError: typesError } = useCredentialTypes();

  const [q, setQ] = useState("");
  const [typeFilter, setTypeFilter] = useState<string>(ALL);
  const [modal, setModal] = useState<CredentialModalTarget | null>(null);
  const [notice, setNotice] = useState<{ tone: "success" | "danger"; text: string } | null>(null);
  const debouncedQ = useDebouncedValue(q, 200);

  const listKey = useMemo(() => ["credentials", { type: typeFilter }] as const, [typeFilter]);
  const list = useQuery({
    queryKey: listKey,
    queryFn: () => credentialsApi.list(typeFilter === ALL ? undefined : typeFilter),
  });

  const rows = useMemo(() => {
    const term = debouncedQ.trim().toLowerCase();
    const all = list.data?.credentials ?? [];
    if (!term) return all;
    return all.filter(
      (c) =>
        c.name.toLowerCase().includes(term) ||
        (byType.get(c.type)?.name ?? c.type).toLowerCase().includes(term),
    );
  }, [list.data, debouncedQ, byType]);

  // The OAuth callback lands back here. The banner is the only place the user
  // learns whether the round trip through the provider actually worked.
  const connectedId = searchParams.get("connected");
  const oauthError = searchParams.get("error");
  useEffect(() => {
    if (!connectedId && !oauthError) return;
    if (oauthError) {
      setNotice({ tone: "danger", text: `Could not connect the credential — ${oauthError}` });
    } else {
      setNotice({ tone: "success", text: "Connected. The credential is ready to use." });
      qc.invalidateQueries({ queryKey: ["credentials"] });
    }
    // Drop the parameters so a refresh does not replay a stale banner, and so
    // no provider message stays in the address bar.
    router.replace("/credentials");
  }, [connectedId, oauthError, qc, router]);

  const remove = useMutation({
    mutationFn: (id: string) => credentialsApi.remove(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["credentials"] }),
  });

  const test = useMutation({
    mutationFn: (credential: CredentialSummary) => credentialsApi.test(credential.id),
    onSuccess: (result, credential) => {
      setNotice(
        result.ok
          ? { tone: "success", text: `${credential.name} works — the provider answered.` }
          : {
              tone: "danger",
              text: `${credential.name} failed the test${
                result.status ? ` (HTTP ${result.status})` : ""
              }${result.message ? ` — ${result.message}` : "."}`,
            },
      );
    },
    onError: (err, credential) => {
      setNotice({ tone: "danger", text: `${credential.name}: ${apiMessage(err)}` });
    },
  });

  const connect = useMutation({
    mutationFn: (credential: CredentialSummary) =>
      credentialsApi.oauthStart(credential.id, "/credentials"),
    onSuccess: ({ url }) => window.location.assign(url),
    onError: (err) => setNotice({ tone: "danger", text: apiMessage(err) }),
  });

  const filtered = debouncedQ.trim() !== "" || typeFilter !== ALL;
  const newButton = (
    <Button variant="primary" onClick={() => setModal({ mode: "create" })}>
      <Icons.plus size={14} />
      New credential
    </Button>
  );

  return (
    <div className="flex min-h-screen flex-col bg-surface">
      <Topbar />

      <div className="flex min-h-0 grow flex-col gap-[22px] px-10 py-8">
        <PageHeader
          title="Credentials"
          subtitle={
            list.data
              ? `${rows.length} credential${rows.length === 1 ? "" : "s"} · encrypted at rest, never shown again`
              : undefined
          }
          action={newButton}
        />

        <div className="flex flex-wrap items-center gap-3">
          <label className="flex w-[280px] items-center gap-2.5 rounded-[10px] border border-line-strong bg-panel px-3.5 py-2.5 focus-within:border-accent focus-within:ring-4 focus-within:ring-accent/14">
            <Icons.search size={14} className="shrink-0 text-ink-4" />
            <input
              type="search"
              value={q}
              onChange={(e) => setQ(e.target.value)}
              placeholder="Filter by name"
              aria-label="Filter credentials by name"
              className="min-w-0 grow bg-transparent text-[13px] text-ink outline-none placeholder:text-ink-4"
            />
          </label>

          <div className="w-[220px]">
            <Select
              value={typeFilter}
              aria-label="Filter by credential type"
              className="py-2.5 text-[12.5px]"
              onChange={(e) => setTypeFilter(e.target.value)}
            >
              <option value={ALL}>All types</option>
              {types.map((t) => (
                <option key={t.type} value={t.type}>
                  {t.name}
                </option>
              ))}
            </Select>
          </div>

          <div className="grow" />

          {(list.isFetching || test.isPending) && (
            <span className="flex items-center gap-2 text-[12px] text-ink-4">
              <Spinner className="h-3.5 w-3.5" />
              {test.isPending ? "Testing…" : "Refreshing…"}
            </span>
          )}
        </div>

        {notice && <Notice tone={notice.tone} onDismiss={() => setNotice(null)} text={notice.text} />}

        {typesError && (
          <ErrorNotice>
            Could not load the credential types, so statuses may be incomplete.
          </ErrorNotice>
        )}
        {remove.isError && (
          <ErrorNotice>Could not delete the credential — {apiMessage(remove.error)}</ErrorNotice>
        )}

        {list.isError ? (
          <Card className="p-5">
            <div className="flex flex-col items-start gap-3">
              <ErrorNotice>{apiMessage(list.error)}</ErrorNotice>
              <Button onClick={() => list.refetch()} disabled={list.isFetching}>
                <Icons.retry size={14} />
                {list.isFetching ? "Retrying…" : "Try again"}
              </Button>
            </div>
          </Card>
        ) : (
          <Card>
            <TableHead />

            {list.isPending || typesPending ? (
              <SkeletonRows />
            ) : rows.length === 0 ? (
              <EmptyState
                title={filtered ? "No credentials match these filters" : "No credentials yet"}
                description={
                  filtered
                    ? "Try a different name, or switch the type filter back to All types."
                    : "A credential is an encrypted set of values a node authenticates with. Nothing here is ever shown again once saved."
                }
                action={
                  <div className="flex items-center gap-2">
                    {filtered && (
                      <Button
                        onClick={() => {
                          setQ("");
                          setTypeFilter(ALL);
                        }}
                      >
                        Clear filters
                      </Button>
                    )}
                    {newButton}
                  </div>
                }
              />
            ) : (
              rows.map((c) => (
                <CredentialRow
                  key={c.id}
                  credential={c}
                  type={byType.get(c.type)}
                  testing={test.isPending && test.variables?.id === c.id}
                  deleting={remove.isPending && remove.variables === c.id}
                  onEdit={() => setModal({ mode: "edit", credential: c })}
                  onTest={() => test.mutate(c)}
                  onConnect={() => connect.mutate(c)}
                  onDelete={() => remove.mutate(c.id)}
                />
              ))
            )}
          </Card>
        )}
      </div>

      <CredentialModal
        open={modal !== null}
        target={modal ?? { mode: "create" }}
        onClose={() => setModal(null)}
        onSaved={() => qc.invalidateQueries({ queryKey: ["credentials"] })}
      />
    </div>
  );
}

function Notice({
  tone,
  text,
  onDismiss,
}: {
  tone: "success" | "danger";
  text: string;
  onDismiss: () => void;
}) {
  return (
    <div
      role="status"
      className={clsx(
        "flex items-start gap-2.5 rounded-[10px] border px-3.5 py-3",
        tone === "success"
          ? "border-success/25 bg-success/8"
          : "border-danger/25 bg-danger/8",
      )}
    >
      <Icons.info
        size={15}
        className={clsx("mt-px shrink-0", tone === "success" ? "text-success" : "text-danger")}
      />
      <span
        className={clsx(
          "grow text-xs leading-snug",
          tone === "success" ? "text-ink-2" : "text-danger-3",
        )}
      >
        {text}
      </span>
      <button
        type="button"
        onClick={onDismiss}
        aria-label="Dismiss this message"
        className="shrink-0 rounded text-ink-4 hover:text-ink-2 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
      >
        <Icons.close size={14} />
      </button>
    </div>
  );
}

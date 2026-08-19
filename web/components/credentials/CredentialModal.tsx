"use client";

import { useEffect, useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ApiError, credentials as credentialsApi } from "@/lib/api";
import {
  Button,
  ErrorNotice,
  Field,
  IconButton,
  Input,
  Modal,
  Spinner,
} from "@/components/ui";
import { Icons } from "@/components/ui/icons";
import type { CredentialSummary, CredentialType } from "@/lib/types";
import {
  CredentialFields,
  initialValues,
  submittedFields,
  unfilledRequired,
  type FieldValues,
} from "./CredentialForm";
import { CredentialStatusPill } from "./CredentialStatusPill";
import { CredentialTypeStep } from "./CredentialTypeStep";
import { RedirectUriBox } from "./RedirectUriBox";
import { TypeIconChip } from "./TypeIcon";
import { credentialStatus } from "./status";
import { useCredentialTypes } from "./useCredentialTypes";

/** What the modal was opened for. A pre-set `type` is how the node drawer says
 *  "the user needs a Slack credential, do not make them find it again". */
export type CredentialModalTarget =
  | { mode: "create"; type?: string }
  | { mode: "edit"; credential: CredentialSummary };

type Step = "type" | "form" | "connect";

export function CredentialModal({
  open,
  target,
  onClose,
  onSaved,
  /** The editor opens the provider in a new tab: navigating away from an
   *  unsaved graph would lose it. */
  connectIn = "sameTab",
  returnTo,
}: {
  open: boolean;
  target: CredentialModalTarget;
  onClose: () => void;
  onSaved?: (credential: CredentialSummary) => void;
  connectIn?: "sameTab" | "newTab";
  returnTo?: string;
}) {
  const qc = useQueryClient();
  const { types, byType, redirectUri, isPending, isError, error } = useCredentialTypes();

  const editing = target.mode === "edit" ? target.credential : undefined;

  const [step, setStep] = useState<Step>("form");
  const [typeId, setTypeId] = useState<string>("");
  const [name, setName] = useState("");
  const [values, setValues] = useState<FieldValues>({});
  const [touched, setTouched] = useState<ReadonlySet<string>>(new Set());
  const [showErrors, setShowErrors] = useState(false);
  const [saved, setSaved] = useState<CredentialSummary | undefined>(undefined);

  const type: CredentialType | undefined = byType.get(typeId);
  const isOAuth = type?.auth === "oauth2";

  // Everything resets from the target, so reopening the modal can never show
  // the previous credential's half-typed values. The effect keys on the
  // target's identity rather than the object: callers build the target inline,
  // and a fresh object every render would wipe the form as the user types.
  const targetKey =
    target.mode === "edit" ? `edit:${target.credential.id}` : `create:${target.type ?? ""}`;
  const targetRef = useRef(target);
  targetRef.current = target;

  useEffect(() => {
    if (!open) return;
    const current = targetRef.current;
    const startType =
      current.mode === "edit" ? current.credential.type : (current.type ?? "");
    setTypeId(startType);
    setName(current.mode === "edit" ? current.credential.name : "");
    setValues({});
    setTouched(new Set());
    setShowErrors(false);
    setSaved(current.mode === "edit" ? current.credential : undefined);
    setStep(current.mode === "create" && !startType ? "type" : "form");
  }, [open, targetKey]);

  // The field defaults live in the type descriptor, which may still be loading
  // when the modal opens with a pre-set type.
  useEffect(() => {
    if (!open || !type) return;
    setValues((current) =>
      Object.keys(current).length > 0 ? current : initialValues(type, Boolean(editing)),
    );
    setName((current) => (current === "" && !editing ? type.name : current));
  }, [open, type, editing]);

  const save = useMutation({
    mutationFn: async (): Promise<CredentialSummary> => {
      if (!type) throw new Error("Pick a credential type first.");
      const fields = submittedFields(type, values, touched, editing);
      const response = editing
        ? await credentialsApi.patch(editing.id, { name: name.trim(), fields })
        : await credentialsApi.create({ type: type.type, name: name.trim(), fields });
      return response.credential;
    },
    onSuccess: (credential) => {
      qc.invalidateQueries({ queryKey: ["credentials"] });
      setSaved(credential);
      setTouched(new Set());
      setValues(type ? initialValues(type, true) : {});
      onSaved?.(credential);
      // An OAuth credential is not usable yet: the client id and secret only
      // make the authorisation possible. Say so instead of closing on a
      // half-done job.
      if (isOAuth) setStep("connect");
      else onClose();
    },
  });

  const connect = useMutation({
    mutationFn: async () => {
      const id = saved?.id ?? editing?.id;
      if (!id) throw new Error("Save the credential before connecting it.");
      return credentialsApi.oauthStart(id, returnTo ?? "/credentials");
    },
    onSuccess: ({ url }) => {
      // The provider's consent screen is a full page load, never an iframe.
      if (connectIn === "newTab") window.open(url, "_blank", "noopener,noreferrer");
      else window.location.assign(url);
    },
  });

  const missing = type ? unfilledRequired(type, values, editing) : [];
  const canSave = Boolean(type) && name.trim() !== "" && missing.length === 0;

  const submit = () => {
    setShowErrors(true);
    if (!canSave) return;
    save.mutate();
  };

  const titleId = "credential-modal-title";

  return (
    <Modal open={open} onClose={onClose} width={620} labelledBy={titleId}>
      <div className="flex max-h-[70vh] flex-col">
        {step === "type" ? (
          <>
            <h2 id={titleId} className="sr-only">
              Choose a credential type
            </h2>
            <CredentialTypeStep
              types={types}
              isPending={isPending}
              error={isError ? apiMessage(error) : undefined}
              onPick={(picked) => {
                setTypeId(picked.type);
                setValues(initialValues(picked, false));
                setName(picked.name);
                setStep("form");
              }}
            />
            <div className="flex items-center justify-end border-t border-line px-5 py-3.5">
              <Button variant="ghost" onClick={onClose}>
                Cancel
              </Button>
            </div>
          </>
        ) : (
          <>
            <header className="flex items-center gap-3 border-b border-line-strong px-5 py-4">
              <TypeIconChip icon={type?.icon ?? "lock"} size={34} tone="accent" />
              <div className="flex min-w-0 grow flex-col gap-0.5">
                <h2 id={titleId} className="truncate text-[14.5px] font-bold text-ink">
                  {editing ? `Edit ${editing.name}` : `New ${type?.name ?? "credential"}`}
                </h2>
                <span className="truncate font-mono text-[10.5px] text-ink-4">
                  {type?.type ?? typeId}
                </span>
              </div>
              {target.mode === "create" && (
                <Button variant="ghost" onClick={() => setStep("type")}>
                  Change type
                </Button>
              )}
              <IconButton aria-label="Close" onClick={onClose}>
                <Icons.close size={16} />
              </IconButton>
            </header>

            <div className="flex min-h-0 grow flex-col gap-[18px] overflow-y-auto px-5 py-5">
              {step === "connect" ? (
                <ConnectStep
                  credential={saved}
                  type={type}
                  redirectUri={redirectUri}
                  connecting={connect.isPending}
                  error={connect.isError ? apiMessage(connect.error) : undefined}
                  onConnect={() => connect.mutate()}
                />
              ) : (
                <>
                  {!type && !isPending && (
                    <ErrorNotice>
                      This build no longer registers the credential type{" "}
                      <span className="font-mono">{typeId}</span>. Nodes using it will fail.
                    </ErrorNotice>
                  )}
                  {isPending && (
                    <div className="flex items-center gap-2.5 text-[13px] text-ink-4">
                      <Spinner className="h-4 w-4" />
                      Loading the credential type…
                    </div>
                  )}

                  {editing && (
                    <p className="rounded-[10px] border border-line bg-white/3 px-3.5 py-3 text-xs leading-relaxed text-ink-3">
                      Stored values are never sent back to the browser. Leave a field
                      untouched to keep what is stored; type in it to replace it.
                    </p>
                  )}

                  <Field label="Name">
                    <Input
                      value={name}
                      aria-label="Credential name"
                      placeholder="Production Slack"
                      className={showErrors && name.trim() === "" ? "border-danger!" : undefined}
                      onChange={(e) => setName(e.target.value)}
                    />
                  </Field>

                  {type && isOAuth && <RedirectUriBox uri={redirectUri} />}

                  {type && (
                    <CredentialFields
                      type={type}
                      values={values}
                      credential={editing}
                      showErrors={showErrors}
                      onChange={(field, value) => {
                        setValues((v) => ({ ...v, [field]: value }));
                        setTouched((t) => new Set(t).add(field));
                      }}
                    />
                  )}

                  {save.isError && <ErrorNotice>{apiMessage(save.error)}</ErrorNotice>}
                  {connect.isError && <ErrorNotice>{apiMessage(connect.error)}</ErrorNotice>}
                </>
              )}
            </div>

            <footer className="flex items-center gap-2 border-t border-line px-5 py-3.5">
              {step === "form" && showErrors && missing.length > 0 && (
                <span className="text-[11.5px] text-danger">
                  {missing.map((f) => f.label || f.name).join(", ")} still needed.
                </span>
              )}
              <div className="grow" />
              <Button variant="ghost" onClick={onClose}>
                {step === "connect" ? "Done" : "Cancel"}
              </Button>
              {step === "form" && (
                <Button variant="primary" onClick={submit} disabled={save.isPending}>
                  {save.isPending && <Spinner className="h-3.5 w-3.5" />}
                  {save.isPending
                    ? "Saving…"
                    : editing
                      ? "Save changes"
                      : isOAuth
                        ? "Save and continue"
                        : "Create credential"}
                </Button>
              )}
              {step === "connect" && (
                <Button
                  variant="primary"
                  onClick={() => connect.mutate()}
                  disabled={connect.isPending}
                >
                  {connect.isPending && <Spinner className="h-3.5 w-3.5" />}
                  {saved?.connected ? "Reconnect" : "Connect"}
                </Button>
              )}
            </footer>
          </>
        )}
      </div>
    </Modal>
  );
}

function ConnectStep({
  credential,
  type,
  redirectUri,
  connecting,
  error,
  onConnect,
}: {
  credential?: CredentialSummary;
  type?: CredentialType;
  redirectUri: string;
  connecting: boolean;
  error?: string;
  onConnect: () => void;
}) {
  const status = credential && type ? credentialStatus(credential, type) : undefined;
  const scopes = type?.oauth2?.scopes ?? [];

  return (
    <>
      <div className="flex items-start gap-2.5 rounded-[10px] border border-success/25 bg-success/8 px-3.5 py-3">
        <Icons.info size={14} className="mt-px shrink-0 text-success" />
        <p className="text-xs leading-relaxed text-ink-2">
          Saved. One step left: authorise it with the provider. Nothing runs against
          this credential until that finishes.
        </p>
      </div>

      {status && (
        <div className="flex items-center gap-2.5">
          <span className="text-xs font-semibold text-ink-3">Status</span>
          <CredentialStatusPill status={status} />
          {status.detail && <span className="text-[11px] text-ink-4">{status.detail}</span>}
        </div>
      )}

      <RedirectUriBox uri={redirectUri} />

      {scopes.length > 0 && (
        <div className="flex flex-col gap-1.5">
          <span className="text-xs font-semibold text-ink-3">Scopes requested</span>
          <ul className="flex flex-wrap gap-1.5">
            {scopes.map((s) => (
              <li
                key={s}
                className="rounded-md bg-white/6 px-2 py-[3px] font-mono text-[10.5px] text-ink-3"
              >
                {s}
              </li>
            ))}
          </ul>
        </div>
      )}

      {error && <ErrorNotice>{error}</ErrorNotice>}

      <Button variant="accentSoft" onClick={onConnect} disabled={connecting} className="w-full">
        {connecting ? <Spinner className="h-3.5 w-3.5" /> : <Icons.lock size={13} />}
        {connecting ? "Opening the provider…" : `Connect ${type?.name ?? "account"}`}
      </Button>
    </>
  );
}

export function apiMessage(err: unknown): string {
  if (err instanceof ApiError) return err.message;
  if (err instanceof Error) return err.message;
  return "Could not reach the API. Check that the server is running.";
}

import { relativeTime } from "@/lib/format";
import type { CredentialField, CredentialSummary, CredentialType } from "@/lib/types";

/* ---------------------------------------------------------------------------
   Whether a credential will actually work, derived from what the API is willing
   to tell us about it. The list screen, the row menu and the node drawer all
   read this one function: a credential that says "Connected" in the table and
   nothing in the drawer would be worse than no status at all.

   Deliberately pure and clock-injectable so every branch below is testable.
   --------------------------------------------------------------------------- */

/** Access tokens dying inside this window are worth warning about: a schedule
 *  that runs overnight would otherwise fail with no notice. */
export const EXPIRY_WARNING_MS = 24 * 60 * 60 * 1000;

export type CredentialStatusKind =
  | "ready"
  | "connected"
  | "expiringSoon"
  | "expired"
  | "notConnected"
  | "incomplete"
  | "unknownType";

export type StatusTone = "success" | "warning" | "danger" | "neutral";

export interface CredentialStatus {
  kind: CredentialStatusKind;
  label: string;
  tone: StatusTone;
  /** Second line: the relative time, or what is missing. */
  detail?: string;
  /** True when running (or re-running) the OAuth dance is the fix. */
  canConnect: boolean;
  connectLabel?: "Connect" | "Reconnect";
  /** Labels of the required fields with no stored value. */
  missing: string[];
}

/** Fields that must hold a value. `notice` carries none, and `hidden` is filled
 *  from its default by the form, so neither can be "missing" by user error. */
export function requiredFields(type: CredentialType): CredentialField[] {
  return (type.fields ?? []).filter(
    (f) => f.required === true && f.type !== "notice" && f.type !== "hidden",
  );
}

/** Required fields of the type that the stored credential has no value for. */
export function missingFields(
  credential: CredentialSummary,
  type: CredentialType,
): CredentialField[] {
  const set = new Set(credential.setFields ?? []);
  return requiredFields(type).filter((f) => !set.has(f.name));
}

/**
 * The one status rule.
 *
 * `type` is undefined when the credential names a type this build no longer
 * registers — an honest "Unknown type" beats guessing.
 *
 * "Incomplete" is checked before the OAuth states on purpose: a credential with
 * no client secret cannot be connected at all, so telling the user to press
 * Connect would send them into a provider error page.
 */
export function credentialStatus(
  credential: CredentialSummary,
  type: CredentialType | undefined,
  now: Date = new Date(),
): CredentialStatus {
  if (!type) {
    return {
      kind: "unknownType",
      label: "Unknown type",
      tone: "warning",
      detail: credential.type,
      canConnect: false,
      missing: [],
    };
  }

  const missing = missingFields(credential, type);
  if (missing.length > 0) {
    return {
      kind: "incomplete",
      label: "Incomplete",
      tone: "warning",
      detail: `Missing ${missing.map((f) => f.label || f.name).join(", ")}`,
      canConnect: false,
      missing: missing.map((f) => f.label || f.name),
    };
  }

  if (type.auth !== "oauth2") {
    return { kind: "ready", label: "Ready", tone: "success", canConnect: false, missing: [] };
  }

  if (!credential.connected) {
    return {
      kind: "notConnected",
      label: "Not connected",
      tone: "warning",
      detail: "Authorise it with the provider",
      canConnect: true,
      connectLabel: "Connect",
      missing: [],
    };
  }

  const expiresAt = parseTime(credential.expiresAt);
  if (expiresAt === undefined) {
    // A provider that returns no expiry is treated as long-lived; a failed call
    // surfaces the problem plainly enough.
    return {
      kind: "connected",
      label: "Connected",
      tone: "success",
      canConnect: false,
      connectLabel: "Reconnect",
      missing: [],
    };
  }

  const left = expiresAt - now.getTime();
  // An access token that expires exactly now is already unusable, so the
  // boundary belongs to "Expired".
  if (left <= 0) {
    return {
      kind: "expired",
      label: "Expired",
      tone: "danger",
      detail: `Expired ${relativeTime(credential.expiresAt, now)}`,
      canConnect: true,
      connectLabel: "Reconnect",
      missing: [],
    };
  }
  if (left <= EXPIRY_WARNING_MS) {
    return {
      kind: "expiringSoon",
      label: "Expires soon",
      tone: "warning",
      detail: `Expires ${inWords(left)}`,
      canConnect: true,
      connectLabel: "Reconnect",
      missing: [],
    };
  }
  return {
    kind: "connected",
    label: "Connected",
    tone: "success",
    canConnect: false,
    connectLabel: "Reconnect",
    missing: [],
  };
}

/** "in 45 seconds", "in 12 minutes", "in 23 hours". The mirror of
 *  `relativeTime`, which only looks backwards. */
export function inWords(ms: number): string {
  const seconds = Math.max(0, Math.round(ms / 1000));
  if (seconds < 60) return `in ${seconds} second${seconds === 1 ? "" : "s"}`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `in ${minutes} minute${minutes === 1 ? "" : "s"}`;
  const hours = Math.floor(minutes / 60);
  return `in ${hours} hour${hours === 1 ? "" : "s"}`;
}

/** "Used by 3 nodes", phrased so the delete confirmation reads as a sentence. */
export function usageSentence(count: number): string {
  if (count <= 0) return "No node references it.";
  return `Used by ${count} node${count === 1 ? "" : "s"}. Those node${
    count === 1 ? "" : "s"
  } will fail until they are pointed at another credential.`;
}

function parseTime(iso: string | undefined): number | undefined {
  if (!iso) return undefined;
  const ms = new Date(iso).getTime();
  return Number.isNaN(ms) ? undefined : ms;
}

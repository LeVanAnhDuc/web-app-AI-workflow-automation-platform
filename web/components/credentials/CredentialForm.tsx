"use client";

import clsx from "clsx";
import { Field, Input, Select } from "@/components/ui";
import { Icons } from "@/components/ui/icons";
import type { CredentialField, CredentialSummary, CredentialType } from "@/lib/types";

/* ---------------------------------------------------------------------------
   The credential form is generated from `type.fields`, exactly as the node
   drawer generates its parameters from a descriptor: a credential type added in
   Go must never mean editing this file, so every branch keys on `field.type`.

   The values are `string` throughout — that is what the API stores and what an
   encrypted blob returns.
   --------------------------------------------------------------------------- */

export type FieldValues = Record<string, string>;

/** Fields the user actually fills in. `notice` is prose and `hidden` is carried
 *  silently, so neither gets a control. */
export function visibleFields(type: CredentialType): CredentialField[] {
  return (type.fields ?? []).filter((f) => f.type !== "hidden");
}

/** Starting values: the type's defaults on create, empty on edit — the API
 *  never returns a stored value, not even a non-secret one. */
export function initialValues(type: CredentialType, editing: boolean): FieldValues {
  const values: FieldValues = {};
  for (const f of type.fields ?? []) {
    if (f.type === "notice") continue;
    values[f.name] = editing ? "" : (f.default ?? "");
  }
  return values;
}

/**
 * What goes on the wire.
 *
 * On create every field is sent. On edit only what the user touched is sent:
 * the PATCH merges, so an omitted key keeps its stored value — which is the
 * only safe thing to do when the browser was never told what that value is.
 * A secret that the user left alone is sent as "" because the API defines an
 * empty secret as "keep the stored one"; either would work, and sending it
 * makes the intent explicit.
 */
export function submittedFields(
  type: CredentialType,
  values: FieldValues,
  touched: ReadonlySet<string>,
  credential?: CredentialSummary,
): FieldValues {
  const out: FieldValues = {};
  const stored = new Set(credential?.setFields ?? []);

  for (const f of type.fields ?? []) {
    if (f.type === "notice") continue;

    // A hidden field is part of the credential's meaning (which header an API
    // key goes in, say), so it travels with every write.
    if (f.type === "hidden") {
      out[f.name] = f.default ?? "";
      continue;
    }
    if (!credential) {
      out[f.name] = values[f.name] ?? "";
      continue;
    }
    if (touched.has(f.name)) {
      out[f.name] = values[f.name] ?? "";
      continue;
    }
    if (f.secret && stored.has(f.name)) out[f.name] = "";
  }
  return out;
}

/** Required fields with nothing to submit — the reason Save stays disabled. */
export function unfilledRequired(
  type: CredentialType,
  values: FieldValues,
  credential?: CredentialSummary,
): CredentialField[] {
  const stored = new Set(credential?.setFields ?? []);
  return (type.fields ?? []).filter((f) => {
    if (!f.required || f.type === "notice" || f.type === "hidden") return false;
    // On edit an untouched field keeps whatever is stored, so "already set"
    // counts as filled.
    if (credential && stored.has(f.name)) return false;
    return (values[f.name] ?? "").trim() === "";
  });
}

export function CredentialFields({
  type,
  values,
  onChange,
  credential,
  showErrors,
}: {
  type: CredentialType;
  values: FieldValues;
  onChange: (name: string, value: string) => void;
  /** Present when editing: drives the "unchanged" affordance. */
  credential?: CredentialSummary;
  showErrors: boolean;
}) {
  const stored = new Set(credential?.setFields ?? []);

  return (
    <div className="flex flex-col gap-[18px]">
      {visibleFields(type).map((field) => (
        <CredentialFieldControl
          key={field.name}
          field={field}
          value={values[field.name] ?? ""}
          isStored={stored.has(field.name)}
          editing={Boolean(credential)}
          showErrors={showErrors}
          onChange={(next) => onChange(field.name, next)}
        />
      ))}
    </div>
  );
}

function CredentialFieldControl({
  field,
  value,
  isStored,
  editing,
  showErrors,
  onChange,
}: {
  field: CredentialField;
  value: string;
  isStored: boolean;
  editing: boolean;
  showErrors: boolean;
  onChange: (next: string) => void;
}) {
  if (field.type === "notice") {
    const text = field.description || field.default || field.label;
    if (!text) return null;
    return (
      <div className="flex items-start gap-2.5 rounded-[10px] border border-line bg-white/3 px-3.5 py-3">
        <Icons.info size={14} className="mt-px shrink-0 text-ink-4" />
        <p className="text-xs leading-relaxed text-ink-3">{text}</p>
      </div>
    );
  }

  // "Untouched keeps what is stored" only reads as a promise if the box says
  // so; an empty field with no explanation reads as data loss.
  const keepsStored = editing && isStored && value === "";
  const missing =
    showErrors && Boolean(field.required) && value.trim() === "" && !(editing && isStored);

  const hint = missing ? (
    <span className="font-sans text-[11px] text-danger">{field.label} is required.</span>
  ) : keepsStored ? (
    <span className="font-sans text-[11px] text-ink-4">
      {field.secret
        ? "Stored and never shown again. Leave it empty to keep it, or type a new value to replace it."
        : "Leave it empty to keep the stored value."}
      {field.description ? ` ${field.description}` : ""}
    </span>
  ) : field.description ? (
    <span className="font-sans text-[11px] text-ink-4">{field.description}</span>
  ) : undefined;

  const label = field.label || field.name;
  const invalid = missing ? "border-danger!" : undefined;
  const placeholder = keepsStored ? "unchanged" : field.placeholder;

  return (
    <Field
      label={label}
      hint={hint}
      action={
        isStored && editing ? (
          <span className="rounded-full bg-white/6 px-2 py-[2px] text-[10.5px] font-semibold text-ink-4">
            stored
          </span>
        ) : field.required ? (
          <span className="text-[10.5px] font-semibold text-ink-5">required</span>
        ) : undefined
      }
    >
      {field.type === "select" ? (
        <Select
          value={value}
          aria-label={label}
          className={clsx(invalid)}
          onChange={(e) => onChange(e.target.value)}
        >
          {!(field.options ?? []).some((o) => o.value === value) && (
            <option value={value}>{value || "Select…"}</option>
          )}
          {(field.options ?? []).map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </Select>
      ) : (
        <Input
          // A secret is masked in the DOM as well as on screen, and browsers are
          // told not to file it away as a saved password.
          type={field.type === "password" || field.secret ? "password" : "text"}
          autoComplete={field.type === "password" || field.secret ? "new-password" : "off"}
          spellCheck={false}
          value={value}
          aria-label={label}
          aria-required={field.required || undefined}
          placeholder={placeholder}
          className={clsx(invalid)}
          onChange={(e) => onChange(e.target.value)}
        />
      )}
    </Field>
  );
}

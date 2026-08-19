import { describe, expect, it } from "vitest";
import type { CredentialSummary, CredentialType } from "@/lib/types";
import { initialValues, submittedFields, unfilledRequired } from "./CredentialForm";

const apiKeyType: CredentialType = {
  type: "httpHeaderAuth",
  name: "API Key",
  icon: "lock",
  auth: "apiKey",
  fields: [
    { name: "notice", label: "", type: "notice", description: "Paste the key." },
    { name: "apiKey", label: "API Key", type: "password", required: true, secret: true },
    { name: "headerName", label: "Name", type: "string", default: "Authorization" },
    { name: "in", label: "", type: "hidden", default: "header" },
  ],
};

const stored: CredentialSummary = {
  id: "cred_1",
  workspaceId: "ws_1",
  type: "httpHeaderAuth",
  name: "Stripe",
  createdAt: "2026-08-01T00:00:00.000Z",
  updatedAt: "2026-08-01T00:00:00.000Z",
  setFields: ["apiKey", "headerName", "in"],
  connected: false,
  usedByCount: 2,
};

describe("initialValues", () => {
  it("seeds defaults on create and nothing on edit", () => {
    expect(initialValues(apiKeyType, false)).toEqual({
      apiKey: "",
      headerName: "Authorization",
      in: "header",
    });
    expect(initialValues(apiKeyType, true)).toEqual({ apiKey: "", headerName: "", in: "" });
  });
});

describe("submittedFields", () => {
  it("sends every field on create, hidden ones from their default", () => {
    const values = { apiKey: "sk-live", headerName: "X-API-Key", in: "" };
    expect(submittedFields(apiKeyType, values, new Set(["apiKey", "headerName"]))).toEqual({
      apiKey: "sk-live",
      headerName: "X-API-Key",
      in: "header",
    });
  });

  it("on edit sends an untouched stored secret as '' — the API keeps it", () => {
    const sent = submittedFields(apiKeyType, initialValues(apiKeyType, true), new Set(), stored);
    expect(sent.apiKey).toBe("");
    // An untouched non-secret is omitted entirely: the browser was never told
    // its value, so sending "" would blank it.
    expect("headerName" in sent).toBe(false);
    expect(sent.in).toBe("header");
  });

  it("on edit sends a typed secret as the new value", () => {
    const sent = submittedFields(
      apiKeyType,
      { apiKey: "sk-new", headerName: "", in: "" },
      new Set(["apiKey"]),
      stored,
    );
    expect(sent.apiKey).toBe("sk-new");
  });

  it("on edit lets a touched non-secret be cleared", () => {
    const sent = submittedFields(
      apiKeyType,
      { apiKey: "", headerName: "", in: "" },
      new Set(["headerName"]),
      stored,
    );
    expect(sent.headerName).toBe("");
  });
});

describe("unfilledRequired", () => {
  it("blocks a create with an empty required field", () => {
    expect(
      unfilledRequired(apiKeyType, { apiKey: "   ", headerName: "", in: "header" }).map(
        (f) => f.name,
      ),
    ).toEqual(["apiKey"]);
  });

  it("counts an already stored required field as filled on edit", () => {
    expect(unfilledRequired(apiKeyType, { apiKey: "" }, stored)).toEqual([]);
  });
});

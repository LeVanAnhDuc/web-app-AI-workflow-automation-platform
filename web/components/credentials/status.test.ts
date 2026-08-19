import { describe, expect, it } from "vitest";
import type { CredentialSummary, CredentialType } from "@/lib/types";
import { credentialStatus, inWords, missingFields, usageSentence } from "./status";

const NOW = new Date("2026-08-19T12:00:00.000Z");
const HOUR = 60 * 60 * 1000;

function type(patch: Partial<CredentialType> = {}): CredentialType {
  return {
    type: "httpHeaderAuth",
    name: "API Key",
    icon: "lock",
    auth: "apiKey",
    fields: [
      { name: "apiKey", label: "API Key", type: "password", required: true, secret: true },
      { name: "in", label: "", type: "hidden", default: "header" },
      { name: "headerName", label: "Name", type: "string" },
    ],
    ...patch,
  };
}

function oauthType(patch: Partial<CredentialType> = {}): CredentialType {
  return type({
    type: "slackOAuth2",
    name: "Slack",
    auth: "oauth2",
    fields: [
      { name: "notice", label: "", type: "notice", description: "Register an app.", required: true },
      { name: "clientId", label: "Client ID", type: "string", required: true },
      {
        name: "clientSecret",
        label: "Client Secret",
        type: "password",
        required: true,
        secret: true,
      },
    ],
    oauth2: { authorizeUrl: "https://example.test/a", tokenUrl: "https://example.test/t" },
    ...patch,
  });
}

function credential(patch: Partial<CredentialSummary> = {}): CredentialSummary {
  return {
    id: "cred_1",
    workspaceId: "ws_1",
    type: "httpHeaderAuth",
    name: "Stripe key",
    createdAt: "2026-08-01T00:00:00.000Z",
    updatedAt: "2026-08-01T00:00:00.000Z",
    setFields: ["apiKey", "in"],
    connected: false,
    usedByCount: 0,
    ...patch,
  };
}

describe("credentialStatus", () => {
  it("reports Ready for a non-OAuth credential with every required field set", () => {
    const status = credentialStatus(credential(), type(), NOW);
    expect(status.kind).toBe("ready");
    expect(status.label).toBe("Ready");
    expect(status.tone).toBe("success");
    expect(status.canConnect).toBe(false);
  });

  it("reports Incomplete when a required field has no stored value", () => {
    const status = credentialStatus(credential({ setFields: ["in"] }), type(), NOW);
    expect(status.kind).toBe("incomplete");
    expect(status.tone).toBe("warning");
    expect(status.missing).toEqual(["API Key"]);
    expect(status.detail).toContain("API Key");
  });

  it("ignores notice and hidden fields when deciding completeness", () => {
    // The Slack notice is marked required in this fixture on purpose: it holds
    // no value, so it must never make a credential look Incomplete.
    const status = credentialStatus(
      credential({ type: "slackOAuth2", setFields: ["clientId", "clientSecret"] }),
      oauthType(),
      NOW,
    );
    expect(status.kind).toBe("notConnected");
  });

  it("reports Not connected for an OAuth credential that never completed the dance", () => {
    const status = credentialStatus(
      credential({ type: "slackOAuth2", setFields: ["clientId", "clientSecret"] }),
      oauthType(),
      NOW,
    );
    expect(status.label).toBe("Not connected");
    expect(status.tone).toBe("warning");
    expect(status.canConnect).toBe(true);
    expect(status.connectLabel).toBe("Connect");
  });

  it("prefers Incomplete over Not connected — Connect cannot work without a client secret", () => {
    const status = credentialStatus(
      credential({ type: "slackOAuth2", setFields: ["clientId"] }),
      oauthType(),
      NOW,
    );
    expect(status.kind).toBe("incomplete");
    expect(status.canConnect).toBe(false);
  });

  it("reports Expired when the token died in the past", () => {
    const status = credentialStatus(
      credential({
        type: "slackOAuth2",
        setFields: ["clientId", "clientSecret"],
        connected: true,
        expiresAt: new Date(NOW.getTime() - 3 * HOUR).toISOString(),
      }),
      oauthType(),
      NOW,
    );
    expect(status.kind).toBe("expired");
    expect(status.tone).toBe("danger");
    expect(status.detail).toBe("Expired 3 hours ago");
    expect(status.connectLabel).toBe("Reconnect");
  });

  it("treats a token expiring exactly now as expired", () => {
    const status = credentialStatus(
      credential({
        type: "slackOAuth2",
        setFields: ["clientId", "clientSecret"],
        connected: true,
        expiresAt: NOW.toISOString(),
      }),
      oauthType(),
      NOW,
    );
    expect(status.kind).toBe("expired");
  });

  it("reports Expires soon 23 hours out, with the relative time", () => {
    const status = credentialStatus(
      credential({
        type: "slackOAuth2",
        setFields: ["clientId", "clientSecret"],
        connected: true,
        expiresAt: new Date(NOW.getTime() + 23 * HOUR).toISOString(),
      }),
      oauthType(),
      NOW,
    );
    expect(status.kind).toBe("expiringSoon");
    expect(status.tone).toBe("warning");
    expect(status.detail).toBe("Expires in 23 hours");
    expect(status.canConnect).toBe(true);
  });

  it("treats exactly 24 hours as still inside the warning window", () => {
    const status = credentialStatus(
      credential({
        type: "slackOAuth2",
        setFields: ["clientId", "clientSecret"],
        connected: true,
        expiresAt: new Date(NOW.getTime() + 24 * HOUR).toISOString(),
      }),
      oauthType(),
      NOW,
    );
    expect(status.kind).toBe("expiringSoon");
  });

  it("reports Connected 25 hours out", () => {
    const status = credentialStatus(
      credential({
        type: "slackOAuth2",
        setFields: ["clientId", "clientSecret"],
        connected: true,
        expiresAt: new Date(NOW.getTime() + 25 * HOUR).toISOString(),
      }),
      oauthType(),
      NOW,
    );
    expect(status.kind).toBe("connected");
    expect(status.tone).toBe("success");
    expect(status.canConnect).toBe(false);
  });

  it("reports Connected when the provider gave no expiry", () => {
    const status = credentialStatus(
      credential({ type: "slackOAuth2", setFields: ["clientId", "clientSecret"], connected: true }),
      oauthType(),
      NOW,
    );
    expect(status.kind).toBe("connected");
  });

  it("ignores an unparseable expiry rather than calling a live token expired", () => {
    const status = credentialStatus(
      credential({
        type: "slackOAuth2",
        setFields: ["clientId", "clientSecret"],
        connected: true,
        expiresAt: "not a date",
      }),
      oauthType(),
      NOW,
    );
    expect(status.kind).toBe("connected");
  });

  it("reports Unknown type when the build no longer registers the type", () => {
    const status = credentialStatus(credential({ type: "gone" }), undefined, NOW);
    expect(status.kind).toBe("unknownType");
    expect(status.detail).toBe("gone");
    expect(status.canConnect).toBe(false);
  });

  it("tolerates a null setFields array from the API", () => {
    const bare = { ...credential(), setFields: undefined as unknown as string[] };
    expect(credentialStatus(bare, type(), NOW).kind).toBe("incomplete");
  });
});

describe("missingFields", () => {
  it("returns the field descriptors, not just names, so the UI can label them", () => {
    expect(missingFields(credential({ setFields: [] }), type()).map((f) => f.name)).toEqual([
      "apiKey",
    ]);
  });
});

describe("inWords", () => {
  it("counts seconds, minutes and hours", () => {
    expect(inWords(1_000)).toBe("in 1 second");
    expect(inWords(45_000)).toBe("in 45 seconds");
    expect(inWords(60_000)).toBe("in 1 minute");
    expect(inWords(90 * 60_000)).toBe("in 1 hour");
    expect(inWords(23 * HOUR)).toBe("in 23 hours");
  });
});

describe("usageSentence", () => {
  it("states the count and the consequence", () => {
    expect(usageSentence(0)).toBe("No node references it.");
    expect(usageSentence(1)).toContain("Used by 1 node.");
    expect(usageSentence(3)).toBe(
      "Used by 3 nodes. Those nodes will fail until they are pointed at another credential.",
    );
  });
});

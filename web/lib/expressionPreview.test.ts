import { describe, expect, it } from "vitest";
import { hasExpression, previewExpression, type PreviewEnv } from "./expressionPreview";

const env: PreviewEnv = {
  json: { email: "ha.nguyen@acme.vn", company: { name: "Acme", size: 42 } },
  nodes: {
    "Fetch profile": { id: "cb_1029", tags: ["lead", "vn"] },
  },
  itemIndex: 3,
  now: new Date("2026-08-18T09:30:00.000Z"),
};

describe("previewExpression", () => {
  it("passes a plain string through untouched", () => {
    expect(previewExpression("https://api.example.com/v2", env)).toBe(
      "https://api.example.com/v2",
    );
  });

  it("resolves a dotted path on $json", () => {
    expect(previewExpression("email={{ $json.email }}", env)).toBe(
      "email=ha.nguyen@acme.vn",
    );
  });

  it("renders a missing path as undefined instead of throwing", () => {
    expect(previewExpression("{{ $json.missing.deeper }}", env)).toBe("undefined");
  });

  it("resolves two expressions in one string", () => {
    expect(previewExpression("{{ $json.email }} at {{ $json.company.name }}", env)).toBe(
      "ha.nguyen@acme.vn at Acme",
    );
  });

  it("resolves a named node reference", () => {
    expect(previewExpression('{{ $node["Fetch profile"].id }}', env)).toBe("cb_1029");
  });

  it("returns the input unchanged when a {{ is never closed", () => {
    expect(previewExpression("https://x/?e={{ $json.email", env)).toBe(
      "https://x/?e={{ $json.email",
    );
  });

  it("treats an explicit .json segment as a no-op, matching the server scope", () => {
    expect(previewExpression('{{ $node["Fetch profile"].json.id }}', env)).toBe("cb_1029");
  });

  it("resolves $itemIndex, $now and bracket segments", () => {
    expect(previewExpression("{{ $itemIndex }}", env)).toBe("3");
    expect(previewExpression("{{ $now }}", env)).toBe("2026-08-18T09:30:00.000Z");
    expect(previewExpression('{{ $node["Fetch profile"].tags[1] }}', env)).toBe("vn");
  });

  it("declines arithmetic rather than guessing, because the server owns it", () => {
    expect(previewExpression("{{ $json.company.size + 1 }}", env)).toBe("undefined");
  });

  it("serialises an object value so the preview line still says something", () => {
    expect(previewExpression("{{ $json.company }}", env)).toBe(
      '{"name":"Acme","size":42}',
    );
  });
});

describe("hasExpression", () => {
  it("needs a complete run", () => {
    expect(hasExpression("{{ $json.a }}")).toBe(true);
    expect(hasExpression("plain")).toBe(false);
    expect(hasExpression("{{ $json.a")).toBe(false);
  });
});

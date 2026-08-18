import { describe, expect, it } from "vitest";
import {
  absoluteTime,
  clockTime,
  duration,
  errorSubline,
  middleTruncate,
  percent,
  relativeTime,
  triggerLabel,
} from "./format";

const now = new Date("2026-08-18T14:02:11.400Z");
const ago = (ms: number) => new Date(now.getTime() - ms).toISOString();

describe("relativeTime", () => {
  it("reads the way the tables show it", () => {
    expect(relativeTime(ago(2_000), now)).toBe("just now");
    expect(relativeTime(ago(12_000), now)).toBe("12 seconds ago");
    expect(relativeTime(ago(2 * 60_000), now)).toBe("2 minutes ago");
    expect(relativeTime(ago(60_000), now)).toBe("1 minute ago");
    expect(relativeTime(ago(6 * 3_600_000), now)).toBe("6 hours ago");
    expect(relativeTime(ago(3_600_000), now)).toBe("1 hour ago");
    expect(relativeTime(ago(26 * 3_600_000), now)).toBe("yesterday");
    expect(relativeTime(ago(3 * 86_400_000), now)).toBe("3 days ago");
    expect(relativeTime(ago(9 * 86_400_000), now)).toBe("1 week ago");
    expect(relativeTime(ago(20 * 86_400_000), now)).toBe("2 weeks ago");
    expect(relativeTime(ago(70 * 86_400_000), now)).toBe("2 months ago");
  });

  it("shows an em dash rather than Invalid Date", () => {
    expect(relativeTime(undefined, now)).toBe("—");
    expect(relativeTime("not a date", now)).toBe("—");
  });
});

describe("duration", () => {
  it("switches unit at a second and at a minute", () => {
    expect(duration(2)).toBe("2ms");
    expect(duration(318)).toBe("318ms");
    expect(duration(1420)).toBe("1.42s");
    expect(duration(12_800)).toBe("12.8s");
    expect(duration(124_000)).toBe("2m 04s");
  });

  it("has no value to show while a node is still running", () => {
    expect(duration(undefined)).toBe("—");
    expect(duration(null)).toBe("—");
  });
});

describe("clockTime", () => {
  it("keeps tenths, which is how the log distinguishes fast nodes", () => {
    const d = new Date(2026, 7, 18, 14, 2, 11, 480);
    expect(clockTime(d.toISOString())).toBe("14:02:11.4");
  });

  it("pads single digits", () => {
    const d = new Date(2026, 7, 18, 9, 5, 3, 0);
    expect(clockTime(d.toISOString())).toBe("09:05:03.0");
  });

  it("survives bad input", () => {
    expect(clockTime(undefined)).toBe("—");
    expect(clockTime("nope")).toBe("—");
  });
});

describe("absoluteTime", () => {
  it("renders the execution header's stamp", () => {
    const d = new Date(2026, 7, 18, 14, 31, 1);
    expect(absoluteTime(d.toISOString())).toMatch(/18 Aug 2026/);
  });

  it("survives bad input", () => {
    expect(absoluteTime("nope")).toBe("—");
  });
});

describe("percent", () => {
  it("rounds to a whole percent", () => {
    expect(percent(0.98)).toBe("98%");
    expect(percent(1)).toBe("100%");
    expect(percent(0.755)).toBe("76%");
    expect(percent(0)).toBe("0%");
  });

  it("distinguishes no data from zero", () => {
    expect(percent(undefined)).toBe("—");
    expect(percent(null)).toBe("—");
  });
});

describe("triggerLabel", () => {
  it("capitalises the trigger and appends its detail", () => {
    expect(triggerLabel("webhook")).toBe("Webhook");
    expect(triggerLabel("schedule", "09:00")).toBe("Schedule 09:00");
    expect(triggerLabel("manual")).toBe("Manual");
    expect(triggerLabel("something-new")).toBe("Manual");
  });
});

describe("errorSubline", () => {
  it("names the node so the row says where it broke", () => {
    expect(
      errorSubline({ code: "http_error", message: "HTTP 429", node_name: "Classify ticket" }),
    ).toBe("HTTP 429 at “Classify ticket”");
  });

  it("omits the location when the error has none", () => {
    expect(errorSubline({ code: "internal_error", message: "boom" })).toBe("boom");
    expect(errorSubline(undefined)).toBe("");
  });
});

describe("middleTruncate", () => {
  it("leaves a short value alone", () => {
    expect(middleTruncate("https://api.acme.vn/v1", 64)).toBe("https://api.acme.vn/v1");
  });

  it("keeps both ends of a long URL readable", () => {
    const long = "https://api.clearbit.com/v2/people/find?email=ha.nguyen@acme.vn&extra=1";
    const out = middleTruncate(long, 30);
    expect(out.length).toBeLessThanOrEqual(30);
    expect(out.startsWith("https://api.cl")).toBe(true);
    expect(out.endsWith("extra=1")).toBe(true);
    expect(out).toContain("…");
  });
});

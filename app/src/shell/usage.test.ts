import { describe, expect, it } from "vitest";
import { contextGauge, formatReset, formatTokens, level, limitsByHarness, readUsage, tokensByTask, type SessionTokens } from "./usage";

const now = 1_764_240_000;

describe("usage model", () => {
  it("reads missing usage fields as empty", () => {
    expect(readUsage(undefined)).toEqual({ limits: [], today: [] });
    expect(readUsage({ limits: null, today: "x" })).toEqual({ limits: [], today: [] });
  });

  it("gauges context against the window when known", () => {
    expect(contextGauge({ contextTokens: 92_000, contextWindow: 200_000 })).toEqual({ used: 92_000, window: 200_000, percent: 46 });
    expect(contextGauge({ contextTokens: 92_000 })).toEqual({ used: 92_000, window: 0, percent: null });
    expect(contextGauge({ contextTokens: 300_000, contextWindow: 200_000 })?.percent).toBe(100);
    expect(contextGauge({ contextTokens: 0, contextWindow: 200_000 })).toBeNull();
    expect(contextGauge(undefined)).toBeNull();
  });

  it("groups windows per harness and zeroes the ones already reset", () => {
    const got = limitsByHarness(
      [
        { harness: "claude", window: "5h", usedPercent: 42, resetsAt: now + 3600, at: "2026-11-27T10:00:00Z" },
        { harness: "claude", window: "weekly", usedPercent: 18, resetsAt: now + 86400, at: "2026-11-27T10:05:00Z" },
        { harness: "codex", window: "5h", usedPercent: 97, resetsAt: now - 1 },
      ],
      now,
    );
    expect(got).toEqual([
      {
        harness: "claude",
        at: "2026-11-27T10:05:00Z",
        windows: [
          { window: "5h", percent: 42, resetsAt: now + 3600 },
          { window: "weekly", percent: 18, resetsAt: now + 86400 },
        ],
      },
      { harness: "codex", windows: [{ window: "5h", percent: 0, resetsAt: undefined }] },
    ]);
  });

  it("sums today's tokens per task, busiest first", () => {
    const row = (session: string, task: string, input: number): SessionTokens => ({
      session,
      task,
      title: session,
      harness: "claude",
      input,
      output: 10,
      cacheRead: 100,
      cacheWrite: 0,
    });
    const got = tokensByTask([row("session:a", "task:x", 5), row("session:b", "task:y", 1000), row("session:c", "task:x", 2000)]);
    expect(got.map((t) => [t.task, t.total])).toEqual([
      ["task:x", 2225],
      ["task:y", 1110],
    ]);
    expect(got[0].sessions.map((s) => s.session)).toEqual(["session:c", "session:a"]);
  });

  it("formats tokens, reset times and levels", () => {
    expect([950, 12_340, 123_400, 2_100_000, 1000].map(formatTokens)).toEqual(["950", "12.3k", "123k", "2.1M", "1k"]);
    expect(formatReset(now + 2 * 3600 + 14 * 60, now)).toBe("2h 14m");
    expect(formatReset(now + 3 * 86400 + 4 * 3600, now)).toBe("3d 4h");
    expect(formatReset(now + 30, now)).toBe("1m");
    expect(formatReset(now + 3 * 86400, now)).toBe("3d");
    expect([10, 70, 95].map(level)).toEqual(["ok", "wait", "bad"]);
  });
});

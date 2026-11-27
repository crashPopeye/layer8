import type { Addr } from "@/lib/types";

export interface Limit {
  harness: string;
  window: string;
  usedPercent: number;
  resetsAt?: number;
  at?: string;
}

export interface SessionTokens {
  session: Addr;
  task: Addr;
  title: string;
  harness: string;
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
}

export interface Window {
  window: string;
  percent: number;
  resetsAt?: number;
}

export interface HarnessLimits {
  harness: string;
  windows: Window[];
  at?: string;
}

export interface TaskTokens {
  task: Addr;
  total: number;
  sessions: (SessionTokens & { total: number })[];
}

export type Level = "ok" | "wait" | "bad";

/** Reads the usage node's fields, missing ones as empty. */
export function readUsage(fields?: Record<string, unknown>): { limits: Limit[]; today: SessionTokens[] } {
  return {
    limits: Array.isArray(fields?.limits) ? (fields.limits as Limit[]) : [],
    today: Array.isArray(fields?.today) ? (fields.today as SessionTokens[]) : [],
  };
}

/** A session's context use, with a percentage when its context window is known; null before its first turn. */
export function contextGauge(fields?: Record<string, unknown>): { used: number; window: number; percent: number | null } | null {
  const used = Number(fields?.contextTokens ?? 0);
  const window = Number(fields?.contextWindow ?? 0);
  if (!(used > 0)) return null;
  return { used, window, percent: window > 0 ? clamp((used / window) * 100) : null };
}

/** Groups windows by harness; a window whose reset time has passed counts as unused. */
export function limitsByHarness(limits: Limit[], nowSec: number): HarnessLimits[] {
  const groups = new Map<string, HarnessLimits>();
  for (const l of limits) {
    const g = groups.get(l.harness) ?? { harness: l.harness, windows: [] };
    const expired = l.resetsAt != null && l.resetsAt > 0 && l.resetsAt <= nowSec;
    g.windows.push({ window: l.window, percent: expired ? 0 : clamp(l.usedPercent), resetsAt: expired ? undefined : l.resetsAt });
    if (l.at && (!g.at || l.at > g.at)) g.at = l.at;
    groups.set(l.harness, g);
  }
  return [...groups.values()];
}

/** Today's tokens per task, busiest task and session first. */
export function tokensByTask(today: SessionTokens[]): TaskTokens[] {
  const groups = new Map<Addr, TaskTokens>();
  for (const s of today) {
    const total = s.input + s.output + s.cacheRead + s.cacheWrite;
    const g = groups.get(s.task) ?? { task: s.task, total: 0, sessions: [] };
    g.total += total;
    g.sessions.push({ ...s, total });
    groups.set(s.task, g);
  }
  const tasks = [...groups.values()].sort((a, b) => b.total - a.total);
  tasks.forEach((t) => t.sessions.sort((a, b) => b.total - a.total));
  return tasks;
}

/** Colours a percentage: calm below 70%, warning below 90%, alarm above. */
export function level(percent: number): Level {
  return percent >= 90 ? "bad" : percent >= 70 ? "wait" : "ok";
}

/** Formats a token count as 950, 12.3k or 2.1M. */
export function formatTokens(n: number): string {
  if (n < 1000) return String(Math.round(n));
  if (n < 1_000_000) return `${trim(n / 1000)}k`;
  return `${trim(n / 1_000_000)}M`;
}

/** Formats how long until a Unix-seconds reset time, e.g. "2h 14m" or "3d 4h". */
export function formatReset(resetsAt: number, nowSec: number): string {
  const minutes = Math.max(0, Math.ceil((resetsAt - nowSec) / 60));
  const d = Math.floor(minutes / 1440);
  const h = Math.floor((minutes % 1440) / 60);
  const m = minutes % 60;
  if (d > 0) return h > 0 ? `${d}d ${h}h` : `${d}d`;
  if (h > 0) return m > 0 ? `${h}h ${m}m` : `${h}h`;
  return `${m}m`;
}

function trim(n: number): string {
  return n >= 100 ? String(Math.round(n)) : n.toFixed(1).replace(/\.0$/, "");
}

function clamp(percent: number): number {
  return Math.min(100, Math.max(0, percent));
}

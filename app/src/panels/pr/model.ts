import type { Node } from "@/lib/types";

/** A task's `pr` field, as `internal/github` stores it. */
export interface PR {
  number: number;
  url: string;
  state: "open" | "closed" | "merged";
  draft: boolean;
  base: string;
  mergeable: "mergeable" | "conflicting" | "unknown";
  reviewDecision: "" | "approved" | "changes_requested" | "review_required";
  checksState: "" | "success" | "failure" | "pending";
  checks: Check[];
  unresolvedThreads: number;
  threads: Thread[];
}

export interface Check {
  name: string;
  state: "pending" | "success" | "failure" | "skipped";
  url: string;
  logTail?: string;
}

export interface Thread {
  path: string;
  line: number;
  author: string;
  body: string;
  url: string;
  outdated: boolean;
  replies: number;
}

/** A colour from the theme tokens. */
export type Tone = "ok" | "bad" | "wait" | "primary" | "muted";

export interface Label {
  text: string;
  tone: Tone;
}

/** Returns the task's pull request, or null when it has none. */
export function prOf(node: Node | null): PR | null {
  return (node?.fields?.pr as PR | null | undefined) ?? null;
}

const order: Record<Check["state"], number> = { failure: 0, pending: 1, success: 2, skipped: 3 };

/** What the PR panel shows: labelled state, review and mergeability, and checks with failures first. */
export function prView(pr: PR) {
  const state: Label =
    pr.state === "merged"
      ? { text: "Merged", tone: "primary" }
      : pr.state === "closed"
        ? { text: "Closed", tone: "muted" }
        : pr.draft
          ? { text: "Draft", tone: "muted" }
          : { text: "Open", tone: "ok" };
  const review: Label | null = {
    approved: { text: "Approved", tone: "ok" } as Label,
    changes_requested: { text: "Changes requested", tone: "bad" } as Label,
    review_required: { text: "Review required", tone: "wait" } as Label,
    "": null,
  }[pr.reviewDecision];
  const merge: Label | null =
    pr.state !== "open"
      ? null
      : {
          mergeable: { text: "No conflicts", tone: "ok" } as Label,
          conflicting: { text: "Conflicts", tone: "bad" } as Label,
          unknown: { text: "Mergeability unknown", tone: "muted" } as Label,
        }[pr.mergeable];
  const checks = [...pr.checks].sort((a, b) => order[a.state] - order[b.state]);
  const count = (s: Check["state"]) => pr.checks.filter((c) => c.state === s).length;
  const summary = pr.checks.length
    ? (["failure", "pending", "success"] as const)
        .filter((s) => count(s))
        .map((s) => `${count(s)} ${{ failure: "failing", pending: "running", success: "passed" }[s]}`)
        .join(" · ")
    : "No checks";
  return { state, review, merge, checks, summary, canFixCI: pr.checksState === "failure", canAddress: pr.unresolvedThreads > 0 || pr.reviewDecision === "changes_requested" };
}

/** The text colour class of a tone. */
export const toneText: Record<Tone, string> = {
  ok: "text-ok",
  bad: "text-bad",
  wait: "text-wait",
  primary: "text-primary",
  muted: "text-muted-foreground",
};

/** The tone of a check state. */
export const checkTone: Record<Check["state"], Tone> = { failure: "bad", pending: "wait", success: "ok", skipped: "muted" };

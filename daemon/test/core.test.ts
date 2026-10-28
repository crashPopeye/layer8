import "./helpers.js";
import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { existsSync, mkdtempSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { loadConfig } from "../src/config.js";
import { Store } from "../src/db.js";
import { editorLaunch, terminalLaunch } from "../src/launch.js";
import { ticketMarkdown } from "../src/linear.js";
import { SessionManager, type QueryFn } from "../src/sessions.js";
import { addWorkspace, readArtifact, slugify, TaskService } from "../src/tasks.js";
import { sanitizeBranch } from "../src/git.js";
import { shellQuote } from "../src/tmux.js";
import { bus } from "../src/bus.js";

const sh = (cwd: string, ...args: string[]) => execFileSync("git", args, { cwd, encoding: "utf8" });

function makeRepo(): string {
  const origin = mkdtempSync(join(tmpdir(), "l8-origin-"));
  sh(origin, "init", "-q", "-b", "main");
  sh(origin, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init");
  const clone = mkdtempSync(join(tmpdir(), "l8-clone-"));
  sh(clone, "clone", "-q", origin, ".");
  return clone;
}

const cfg = () => ({ ...loadConfig(), useTmux: false });

test("slugs and branch names", () => {
  assert.equal(slugify("ENG-123"), "eng-123");
  assert.equal(slugify("  Fix: the Thing!  "), "fix-the-thing");
  assert.equal(sanitizeBranch("Alex/ENG-12 add  thing.."), "alex/eng-12-add-thing");
});

test("a plain folder is a workspace without git", async () => {
  const store = new Store(":memory:");
  const dir = mkdtempSync(join(tmpdir(), "l8-plain-"));
  const ws = await addWorkspace(store, dir);
  assert.equal(ws.isGit, false);
  const task = await new TaskService(store, cfg).create({ workspaceId: ws.id, title: "Write notes" });
  assert.equal(task.worktreePath, dir);
  assert.equal(task.branch, null);
});

test("a git task gets its own worktree and branch from origin/main", async () => {
  const store = new Store(":memory:");
  const repo = makeRepo();
  const ws = await addWorkspace(store, repo);
  assert.equal(ws.isGit, true);
  assert.equal(ws.defaultBranch, "main");
  const svc = new TaskService(store, cfg);
  const task = await svc.create({ workspaceId: ws.id, title: "Add login" });
  assert.equal(task.branch, "layer8/add-login");
  assert.equal(task.baseRef, "origin/main");
  assert.ok(existsSync(join(task.worktreePath, ".git")));
  assert.equal(sh(task.worktreePath, "branch", "--show-current").trim(), "layer8/add-login");

  // Same title again: new slug, and the existing branch is reused rather than recreated.
  const again = await new TaskService(store, cfg).create({ workspaceId: ws.id, title: "Add login" }).catch((e) => e);
  assert.ok(again instanceof Error, "branch already checked out in another worktree");

  await svc.archive(task.id, true);
  assert.equal(existsSync(task.worktreePath), false);
  assert.equal(store.getTask(task.id)?.status, "archived");
});

test("artifact names cannot escape the task folder", async () => {
  const store = new Store(":memory:");
  const ws = await addWorkspace(store, mkdtempSync(join(tmpdir(), "l8-a-")));
  const task = await new TaskService(store, cfg).create({ workspaceId: ws.id, title: "x" });
  assert.throws(() => readArtifact(task, "../../config.json"));
  assert.throws(() => readArtifact(task, ".hidden"));
});

test("terminal launch uses the user's app and shell", () => {
  const base = { ...loadConfig(), terminal: "ghostty" as const };
  const mac = terminalLaunch(base, "darwin", "/w", "tmux new-session -A -s l8-x");
  assert.equal(mac.file, "open");
  assert.deepEqual(mac.args.slice(0, 5), ["-na", "Ghostty", "--args", "--working-directory=/w", "-e"]);
  assert.equal(mac.args.at(-1), "tmux new-session -A -s l8-x");

  const iterm = terminalLaunch({ ...base, terminal: "iterm" }, "darwin", "/my dir", 'echo "hi"');
  assert.equal(iterm.file, "osascript");
  assert.match(iterm.args[1], /write text "cd '\/my dir' && echo \\"hi\\""/);

  const custom = terminalLaunch(
    { ...base, terminal: "custom", terminalCommand: "alacritty --working-directory {cwd} -e sh -c {cmd}" },
    "linux",
    "/a b",
    "tmux attach",
  );
  assert.deepEqual(custom, { file: "alacritty", args: ["--working-directory", "/a b", "-e", "sh", "-c", "tmux attach"] });

  assert.deepEqual(editorLaunch({ ...base, editor: "cursor" }, "/w"), { file: "cursor", args: ["-n", "/w"] });
});

test("shellQuote", () => {
  assert.equal(shellQuote("abc-1"), "abc-1");
  assert.equal(shellQuote("it's"), `'it'\\''s'`);
});

test("ticket markdown includes description and comments in order", () => {
  const md = ticketMarkdown({
    id: "1",
    identifier: "ENG-1",
    title: "Thing",
    url: "https://linear.app/x/issue/ENG-1",
    branchName: "alex/eng-1-thing",
    priority: 2,
    state: { name: "Todo", type: "unstarted", color: "#fff" },
    team: "ENG",
    updatedAt: "2026-01-01",
    description: "Do it",
    assignee: "Alex",
    labels: ["bug"],
    parent: null,
    comments: [{ author: "Sam", body: "first", createdAt: "2026-01-02T00:00:00Z" }],
    attachments: [],
  });
  assert.match(md, /^# ENG-1: Thing/);
  assert.match(md, /## Description\n\nDo it/);
  assert.match(md, /### Sam \(2026-01-02\)\n\nfirst/);
});

// A scripted stand-in for the Agent SDK.
function fakeQuery(script: (opts: any) => AsyncGenerator<any>): { fn: QueryFn; calls: any[] } {
  const calls: any[] = [];
  const fn: QueryFn = ({ prompt, options }) => {
    calls.push({ prompt, options });
    return script(options);
  };
  return { fn, calls };
}

const tick = () => new Promise((r) => setTimeout(r, 10));
async function until(cond: () => boolean) {
  for (let i = 0; i < 200 && !cond(); i++) await tick();
  assert.ok(cond(), "condition not reached");
}

test("session runs, asks for approval, records events, then resumes", async () => {
  const store = new Store(":memory:");
  const ws = await addWorkspace(store, mkdtempSync(join(tmpdir(), "l8-s-")));
  const task = await new TaskService(store, cfg).create({ workspaceId: ws.id, title: "s" });

  const { fn, calls } = fakeQuery(async function* (options) {
    yield { type: "system", subtype: "init", session_id: "claude-abc" };
    const decision = await options.canUseTool("Bash", { command: "ls" }, { signal: new AbortController().signal });
    yield {
      type: "assistant",
      parent_tool_use_id: null,
      message: { content: [{ type: "text", text: `decision: ${decision.behavior}` }] },
    };
    yield { type: "result", subtype: "success", is_error: false, total_cost_usd: 0.5, duration_ms: 1, num_turns: 1 };
  });
  const mgr = new SessionManager(store, () => 2, fn);
  const s = mgr.create(task.id, "research", "research", "go");

  await until(() => store.pendingApprovals().length === 1);
  assert.equal(store.getSession(s.id)?.status, "waiting_approval");
  mgr.resolveApproval(store.pendingApprovals()[0].id, true);

  await until(() => store.getSession(s.id)?.status === "idle");
  const session = store.getSession(s.id)!;
  assert.equal(session.claudeSessionId, "claude-abc");
  assert.equal(session.costUsd, 0.5);
  const texts = store.listEvents(s.id).filter((e) => e.type === "text").map((e) => e.data.text);
  assert.deepEqual(texts, ["decision: allow"]);
  assert.equal(calls[0].options.cwd, task.worktreePath);
  assert.deepEqual(calls[0].options.additionalDirectories, [task.artifactsPath]);
  assert.equal(calls[0].options.resume, undefined);

  mgr.send(s.id, "and now?");
  await until(() => calls.length === 2);
  assert.equal(calls[1].options.resume, "claude-abc");
  await until(() => store.pendingApprovals().length === 1);
  mgr.resolveApproval(store.pendingApprovals()[0].id, false, "no");
  await until(() => store.getSession(s.id)?.status === "idle");
  const last = store.listEvents(s.id).filter((e) => e.type === "text").at(-1);
  assert.equal(last?.data.text, "decision: deny");
});

test("sessions beyond the concurrency limit wait in the queue", async () => {
  const store = new Store(":memory:");
  const ws = await addWorkspace(store, mkdtempSync(join(tmpdir(), "l8-q-")));
  const task = await new TaskService(store, cfg).create({ workspaceId: ws.id, title: "q" });
  const gates: (() => void)[] = [];
  const { fn } = fakeQuery(async function* () {
    await new Promise<void>((r) => gates.push(r));
    yield { type: "result", subtype: "success", is_error: false, total_cost_usd: 0, duration_ms: 1, num_turns: 1 };
  });
  const mgr = new SessionManager(store, () => 1, fn);
  const a = mgr.create(task.id, "chat", "a", "a");
  const b = mgr.create(task.id, "chat", "b", "b");
  await until(() => gates.length === 1);
  assert.equal(store.getSession(a.id)?.status, "running");
  assert.equal(store.getSession(b.id)?.status, "queued");
  gates[0]();
  await until(() => gates.length === 2);
  assert.equal(store.getSession(b.id)?.status, "running");
  gates[1]();
  await until(() => store.getSession(b.id)?.status === "idle");
  bus.removeAllListeners();
});

test("stopping a session marks it interrupted, not idle", async () => {
  const store = new Store(":memory:");
  const ws = await addWorkspace(store, mkdtempSync(join(tmpdir(), "l8-i-")));
  const task = await new TaskService(store, cfg).create({ workspaceId: ws.id, title: "i" });
  let release!: () => void;
  const fn: QueryFn = () => {
    const gen = (async function* () {
      yield { type: "system", subtype: "init", session_id: "c1" } as any;
      await new Promise<void>((r) => (release = r));
    })();
    // Like the SDK: interrupt ends the stream cleanly instead of throwing.
    return Object.assign(gen, { interrupt: async () => release() });
  };
  const mgr = new SessionManager(store, () => 1, fn);
  const s = mgr.create(task.id, "chat", "c", "c");
  await until(() => store.getSession(s.id)?.claudeSessionId === "c1");
  await mgr.interrupt(s.id);
  await until(() => store.getSession(s.id)?.status === "interrupted");
});

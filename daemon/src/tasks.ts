import { randomUUID } from "node:crypto";
import { existsSync, mkdirSync, readdirSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { basename, join, resolve } from "node:path";
import { paths } from "./config.js";
import type { Store } from "./db.js";
import * as git from "./git.js";
import { Linear, ticketMarkdown } from "./linear.js";
import * as tmux from "./tmux.js";
import type { Artifact, Config, Task, Workspace } from "./types.js";

export function slugify(s: string): string {
  return s
    .toLowerCase()
    .normalize("NFKD")
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 48) || "task";
}

/** Any folder is a workspace. Git features switch on when it happens to be a repo. */
export async function addWorkspace(store: Store, path: string): Promise<Workspace> {
  const abs = resolve(path);
  if (!existsSync(abs) || !statSync(abs).isDirectory()) throw new Error(`not a folder: ${abs}`);
  const root = await git.repoRoot(abs);
  const isGit = root === abs;
  return store.insertWorkspace({
    name: basename(abs),
    path: abs,
    isGit,
    defaultBranch: isGit ? await git.defaultBranch(abs) : null,
  });
}

export type NewTask = { workspaceId: string; linearIssue?: string; title?: string };

export class TaskService {
  constructor(
    private readonly store: Store,
    private readonly config: () => Config,
  ) {}

  private uniqueSlug(base: string): string {
    let slug = slugify(base);
    for (let n = 2; this.store.slugTaken(slug); n++) slug = `${slugify(base)}-${n}`;
    return slug;
  }

  async create(input: NewTask): Promise<Task> {
    const ws = this.store.getWorkspace(input.workspaceId);
    if (!ws) throw new Error("unknown workspace");
    const cfg = this.config();

    let title = input.title?.trim() ?? "";
    let ticket: string | null = null;
    let linear: Task["linear"] = null;
    let branchHint: string;
    if (input.linearIssue) {
      if (!cfg.linearApiKey) throw new Error("Linear API key is not configured");
      const issue = await new Linear(cfg.linearApiKey).issue(input.linearIssue);
      linear = { id: issue.id, identifier: issue.identifier, url: issue.url, branchName: issue.branchName };
      title ||= issue.title;
      ticket = ticketMarkdown(issue);
      // Linear's branch name contains the identifier, which is what links PRs back to the issue.
      branchHint = issue.branchName;
    } else {
      if (!title) throw new Error("a task needs a Linear issue or a title");
      branchHint = `layer8/${slugify(title)}`;
    }

    const slug = this.uniqueSlug(linear ? linear.identifier : title);
    const artifactsPath = join(paths.tasks, slug);
    mkdirSync(artifactsPath, { recursive: true });
    if (ticket) writeFileSync(join(artifactsPath, "ticket.md"), ticket);

    let worktreePath = ws.path;
    let branch: string | null = null;
    let baseRef: string | null = null;
    if (ws.isGit) {
      const def = ws.defaultBranch ?? (await git.defaultBranch(ws.path));
      await git.fetchBranch(ws.path, def);
      baseRef = await git.baseRefFor(ws.path, def);
      branch = git.sanitizeBranch(branchHint);
      worktreePath = join(cfg.worktreeRoot, ws.name, slug);
      await git.addWorktree(ws.path, worktreePath, branch, baseRef);
    }

    let tmuxSession: string | null = null;
    if (cfg.useTmux && (await tmux.hasTmux())) {
      tmuxSession = tmux.tmuxName(slug);
      await tmux.ensureSession(tmuxSession, worktreePath, {
        LAYER8_TASK: slug,
        LAYER8_ARTIFACTS: artifactsPath,
        ...(linear ? { LAYER8_LINEAR: linear.identifier } : {}),
      });
    }

    return this.store.insertTask({
      id: randomUUID(),
      workspaceId: ws.id,
      slug,
      title,
      linear,
      branch,
      baseRef,
      worktreePath,
      artifactsPath,
      tmuxSession,
      status: "active",
    });
  }

  /** Archiving keeps the branch and artifacts; removing the worktree is opt-in. */
  async archive(taskId: string, removeWorktree: boolean): Promise<void> {
    const task = this.store.getTask(taskId);
    if (!task) throw new Error("unknown task");
    const ws = this.store.getWorkspace(task.workspaceId);
    if (task.tmuxSession) await tmux.killSession(task.tmuxSession);
    if (removeWorktree && ws?.isGit && task.worktreePath !== ws.path) {
      await git.removeWorktree(ws.path, task.worktreePath);
    }
    this.store.setTaskStatus(taskId, "archived");
  }
}

export function listArtifacts(task: Task): Artifact[] {
  if (!existsSync(task.artifactsPath)) return [];
  return readdirSync(task.artifactsPath, { withFileTypes: true })
    .filter((d) => d.isFile() && /\.(md|txt|ya?ml|json)$/i.test(d.name))
    .map((d) => {
      const st = statSync(join(task.artifactsPath, d.name));
      return { name: d.name, size: st.size, updatedAt: st.mtime.toISOString() };
    })
    .sort((a, b) => a.name.localeCompare(b.name));
}

export function readArtifact(task: Task, name: string): string {
  // Names come from the URL; only plain file names inside the artifacts folder are allowed.
  if (name !== basename(name) || name.startsWith(".")) throw new Error("invalid artifact name");
  return readFileSync(join(task.artifactsPath, name), "utf8");
}

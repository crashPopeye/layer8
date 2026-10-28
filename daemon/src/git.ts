import { mkdirSync } from "node:fs";
import { dirname } from "node:path";
import { ExecError, run } from "./exec.js";

const git = (cwd: string, ...args: string[]) => run("git", args, cwd);

export async function repoRoot(path: string): Promise<string | null> {
  try {
    return (await git(path, "rev-parse", "--show-toplevel")).trim();
  } catch {
    return null;
  }
}

async function refExists(repo: string, ref: string): Promise<boolean> {
  try {
    await git(repo, "rev-parse", "--verify", "--quiet", `${ref}^{commit}`);
    return true;
  } catch {
    return false;
  }
}

export async function hasRemote(repo: string, remote = "origin"): Promise<boolean> {
  const remotes = (await git(repo, "remote")).split("\n").map((s) => s.trim());
  return remotes.includes(remote);
}

export async function defaultBranch(repo: string): Promise<string> {
  try {
    const ref = (await git(repo, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")).trim();
    return ref.replace(/^origin\//, "");
  } catch {
    for (const candidate of ["main", "master", "trunk", "develop"]) {
      if (await refExists(repo, candidate)) return candidate;
    }
    return (await git(repo, "rev-parse", "--abbrev-ref", "HEAD")).trim();
  }
}

/** Best effort: a task should still start offline. */
export async function fetchBranch(repo: string, branch: string): Promise<boolean> {
  try {
    await git(repo, "fetch", "--quiet", "origin", branch);
    return true;
  } catch {
    return false;
  }
}

/** The ref new branches start from: the remote's copy of the default branch when we have one. */
export async function baseRefFor(repo: string, branch: string): Promise<string> {
  if ((await hasRemote(repo)) && (await refExists(repo, `origin/${branch}`))) return `origin/${branch}`;
  return branch;
}

/**
 * Create a worktree for `branch`. Reuses the branch when it already exists
 * (locally or on origin) so restarting a task never forks history.
 */
export async function addWorktree(repo: string, path: string, branch: string, baseRef: string): Promise<void> {
  mkdirSync(dirname(path), { recursive: true });
  if (await refExists(repo, `refs/heads/${branch}`)) {
    await git(repo, "worktree", "add", path, branch);
  } else if (await refExists(repo, `refs/remotes/origin/${branch}`)) {
    await git(repo, "worktree", "add", "--track", "-b", branch, path, `origin/${branch}`);
  } else {
    await git(repo, "worktree", "add", "--no-track", "-b", branch, path, baseRef);
  }
}

export async function removeWorktree(repo: string, path: string, force = false): Promise<void> {
  try {
    await git(repo, "worktree", "remove", ...(force ? ["--force"] : []), path);
  } catch (err) {
    if (err instanceof ExecError && /is not a working tree/.test(err.stderr)) return;
    throw err;
  }
}

/** Branch names from Linear are already safe; this guards free-form titles. */
export function sanitizeBranch(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9/._-]+/g, "-")
    .replace(/\.{2,}/g, ".")
    .replace(/\/{2,}/g, "/")
    .replace(/^[-./]+|[-./]+$/g, "")
    .slice(0, 100);
}

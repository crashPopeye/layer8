import { run, which } from "./exec.js";

/** tmux treats '.' and ':' as target separators. */
export function tmuxName(slug: string): string {
  return `l8-${slug}`.replace(/[.:\s]/g, "-");
}

export const hasTmux = () => which("tmux");

async function hasSession(name: string): Promise<boolean> {
  try {
    await run("tmux", ["has-session", "-t", `=${name}`]);
    return true;
  } catch {
    return false;
  }
}

/** Starts a detached session in `cwd` running the user's own shell, with task env set. */
export async function ensureSession(name: string, cwd: string, env: Record<string, string>): Promise<void> {
  if (await hasSession(name)) return;
  const envArgs = Object.entries(env).flatMap(([k, v]) => ["-e", `${k}=${v}`]);
  await run("tmux", ["new-session", "-d", "-s", name, "-c", cwd, ...envArgs]);
}

export async function newWindow(name: string, cwd: string, title: string, command: string): Promise<void> {
  await run("tmux", ["new-window", "-t", `=${name}:`, "-c", cwd, "-n", title, command]);
}

export async function killSession(name: string): Promise<void> {
  if (await hasSession(name)) await run("tmux", ["kill-session", "-t", `=${name}`]);
}

/** Shell command that attaches to (or recreates) the task's session. */
export function attachCommand(name: string, cwd: string): string {
  return `tmux new-session -A -s ${shellQuote(name)} -c ${shellQuote(cwd)}`;
}

export function shellQuote(s: string): string {
  if (/^[A-Za-z0-9_\-./=:@%+,]+$/.test(s)) return s;
  return `'${s.replace(/'/g, `'\\''`)}'`;
}

import { execFile, spawn } from "node:child_process";

export class ExecError extends Error {
  constructor(
    readonly cmd: string,
    readonly code: number | null,
    readonly stderr: string,
  ) {
    super(`${cmd} failed (${code}): ${stderr.trim()}`);
  }
}

export function run(file: string, args: string[], cwd?: string): Promise<string> {
  return new Promise((resolve, reject) => {
    execFile(file, args, { cwd, maxBuffer: 64 * 1024 * 1024 }, (err, stdout, stderr) => {
      if (err) {
        const code = typeof err.code === "number" ? err.code : null;
        reject(new ExecError(`${file} ${args.join(" ")}`, code, stderr || err.message));
      } else resolve(stdout);
    });
  });
}

/** Fire-and-forget launch of a GUI program that must outlive the daemon. */
export function launchDetached(file: string, args: string[], cwd?: string): void {
  const child = spawn(file, args, { cwd, detached: true, stdio: "ignore" });
  child.on("error", (err) => console.error(`[launch] ${file}: ${err.message}`));
  child.unref();
}

export async function which(bin: string): Promise<boolean> {
  try {
    await run("which", [bin]);
    return true;
  } catch {
    return false;
  }
}

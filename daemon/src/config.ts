import { mkdirSync, readFileSync, writeFileSync, existsSync, chmodSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { randomBytes } from "node:crypto";
import type { Config } from "./types.js";

export const home = process.env.LAYER8_HOME ?? join(homedir(), ".layer8");

export const paths = {
  home,
  config: join(home, "config.json"),
  token: join(home, "token"),
  db: join(home, "layer8.db"),
  tasks: join(home, "tasks"),
};

const defaults: Config = {
  port: 7878,
  linearApiKey: null,
  terminal: process.platform === "darwin" ? "terminal" : "ghostty",
  terminalCommand: null,
  editor: "code",
  editorCommand: null,
  useTmux: true,
  maxConcurrentSessions: 6,
  worktreeRoot: join(home, "worktrees"),
};

export function loadConfig(): Config {
  mkdirSync(home, { recursive: true });
  let fromFile: Partial<Config> = {};
  if (existsSync(paths.config)) fromFile = JSON.parse(readFileSync(paths.config, "utf8"));
  const cfg = { ...defaults, ...fromFile };
  if (!cfg.linearApiKey && process.env.LINEAR_API_KEY) cfg.linearApiKey = process.env.LINEAR_API_KEY;
  if (process.env.LAYER8_PORT) cfg.port = Number(process.env.LAYER8_PORT);
  return cfg;
}

export function saveConfig(cfg: Config): void {
  writeFileSync(paths.config, JSON.stringify(cfg, null, 2) + "\n", { mode: 0o600 });
}

/** The app reads this file (through the Tauri shell) to authenticate to the daemon. */
export function loadOrCreateToken(): string {
  mkdirSync(home, { recursive: true });
  if (existsSync(paths.token)) return readFileSync(paths.token, "utf8").trim();
  const token = randomBytes(24).toString("hex");
  writeFileSync(paths.token, token + "\n", { mode: 0o600 });
  chmodSync(paths.token, 0o600);
  return token;
}

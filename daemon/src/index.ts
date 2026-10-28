#!/usr/bin/env node
import { loadConfig, loadOrCreateToken, paths, saveConfig } from "./config.js";
import { Store } from "./db.js";
import { createApp } from "./server.js";
import { SessionManager } from "./sessions.js";
import { TaskService } from "./tasks.js";
import type { Config } from "./types.js";

let cfg = loadConfig();
const config = () => cfg;
const store = new Store(paths.db);
const sessions = new SessionManager(store, () => cfg.maxConcurrentSessions);
sessions.recover();

const server = createApp({
  store,
  sessions,
  tasks: new TaskService(store, config),
  config,
  saveConfig: (next: Config) => {
    saveConfig(next);
    cfg = loadConfig();
  },
  token: loadOrCreateToken(),
});

server.listen(cfg.port, "127.0.0.1", () => {
  console.log(`layer8d listening on http://127.0.0.1:${cfg.port} (home: ${paths.home})`);
});

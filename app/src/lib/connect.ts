import { invoke, isTauri } from "@tauri-apps/api/core";
import { httpClient, type DaemonInfo } from "./client";
import { mockClient } from "./mock";
import type { Client } from "./types";

/** Finds the daemon's url and token: from the Tauri shell, the page query, or Vite env. */
export async function daemonInfo(): Promise<DaemonInfo | null> {
  if (isTauri()) return invoke<DaemonInfo>("daemon_info").catch(() => null);
  const q = new URLSearchParams(location.search);
  const url = q.get("url") ?? import.meta.env.VITE_LAYER8_URL;
  const token = q.get("token") ?? import.meta.env.VITE_LAYER8_TOKEN;
  return url && token ? { url, token } : null;
}

/** Returns a daemon Client when one answers, and the in-memory mock otherwise. */
export async function connect(): Promise<Client> {
  const info = await daemonInfo();
  if (info) {
    const client = httpClient(info);
    const reachable = await client.getNode("app", 0).then(() => true, () => false);
    if (reachable) return client;
  }
  return mockClient();
}

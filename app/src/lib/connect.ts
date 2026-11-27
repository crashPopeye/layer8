import { invoke, isTauri } from "@tauri-apps/api/core";
import { httpClient, type DaemonInfo } from "./client";
import { demoSeed, mockClient } from "./mock";
import type { Client } from "./types";

/** Finds the daemon's url and token: from the Tauri shell, the page query, or Vite env. */
export async function daemonInfo(): Promise<DaemonInfo | null> {
  if (isTauri()) return invoke<DaemonInfo>("daemon_info").catch(() => null);
  const q = new URLSearchParams(location.search);
  const url = q.get("url") ?? import.meta.env.VITE_LAYER8_URL;
  const token = q.get("token") ?? import.meta.env.VITE_LAYER8_TOKEN;
  return url && token ? { url, token } : null;
}

/** Reports whether a layer8d answers `/api/version` at url. */
async function reachable(url: string): Promise<boolean> {
  return fetch(new URL("/api/version", url)).then((r) => r.ok, () => false);
}

/** Returns a daemon Client when one answers, and the in-memory mock with a demo tree otherwise. */
export async function connect(): Promise<Client> {
  const info = await daemonInfo();
  return info && (await reachable(info.url)) ? httpClient(info) : mockClient(demoSeed);
}

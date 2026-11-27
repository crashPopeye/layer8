import { useSyncExternalStore } from "react";
import { isTauri } from "@tauri-apps/api/core";
import { relaunch } from "@tauri-apps/plugin-process";
import { check, type Update } from "@tauri-apps/plugin-updater";
import { daemonInfo } from "./connect";

/** What the update prompt shows: an available update, or the outcome of a check the user asked for. */
export interface UpdateState {
  update?: Update;
  installing?: boolean;
  message?: string;
}

let state: UpdateState = {};
const subscribers = new Set<() => void>();

function set(next: UpdateState) {
  state = next;
  subscribers.forEach((s) => s());
}

/** Returns the current update state, re-rendering when it changes. */
export function useUpdate(): UpdateState {
  return useSyncExternalStore(
    (s) => {
      subscribers.add(s);
      return () => subscribers.delete(s);
    },
    () => state,
  );
}

/** Asks the daemon's update feed for a newer layer8; a manual check also reports "up to date" and failures. */
export async function checkForUpdate(manual = false) {
  if (!isTauri()) {
    if (manual) set({ message: "Updates install in the layer8 app, not in a browser." });
    return;
  }
  try {
    const info = await daemonInfo();
    const update = await check({ headers: info ? { Authorization: `Bearer ${info.token}` } : {} });
    if (update) set({ update });
    else if (manual) set({ message: "layer8 is up to date." });
  } catch (e) {
    if (manual) set({ message: `Checking for updates failed: ${e}` });
  }
}

/** Downloads and installs the available update, then restarts the app. */
export async function installUpdate() {
  const { update } = state;
  if (!update) return;
  set({ update, installing: true });
  try {
    await update.downloadAndInstall();
    await relaunch();
  } catch (e) {
    set({ message: `Installing the update failed: ${e}` });
  }
}

/** Hides the update prompt. */
export function dismissUpdate() {
  set({});
}

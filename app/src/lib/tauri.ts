import { invoke, isTauri } from "@tauri-apps/api/core";

/** Asks the user for a folder: the native picker in the app, a prompt in the browser. */
export async function pickFolder(): Promise<string | null> {
  if (isTauri()) return invoke<string | null>("pick_folder");
  return window.prompt("Path of the project folder");
}

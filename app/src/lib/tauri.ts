import { invoke, isTauri } from "@tauri-apps/api/core";

/** Asks the user for a folder: the native picker in the app, a prompt in the browser. */
export async function pickFolder(): Promise<string | null> {
  if (isTauri()) return invoke<string | null>("pick_folder");
  return window.prompt("Path of the project folder");
}

/** Opens an https link in the default browser. */
export function openUrl(url: string) {
  if (isTauri()) invoke("open_url", { url });
  else window.open(url, "_blank", "noopener");
}

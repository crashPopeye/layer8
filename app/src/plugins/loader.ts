import type { Client } from "@/lib/types";
import { createSandbox, type Sandbox } from "./sdk";

/** Evaluates ES module source and returns its exports. */
export type Importer = (source: string) => Promise<{ default?: unknown }>;

/** Imports module source through a blob URL. */
export const importSource: Importer = async (source) => {
  const url = URL.createObjectURL(new Blob([source], { type: "text/javascript" }));
  try {
    return await import(/* @vite-ignore */ url);
  } finally {
    URL.revokeObjectURL(url);
  }
};

/** A loaded plugin: what it registered, or why it failed with nothing registered. */
export interface Loaded {
  panels: string[];
  commands: string[];
  error?: string;
  dispose(): void;
}

/** Runs a plugin's default export with a fresh SDK, undoing whatever it registered when it throws. */
export async function loadPlugin(name: string, source: string, client: Client, importer: Importer = importSource): Promise<Loaded> {
  const box: Sandbox = createSandbox(name, client);
  try {
    const setup = (await importer(source)).default;
    if (typeof setup !== "function") throw new Error("the default export must be a function setup(sdk)");
    await setup(box.sdk);
    return box;
  } catch (e) {
    box.dispose();
    return { panels: [], commands: [], error: e instanceof Error ? e.message : String(e), dispose: () => {} };
  }
}

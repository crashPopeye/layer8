import { useEffect } from "react";
import { useClient, useNode } from "@/lib/react";
import type { Client } from "@/lib/types";
import { toast } from "@/shell/Toasts";
import { loadPlugin, type Importer, type Loaded } from "./loader";

/** One entry of the `plugins` field of ui:plugins. */
export interface PluginEntry {
  name: string;
  url: string;
  stamp: string;
  status: "pending" | "loaded" | "failed";
  error?: string;
  panels?: string[];
  commands?: string[];
}

const loaded = new Map<string, { stamp: string; plugin: Loaded }>();

/** Loads the plugins that are new or changed (all of them when force is set), drops removed ones, and reports each load to the daemon. */
export async function syncPlugins(client: Client, list: PluginEntry[], force = false, importer?: Importer) {
  for (const [name, l] of loaded) {
    if (list.some((p) => p.name === name)) continue;
    l.plugin.dispose();
    loaded.delete(name);
  }
  for (const p of list) {
    if (!force && loaded.get(p.name)?.stamp === p.stamp) continue;
    loaded.get(p.name)?.plugin.dispose();
    const plugin = await client.raw(p.url).then(
      (blob) => blob.text().then((source) => loadPlugin(p.name, source, client, importer)),
      (e: Error): Loaded => ({ panels: [], commands: [], error: e.message, dispose: () => {} }),
    );
    loaded.set(p.name, { stamp: p.stamp, plugin });
    const { panels, commands, error } = plugin;
    await client.doOps([{ target: "ui:plugins", action: "report", args: { plugin: p.name, error, panels, commands } }]);
    if (error) toast("error", `Plugin ${p.name}: ${error}`);
  }
}

let queue = Promise.resolve();

/** Keeps the window's plugins in step with the plugins folder: loads them at start, reloads one when its file changes, and all of them on `reload-plugins`. */
export function usePluginHost() {
  const client = useClient();
  const list = useNode("ui:plugins", 0).node?.fields?.plugins as PluginEntry[] | undefined;
  useEffect(() => {
    if (list) queue = queue.then(() => syncPlugins(client, list));
  }, [client, list]);
  useEffect(
    () =>
      client.subscribe((e) => {
        if (list && e.type === "ui" && e.data.verb === "reload-plugins") queue = queue.then(() => syncPlugins(client, list, true));
      }),
    [client, list],
  );
}

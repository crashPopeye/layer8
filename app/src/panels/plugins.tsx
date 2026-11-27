import { RotateCw } from "lucide-react";
import { ErrorCard } from "@/components/error-card";
import { Button } from "@/components/ui/button";
import { useAction, useNode } from "@/lib/react";
import { cn } from "@/lib/utils";
import type { PluginEntry } from "@/plugins/host";

const tone = { pending: "text-wait", loaded: "text-ok", failed: "text-bad" };

/** The plugins in the plugins folder, with what each registered or why it failed. */
export function PluginsPanel() {
  const { node, error } = useNode("ui:plugins", 0);
  const { run, busy } = useAction("ui:plugins");
  if (!node) return <p className="p-3 text-muted-foreground">{error ? "Plugins need the layer8 daemon." : "Loading…"}</p>;
  const list = (node.fields?.plugins ?? []) as PluginEntry[];
  return (
    <div className="grid h-full content-start gap-2 overflow-auto p-3 text-[12px]">
      <div className="flex items-center gap-2">
        <code className="flex-1 truncate font-mono text-muted-foreground">{String(node.fields?.dir)}</code>
        <Button size="xs" variant="ghost" disabled={!!busy} onClick={() => run("reload")}>
          <RotateCw />
          Reload
        </Button>
      </div>
      {list.length === 0 && <p className="text-muted-foreground">No plugins yet. Drop a .js file in the folder, or ask an agent to make a panel.</p>}
      {list.map((p) => (
        <div key={p.name} className="grid gap-1 rounded-md border p-2">
          <p className="flex items-center gap-2">
            <span className="font-medium">{p.name}</span>
            <span className={cn("text-[11px]", tone[p.status])}>{p.status}</span>
          </p>
          {!!p.panels?.length && <p className="text-muted-foreground">panels: {p.panels.join(", ")}</p>}
          {!!p.commands?.length && <p className="text-muted-foreground">commands: {p.commands.join(", ")}</p>}
          {p.error && <ErrorCard title={`${p.name} failed`} error={p.error} />}
        </div>
      ))}
    </div>
  );
}

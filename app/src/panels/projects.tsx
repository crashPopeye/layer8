import { FolderGit2, Plus } from "lucide-react";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { setFocus, useFocus } from "@/lib/focus";
import { useClient, useNode } from "@/lib/react";
import { pickFolder } from "@/lib/tauri";
import { cn } from "@/lib/utils";

/** Lists the projects under `app` and adds new ones from a folder. */
export function ProjectsPanel() {
  const client = useClient();
  const focus = useFocus();
  const { node, error } = useNode("app");
  const [message, setMessage] = useState<string | null>(null);

  async function add() {
    const path = await pickFolder();
    if (!path) return;
    const [result] = await client.doOps([{ target: "app", action: "add-project", args: { path } }]);
    setMessage(result.ok ? null : (result.message ?? "add-project failed"));
    if (result.ok) setFocus(result.addr);
  }

  return (
    <div className="flex h-full flex-col gap-1 overflow-auto p-2">
      <div className="flex items-center justify-between px-1.5 pb-1 text-[11px] text-muted-foreground">
        Projects
        <Button variant="ghost" size="icon-xs" title="Add project" onClick={add}>
          <Plus />
        </Button>
      </div>
      {error && <p className="px-2 text-bad">{error.message}</p>}
      {message && <p className="px-2 text-bad">{message}</p>}
      {node?.children.map((c) => (
        <button
          key={c.addr}
          onClick={() => setFocus(c.addr)}
          className={cn(
            "flex items-center gap-2 rounded-md px-2 py-1 text-left hover:bg-muted",
            focus === c.addr && "bg-muted",
          )}
        >
          <FolderGit2 className="size-3.5 text-muted-foreground" />
          {c.title}
        </button>
      ))}
      {node && node.children.length === 0 && (
        <p className="px-2 text-muted-foreground">No projects yet. Add a git repository with +.</p>
      )}
    </div>
  );
}

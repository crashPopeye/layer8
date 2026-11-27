import { ChevronDown, ChevronRight, File as FileIcon, Folder as FolderIcon, RefreshCw } from "lucide-react";
import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { taskOf, useFocus } from "@/lib/focus";
import { useClient } from "@/lib/react";
import type { Addr } from "@/lib/types";
import { cn } from "@/lib/utils";
import { Viewer } from "@/viewers/viewer";

interface Entry {
  name: string;
  dir: boolean;
  size: number;
}

const fileURL = (task: Addr, path: string) => `/api/file?task=${encodeURIComponent(task)}&path=${encodeURIComponent(path)}`;

/** Lists one folder of the worktree, opening sub-folders on click. */
function FolderList({
  task,
  path,
  depth,
  picked,
  onPick,
}: {
  task: Addr;
  path: string;
  depth: number;
  picked: string | null;
  onPick: (p: string) => void;
}) {
  const client = useClient();
  const [entries, setEntries] = useState<Entry[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [open, setOpen] = useState<Set<string>>(new Set());

  useEffect(() => {
    client.raw(fileURL(task, path)).then(
      async (b) => setEntries(JSON.parse(await b.text())),
      (e: Error) => setError(e.message),
    );
  }, [client, task, path]);

  if (error) return <p className="px-2 text-bad">{error}</p>;
  const sorted = (entries ?? []).filter((e) => e.name !== ".git").sort((a, b) => Number(b.dir) - Number(a.dir) || a.name.localeCompare(b.name));
  return sorted.map((e) => {
    const p = path === "." ? e.name : `${path}/${e.name}`;
    const isOpen = open.has(p);
    const Icon = e.dir ? (isOpen ? ChevronDown : ChevronRight) : FileIcon;
    const toggle = () =>
      setOpen((old) => {
        const next = new Set(old);
        if (!next.delete(p)) next.add(p);
        return next;
      });
    return (
      <div key={p}>
        <button
          onClick={e.dir ? toggle : () => onPick(p)}
          style={{ paddingLeft: 6 + depth * 12 }}
          className={cn(
            "flex w-full items-center gap-1.5 rounded-md py-0.5 pr-2 text-left hover:bg-muted",
            picked === p && "bg-muted font-semibold",
          )}
        >
          <Icon className="size-3.5 shrink-0 text-muted-foreground" />
          {e.dir && <FolderIcon className="size-3.5 shrink-0 text-muted-foreground" />}
          <span className="truncate">{e.name}</span>
        </button>
        {e.dir && isOpen && <FolderList task={task} path={p} depth={depth + 1} picked={picked} onPick={onPick} />}
      </div>
    );
  });
}

/** Browses the focused task's worktree and shows files with the artifact viewers. */
export function FilesPanel() {
  const task = taskOf(useFocus());
  const [picked, setPicked] = useState<{ task: Addr; path: string } | null>(null);
  const [version, setVersion] = useState(0);
  if (!task) return <p className="p-3 text-muted-foreground">Focus a task to browse its worktree.</p>;
  const path = picked?.task === task ? picked.path : null;

  return (
    <div className="flex h-full flex-col">
      <div className="relative max-h-[45%] shrink-0 overflow-auto border-b p-1.5">
        <Button
          variant="ghost"
          size="icon-xs"
          title="Refresh"
          className="absolute top-1.5 right-1.5"
          onClick={() => setVersion(version + 1)}
        >
          <RefreshCw />
        </Button>
        <FolderList
          key={`${task}#${version}`}
          task={task}
          path="."
          depth={0}
          picked={path}
          onPick={(p) => setPicked({ task, path: p })}
        />
      </div>
      {path && <Viewer key={path} src={fileURL(task, path)} name={path} version={version} />}
    </div>
  );
}

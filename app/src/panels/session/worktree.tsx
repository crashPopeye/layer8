import { ChevronDown, Code, Folder, GitBranch, GitBranchPlus, SquareTerminal } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import type { Node } from "@/lib/types";

/** Where a task's worktree stands: its branch, commits ahead of and behind its base, and uncommitted paths. */
export interface BranchStatus {
  branch: string;
  base: string;
  ahead: number;
  behind: number;
  dirty: number;
}

/** Reads a task node's BranchStatus. */
export function branchStatus(task: Node): BranchStatus {
  const f = task.fields ?? {};
  const git = (f.git ?? {}) as { changed?: number };
  return {
    branch: String(f.branch ?? ""),
    base: String(f.base ?? ""),
    ahead: Number(f.ahead ?? 0),
    behind: Number(f.behind ?? 0),
    dirty: Number(git.changed ?? 0),
  };
}

const openTargets = [
  ["terminal", "Open in terminal", SquareTerminal],
  ["editor", "Open in editor", Code],
  ["finder", "Reveal in Finder", Folder],
] as const;

/** The task's branch chip: ahead/behind/dirty at a glance, a menu to open the worktree, and for a new session where it runs. */
export function BranchChip({
  task,
  onOpen,
  fresh,
  onFresh,
}: {
  task: Node;
  onOpen: (target: string) => void;
  fresh: boolean | null;
  onFresh: (fresh: boolean) => void;
}) {
  const s = branchStatus(task);
  const canOpen = task.actions.some((a) => a.name === "open");
  const canFork = fresh !== null && task.actions.some((a) => a.name === "create-subtask");
  if (!s.branch) return null;
  const title = [`${s.branch} on ${s.base}`, `${s.ahead} ahead, ${s.behind} behind`, `${s.dirty} uncommitted`].join(" · ");
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          title={title}
          className="flex h-7 max-w-60 items-center gap-1.5 rounded-md px-2 font-mono text-[11px] text-muted-foreground transition-colors outline-none hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50 data-[state=open]:bg-accent data-[state=open]:text-foreground"
        >
          {fresh ? <GitBranchPlus className="size-3.5 shrink-0" /> : <GitBranch className="size-3.5 shrink-0" />}
          <span className="truncate">{fresh ? "new sub-task" : s.branch}</span>
          {!fresh && s.ahead > 0 && <span>↑{s.ahead}</span>}
          {!fresh && s.behind > 0 && <span>↓{s.behind}</span>}
          {!fresh && s.dirty > 0 && <span className="text-wait">●{s.dirty}</span>}
          <ChevronDown className="size-3 shrink-0 opacity-60" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" side="top" className="min-w-56">
        {canOpen &&
          openTargets.map(([target, label, Icon]) => (
            <DropdownMenuItem key={target} onSelect={() => onOpen(target)}>
              <Icon /> {label}
            </DropdownMenuItem>
          ))}
        {canFork && (
          <>
            {canOpen && <div role="separator" className="-mx-1 my-1 h-px bg-border" />}
            <p className="px-2 py-1 text-[11px] text-muted-foreground">New session runs in</p>
            <DropdownMenuRadioGroup value={fresh ? "fresh" : "here"} onValueChange={(v) => onFresh(v === "fresh")}>
              <DropdownMenuRadioItem value="here">
                <GitBranch /> This worktree
              </DropdownMenuRadioItem>
              <DropdownMenuRadioItem value="fresh">
                <GitBranchPlus /> New sub-task worktree
              </DropdownMenuRadioItem>
            </DropdownMenuRadioGroup>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

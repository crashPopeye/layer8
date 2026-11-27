import { Check, Pencil, RotateCcw, Send, Trash2 } from "lucide-react";
import { useEffect, useState, type CSSProperties } from "react";
import { Button } from "@/components/ui/button";
import { useAction, useNode } from "@/lib/react";
import type { Addr } from "@/lib/types";
import { cn } from "@/lib/utils";
import { commentsOf, lineLabel, liveSession, type Comment } from "./model";

/** A textarea that saves on ⌘Enter or Save and cancels on Escape. */
export function CommentBox({
  initial = "",
  quote,
  onSave,
  onCancel,
  className,
  style,
}: {
  initial?: string;
  quote?: string;
  onSave: (body: string) => Promise<unknown> | void;
  onCancel: () => void;
  className?: string;
  style?: CSSProperties;
}) {
  const [body, setBody] = useState(initial);
  const save = () => body.trim() && onSave(body.trim());
  return (
    <div className={cn("flex flex-col gap-1.5 rounded-md border bg-card p-2 font-sans shadow-md", className)} style={style}>
      {quote && <blockquote className="line-clamp-3 border-l-2 pl-2 text-muted-foreground">{quote}</blockquote>}
      <textarea
        autoFocus
        value={body}
        rows={3}
        placeholder="Comment for the agent"
        onChange={(e) => setBody(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) save();
          if (e.key === "Escape") onCancel();
          e.stopPropagation();
        }}
        className="resize-none rounded-md border bg-input/30 px-2 py-1 outline-none select-text focus-visible:ring-2 focus-visible:ring-ring/50"
      />
      <div className="flex justify-end gap-1.5">
        <Button size="xs" variant="ghost" onClick={onCancel}>
          Cancel
        </Button>
        <Button size="xs" disabled={!body.trim()} onClick={save}>
          Comment
        </Button>
      </div>
    </div>
  );
}

const statusTone: Record<Comment["status"], string> = { open: "text-wait", sent: "text-primary", resolved: "text-ok" };

/** One comment with its location, text and the actions it offers; a resolved one shows the agent's reply. */
export function CommentCard({ comment: c }: { comment: Comment }) {
  const action = useAction(c.addr);
  const [editing, setEditing] = useState(false);
  if (editing) {
    return (
      <CommentBox
        initial={c.body}
        onSave={async (body) => (await action.run("edit", { body })).ok && setEditing(false)}
        onCancel={() => setEditing(false)}
      />
    );
  }
  return (
    <div className="group flex flex-col gap-1 rounded-md border bg-card px-2 py-1.5 font-sans">
      <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
        <span className={statusTone[c.status]}>{c.status}</span>
        {c.target === "diff" && <span className="truncate font-mono">{c.path}</span>}
        {c.line ? <span className="font-mono">{lineLabel(c)}</span> : null}
        <span className="ml-auto flex gap-0.5 opacity-0 group-hover:opacity-100">
          {c.status !== "resolved" && (
            <>
              <Button variant="ghost" size="icon-xs" title="Resolve" onClick={() => action.run("resolve")}>
                <Check />
              </Button>
              <Button variant="ghost" size="icon-xs" title="Edit" onClick={() => setEditing(true)}>
                <Pencil />
              </Button>
            </>
          )}
          {c.status !== "open" && (
            <Button variant="ghost" size="icon-xs" title="Reopen" onClick={() => action.run("reopen")}>
              <RotateCcw />
            </Button>
          )}
          <Button variant="ghost" size="icon-xs" title="Delete" onClick={() => action.run("delete")}>
            <Trash2 />
          </Button>
        </span>
      </div>
      {c.quote && <blockquote className="line-clamp-2 border-l-2 pl-2 text-muted-foreground">{c.quote}</blockquote>}
      <p className="whitespace-pre-wrap select-text">{c.body}</p>
      {c.reply && (
        <p className="border-l-2 border-ok/60 pl-2 whitespace-pre-wrap text-muted-foreground select-text">
          {c.resolvedBy}: {c.reply}
        </p>
      )}
      {action.result && !action.result.ok && <p className="text-bad">{action.result.message}</p>}
    </div>
  );
}

/** Comments still to address, then the resolved ones collapsed. */
export function CommentList({ comments }: { comments: Comment[] }) {
  const resolved = comments.filter((c) => c.status === "resolved");
  return (
    <div className="flex flex-col gap-1.5">
      {comments
        .filter((c) => c.status !== "resolved")
        .map((c) => (
          <CommentCard key={c.addr} comment={c} />
        ))}
      {resolved.length > 0 && (
        <details>
          <summary className="cursor-pointer text-muted-foreground">{resolved.length} resolved</summary>
          <div className="mt-1.5 flex flex-col gap-1.5">
            {resolved.map((c) => (
              <CommentCard key={c.addr} comment={c} />
            ))}
          </div>
        </details>
      )}
    </div>
  );
}

/** "Send N comments to <session>": sends the task's open comments to its most recent live session. */
export function SendComments({ task }: { task: Addr }) {
  const { node } = useNode(task, 1);
  const action = useAction(task);
  const open = commentsOf(node).filter((c) => c.status === "open").length;
  const session = liveSession(node?.children ?? []);
  if (!open) return null;
  return (
    <div className="flex items-center gap-2">
      <Button
        size="xs"
        disabled={!session || !!action.busy}
        title={session ? undefined : "Start a session on this task first"}
        onClick={() => action.run("send-comments", { session: session!.addr })}
      >
        <Send />
        Send {open} {open === 1 ? "comment" : "comments"} to {session?.title || session?.addr || "a session"}
      </Button>
      {action.result && !action.result.ok && <span className="truncate text-bad">{action.result.message}</span>}
    </div>
  );
}

function typing(e: KeyboardEvent): boolean {
  const el = e.target as HTMLElement | null;
  return e.metaKey || e.ctrlKey || e.altKey || !!el?.closest?.("input, textarea, [contenteditable]");
}

/** Runs onC when C is pressed outside a field while active. */
export function useCShortcut(active: boolean, onC: () => void) {
  useEffect(() => {
    if (!active) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "c" || typing(e)) return;
      e.preventDefault();
      onC();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [active, onC]);
}

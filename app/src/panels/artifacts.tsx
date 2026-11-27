import { FileCode, FileImage, FileText, Globe, MessageSquarePlus } from "lucide-react";
import { useMemo, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { commentsOf, marksOf } from "@/comments/model";
import { CommentBox, CommentList, SendComments, useCShortcut } from "@/comments/ui";
import { taskOf, useFocus } from "@/lib/focus";
import { useClient, useNode } from "@/lib/react";
import type { Addr, Summary } from "@/lib/types";
import { cn } from "@/lib/utils";
import { Viewer, type TextSelection } from "@/viewers/viewer";

const icons = { md: FileText, html: Globe, htm: Globe, png: FileImage, jpg: FileImage, svg: FileImage, gif: FileImage };

function iconOf(name: string) {
  return icons[name.split(".").pop()!.toLowerCase() as keyof typeof icons] ?? FileCode;
}

/** Shows one artifact with its comments: select text, then press C or click Comment, to comment on it. */
function ArtifactView({ addr, task }: { addr: Addr; task: Addr }) {
  const client = useClient();
  const { node, error } = useNode(addr, 0);
  const box = useRef<HTMLDivElement>(null);
  const [sel, setSel] = useState<TextSelection | null>(null);
  const [writing, setWriting] = useState<"selection" | "artifact" | null>(null);
  const open = () => setWriting("selection");
  useCShortcut(!!sel && !writing, open);
  const comments = useMemo(() => commentsOf(node), [node]);
  const marks = useMemo(() => marksOf(comments), [comments]);
  if (error) return <p className="p-3 text-bad">{error.message}</p>;
  if (!node?.fields?.url) return null;

  const pick = (s: TextSelection | null) => {
    if (writing) return;
    setSel(s);
    if (s?.open) open();
  };
  const close = () => {
    setSel(null);
    setWriting(null);
  };
  async function save(body: string) {
    const at = writing === "selection" && sel ? { quote: sel.quote, ...sel.lines } : {};
    const [r] = await client.doOps([{ target: addr, action: "add-comment", args: { body, ...at } }]);
    if (r.ok) {
      close();
      getSelection()?.removeAllRanges();
    }
  }
  const frame = box.current?.getBoundingClientRect();
  const at = sel && frame && { top: sel.rect.bottom - frame.top + 4, left: Math.max(0, Math.min(sel.rect.left - frame.left, frame.width - 290)) };

  return (
    <>
      <div ref={box} className="relative flex min-h-0 flex-1 flex-col">
        <Viewer src={node.fields.url as string} name={node.title} version={node.fields.modified} marks={marks} onSelectText={pick} />
        {at && !writing && (
          <Button size="xs" className="absolute z-20" style={at} onMouseDown={(e) => e.preventDefault()} onClick={open}>
            <MessageSquarePlus />
            Comment <kbd className="opacity-60">C</kbd>
          </Button>
        )}
        {writing && (
          <CommentBox
            className="absolute z-20 w-72"
            style={writing === "selection" && at ? at : { top: 8, right: 8 }}
            quote={writing === "selection" ? sel?.quote : undefined}
            onSave={save}
            onCancel={close}
          />
        )}
      </div>
      <div className="flex max-h-[35%] shrink-0 flex-col gap-1.5 overflow-auto border-t p-2">
        <div className="flex items-center gap-2">
          <span className="font-semibold">Comments</span>
          <Button variant="ghost" size="icon-xs" title="Comment on the whole artifact" onClick={() => setWriting("artifact")}>
            <MessageSquarePlus />
          </Button>
          <span className="ml-auto">
            <SendComments task={task} />
          </span>
        </div>
        {comments.length === 0 && <p className="text-muted-foreground">Select text and press C to comment for the agent.</p>}
        <CommentList comments={comments} />
      </div>
    </>
  );
}

/** The focused task tree's artifacts with their open comment counts: a list above a viewer. */
export function ArtifactsPanel() {
  const focus = useFocus();
  const task = taskOf(focus);
  const { node } = useNode(task ?? "app", 0);
  const [picked, setPicked] = useState<Addr | null>(null);
  if (!task) return <p className="p-3 text-muted-foreground">Focus a task to see its artifacts.</p>;

  const files = (node?.addr === task ? (node.fields?.artifactFiles ?? []) : []) as Summary[];
  const wanted = focus.startsWith("artifact:") ? focus : picked;
  const current = files.find((f) => f.addr === wanted) ?? files[0];
  const counts = new Map<Addr, number>();
  for (const c of commentsOf(node)) {
    if (c.artifact && c.status !== "resolved") counts.set(c.artifact, (counts.get(c.artifact) ?? 0) + 1);
  }

  return (
    <div className="flex h-full flex-col">
      <div className="max-h-[30%] shrink-0 overflow-auto border-b p-1.5">
        {files.map((f) => {
          const Icon = iconOf(f.title);
          return (
            <button
              key={f.addr}
              onClick={() => setPicked(f.addr)}
              className={cn(
                "flex w-full items-center gap-2 rounded-md px-2 py-1 text-left hover:bg-muted",
                f === current && "bg-muted font-semibold",
              )}
            >
              <Icon className="size-3.5 shrink-0 text-muted-foreground" />
              <span className="truncate">{f.title}</span>
              {counts.get(f.addr) && (
                <span className="ml-auto rounded-full bg-primary/15 px-1.5 text-[11px] text-primary">{counts.get(f.addr)}</span>
              )}
            </button>
          );
        })}
        {node && files.length === 0 && <p className="p-1.5 text-muted-foreground">No artifacts yet.</p>}
      </div>
      {current && <ArtifactView key={current.addr} addr={current.addr} task={task} />}
    </div>
  );
}

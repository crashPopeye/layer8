import { FileCode, FileImage, FileText, Globe } from "lucide-react";
import { useState } from "react";
import { taskOf, useFocus } from "@/lib/focus";
import { useNode } from "@/lib/react";
import type { Addr, Summary } from "@/lib/types";
import { cn } from "@/lib/utils";
import { Viewer } from "@/viewers/viewer";

const icons = { md: FileText, html: Globe, htm: Globe, png: FileImage, jpg: FileImage, svg: FileImage, gif: FileImage };

function iconOf(name: string) {
  return icons[name.split(".").pop()!.toLowerCase() as keyof typeof icons] ?? FileCode;
}

/** Shows one artifact, reloading it whenever the daemon reports it changed. */
function ArtifactView({ addr }: { addr: Addr }) {
  const { node, error } = useNode(addr, 0);
  if (error) return <p className="p-3 text-bad">{error.message}</p>;
  if (!node?.fields?.url) return null;
  return <Viewer src={node.fields.url as string} name={node.title} version={node.fields.modified} />;
}

/** The focused task tree's artifacts: a list above a viewer. */
export function ArtifactsPanel() {
  const focus = useFocus();
  const task = taskOf(focus);
  const { node } = useNode(task ?? "app", 0);
  const [picked, setPicked] = useState<Addr | null>(null);
  if (!task) return <p className="p-3 text-muted-foreground">Focus a task to see its artifacts.</p>;

  const files = (node?.addr === task ? (node.fields?.artifactFiles ?? []) : []) as Summary[];
  const wanted = focus.startsWith("artifact:") ? focus : picked;
  const current = files.find((f) => f.addr === wanted) ?? files[0];

  return (
    <div className="flex h-full flex-col">
      <div className="max-h-[40%] shrink-0 overflow-auto border-b p-1.5">
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
            </button>
          );
        })}
        {node && files.length === 0 && <p className="p-1.5 text-muted-foreground">No artifacts yet.</p>}
      </div>
      {current && <ArtifactView key={current.addr} addr={current.addr} />}
    </div>
  );
}

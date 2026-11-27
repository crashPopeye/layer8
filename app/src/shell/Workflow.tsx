import { Play } from "lucide-react";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { useFocus } from "@/lib/focus";
import { useClient, useNode } from "@/lib/react";
import type { Addr, Op } from "@/lib/types";

/** The `workflow` field of a task running a workflow. */
export interface WorkflowField {
  name: string;
  task: Addr;
  step: string;
  status: "running" | "waiting" | "done" | "stopped";
  autopilot: boolean;
  next: string;
}

/** Runs a workflow on the focused task, and starts its next step when one is waiting. */
export function WorkflowControl() {
  const focus = useFocus();
  return focus.startsWith("task:") ? <TaskWorkflow addr={focus} /> : null;
}

function TaskWorkflow({ addr }: { addr: Addr }) {
  const client = useClient();
  const { node: app } = useNode("app");
  const { node: task } = useNode(addr, 0);
  const [autopilot, setAutopilot] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const workflows = app?.children.filter((c) => c.kind === "workflow" && c.status !== "invalid") ?? [];
  const wf = task?.fields?.workflow as WorkflowField | undefined;
  const can = (action: string) => task?.actions.some((a) => a.name === action) ?? false;
  const run = async (op: Op) => {
    const [result] = await client.doOps([op]);
    setError(result.ok ? null : (result.message ?? `${op.action} failed`));
  };

  return (
    <div className="flex items-center gap-1.5 text-[12px]">
      {wf && (wf.status === "running" || wf.status === "waiting") && (
        <span className="text-muted-foreground">
          {wf.name} · {wf.step}
          {wf.autopilot && " · autopilot"}
        </span>
      )}
      {can("next-step") && (
        <Button size="sm" className="h-7" onClick={() => run({ target: addr, action: "next-step" })}>
          Next: {wf?.next} ▸
        </Button>
      )}
      {can("run-workflow") && (
        <>
          <select
            aria-label="Run workflow"
            className="h-7 rounded-md border bg-transparent px-1.5 text-muted-foreground"
            value=""
            onChange={(e) =>
              e.target.value && run({ target: addr, action: "run-workflow", args: { workflow: e.target.value, autopilot } })
            }
          >
            <option value="">Workflow…</option>
            {workflows.map((w) => (
              <option key={w.addr} value={w.title}>
                {w.title}
              </option>
            ))}
          </select>
          <Button
            variant={autopilot ? "secondary" : "ghost"}
            size="icon-sm"
            aria-pressed={autopilot}
            title="Autopilot: start each step as soon as the previous one finishes"
            onClick={() => setAutopilot(!autopilot)}
          >
            <Play />
          </Button>
        </>
      )}
      {error && (
        <span className="max-w-60 truncate text-bad" title={error}>
          {error}
        </span>
      )}
    </div>
  );
}

import { useState } from "react";
import type { Approval } from "@layer8/types";

function describe(a: Approval): string {
  const i = a.input as Record<string, any>;
  if (a.toolName === "Bash") return String(i.command ?? "");
  if (typeof i.file_path === "string") return i.file_path;
  return JSON.stringify(i, null, 2);
}

export function ApprovalBlock({ approval, decide }: { approval: Approval; decide: (allow: boolean, msg?: string) => unknown }) {
  const [reason, setReason] = useState("");
  return (
    <div className="approval">
      <div>
        <div className="what">Approve {approval.toolName}?</div>
        <pre>{describe(approval)}</pre>
        <div className="row">
          <button className="key primary" onClick={() => decide(true)}>
            <kbd>A</kbd> Allow
          </button>
          <input
            className="field"
            placeholder="Or tell the agent what to do instead…"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && decide(false, reason || undefined)}
          />
          <button className="key danger" onClick={() => decide(false, reason || undefined)}>
            <kbd>D</kbd> Deny
          </button>
        </div>
      </div>
    </div>
  );
}

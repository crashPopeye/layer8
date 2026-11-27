import { FileText, GitCompare, GitPullRequest, ListTree, MessageSquare, type LucideIcon } from "lucide-react";
import { registerPanel } from "@/dock/registry";
import { useFocus } from "@/lib/focus";
import { ChangesPanel } from "./changes";
import { PRPanel } from "./pr";
import { ProjectsPanel } from "./projects";

function placeholder(Icon: LucideIcon, what: string) {
  return function Placeholder() {
    const focus = useFocus();
    return (
      <div className="flex h-full flex-col items-center justify-center gap-2 text-muted-foreground">
        <Icon className="size-5" />
        <p>{what}</p>
        <code className="font-mono text-[11px]">{focus}</code>
      </div>
    );
  };
}

registerPanel({ id: "tasks", title: "Tasks", icon: ListTree, component: ProjectsPanel, defaultPosition: "left" });
registerPanel({
  id: "session",
  title: "Session",
  icon: MessageSquare,
  component: placeholder(MessageSquare, "The session transcript and composer land here."),
  defaultPosition: "center",
});
registerPanel({
  id: "artifacts",
  title: "Artifacts",
  icon: FileText,
  component: placeholder(FileText, "The task tree's artifacts land here."),
  defaultPosition: "right",
});
registerPanel({ id: "pr", title: "PR", icon: GitPullRequest, component: PRPanel, defaultPosition: "right" });
registerPanel({ id: "changes", title: "Changes", icon: GitCompare, component: ChangesPanel, defaultPosition: "right-bottom" });

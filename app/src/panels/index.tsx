import { FileText, FolderTree, GitCompare, GitPullRequest, ListTree, MessageSquare, Palette, Puzzle } from "lucide-react";
import { registerPanel } from "@/dock/registry";
import { ArtifactsPanel } from "./artifacts";
import { ChangesPanel } from "./changes";
import { FilesPanel } from "./files";
import { PluginsPanel } from "./plugins";
import { PRPanel } from "./pr";
import { SessionPanel } from "./session";
import { TasksPanel } from "./tasks/panel";
import { ThemePanel } from "./theme";

registerPanel({ id: "tasks", title: "Tasks", icon: ListTree, component: TasksPanel, defaultPosition: "left" });
registerPanel({
  id: "session",
  title: "Session",
  icon: MessageSquare,
  component: SessionPanel,
  defaultPosition: "center",
});
registerPanel({
  id: "artifacts",
  title: "Artifacts",
  icon: FileText,
  component: ArtifactsPanel,
  defaultPosition: "right",
});
registerPanel({ id: "pr", title: "PR", icon: GitPullRequest, component: PRPanel, defaultPosition: "right" });
registerPanel({ id: "files", title: "Files", icon: FolderTree, component: FilesPanel, defaultPosition: "right" });
registerPanel({ id: "changes", title: "Changes", icon: GitCompare, component: ChangesPanel, defaultPosition: "right-bottom" });
registerPanel({ id: "theme", title: "Theme", icon: Palette, component: ThemePanel, defaultPosition: "right" });
registerPanel({ id: "plugins", title: "Plugins", icon: Puzzle, component: PluginsPanel, defaultPosition: "right" });

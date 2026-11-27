import { useEffect } from "react";
import { ActionDialog } from "@/components/action-form";
import { Dock, focusPanel } from "@/dock/Dock";
import { newSubtask, newTask } from "@/lib/commands";
import { useClient } from "@/lib/react";
import { useThemeSync } from "@/lib/theme";
import { applyUi } from "@/lib/ui";
import { newSession } from "@/panels/session/target";
import { usePluginHost } from "@/plugins/host";
import { Footer } from "@/shell/Footer";
import { Palette, togglePalette } from "@/shell/Palette";
import { Toasts } from "@/shell/Toasts";
import { TopBar } from "@/shell/TopBar";
import { UpdatePrompt } from "@/shell/UpdatePrompt";

/** The main window: title bar, dock of panels, status bar, ⌘K palette and notifications. */
export function App() {
  const client = useClient();
  useThemeSync();
  usePluginHost();
  useEffect(() => client.subscribe((e) => e.type === "ui" && applyUi(e.data, client)), [client]);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!e.metaKey) return;
      const key = e.key.toLowerCase();
      if (key === "k") togglePalette();
      else if (key === "n" && e.shiftKey) newSubtask();
      else if (key === "n") newTask(client);
      else if (key === "t") newSession();
      else if (/^[1-9]$/.test(key)) focusPanel(Number(key));
      else return;
      e.preventDefault();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [client]);
  return (
    <div className="flex h-full flex-col">
      <TopBar />
      <main className="min-h-0 flex-1">
        <Dock />
      </main>
      <Footer />
      <ActionDialog />
      <Palette />
      <Toasts />
      <UpdatePrompt />
    </div>
  );
}

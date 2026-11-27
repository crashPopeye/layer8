import { useEffect } from "react";
import { ActionDialog } from "@/components/action-form";
import { Dock } from "@/dock/Dock";
import { newSubtask, newTask } from "@/lib/commands";
import { useClient } from "@/lib/react";
import { applyUi } from "@/lib/ui";
import { newSession } from "@/panels/session/target";
import { Footer } from "@/shell/Footer";
import { TopBar } from "@/shell/TopBar";

/** The main window: title bar, dock of panels, status bar. */
export function App() {
  const client = useClient();
  useEffect(() => client.subscribe((e) => e.type === "ui" && applyUi(e.data, client)), [client]);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const key = e.key.toLowerCase();
      if (!e.metaKey || (key !== "n" && key !== "t")) return;
      e.preventDefault();
      if (key === "t") newSession();
      else if (e.shiftKey) newSubtask();
      else newTask(client);
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
    </div>
  );
}

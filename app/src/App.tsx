import { useEffect } from "react";
import { ActionDialog } from "@/components/action-form";
import { Dock } from "@/dock/Dock";
import { newSubtask, newTask } from "@/lib/commands";
import { useClient } from "@/lib/react";
import { applyUi } from "@/lib/ui";
import { Footer } from "@/shell/Footer";
import { TopBar } from "@/shell/TopBar";

/** The main window: title bar, dock of panels, status bar. */
export function App() {
  const client = useClient();
  useEffect(() => client.subscribe((e) => e.type === "ui" && applyUi(e.data)), [client]);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!e.metaKey || e.key.toLowerCase() !== "n") return;
      e.preventDefault();
      if (e.shiftKey) newSubtask();
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

import { useEffect } from "react";
import { Dock } from "@/dock/Dock";
import { useClient } from "@/lib/react";
import { applyUi } from "@/lib/ui";
import { Footer } from "@/shell/Footer";
import { TopBar } from "@/shell/TopBar";

/** The main window: title bar, dock of panels, status bar. */
export function App() {
  const client = useClient();
  useEffect(() => client.subscribe((e) => e.type === "ui" && applyUi(e.data)), [client]);
  return (
    <div className="flex h-full flex-col">
      <TopBar />
      <main className="min-h-0 flex-1">
        <Dock />
      </main>
      <Footer />
    </div>
  );
}

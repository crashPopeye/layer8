import "@fontsource/ibm-plex-mono/400.css";
import "@fontsource/ibm-plex-mono/500.css";
import "@fontsource/ibm-plex-mono/600.css";
import "@fontsource-variable/archivo/wdth.css";
import "./styles.css";
import { createRoot } from "react-dom/client";
import { App } from "./App";

createRoot(document.getElementById("root")!).render(<App />);

// External links open in the user's browser, not inside the app window.
if ("__TAURI_INTERNALS__" in window) {
  document.addEventListener("click", async (e) => {
    const a = (e.target as HTMLElement).closest("a[href^='http']") as HTMLAnchorElement | null;
    if (!a) return;
    e.preventDefault();
    const { openUrl } = await import("@tauri-apps/plugin-opener");
    await openUrl(a.href);
  });
}

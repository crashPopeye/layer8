import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { connect } from "./lib/connect";
import { registerCommand } from "./lib/palette";
import { ClientProvider } from "./lib/react";
import { restoreTheme } from "./lib/theme";
import { checkForUpdate } from "./lib/updater";
import "./panels";
import "./index.css";

restoreTheme();
registerCommand({ id: "check-for-updates", title: "Check for updates", group: "layer8", run: () => checkForUpdate(true) });

connect().then((client) => {
  createRoot(document.getElementById("root")!).render(
    <StrictMode>
      <ClientProvider client={client}>
        <App />
      </ClientProvider>
    </StrictMode>,
  );
  checkForUpdate();
});

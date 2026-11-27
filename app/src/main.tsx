import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { connect } from "./lib/connect";
import { ClientProvider } from "./lib/react";
import { setTheme, storedTheme } from "./lib/theme";
import "./panels";
import "./index.css";

setTheme(storedTheme());

connect().then((client) =>
  createRoot(document.getElementById("root")!).render(
    <StrictMode>
      <ClientProvider client={client}>
        <App />
      </ClientProvider>
    </StrictMode>,
  ),
);

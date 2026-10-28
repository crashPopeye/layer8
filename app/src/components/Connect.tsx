import { useState } from "react";
import { rememberDaemon, type DaemonInfo } from "../api";

/** Browser-only fallback; the Tauri shell finds the daemon by itself. */
export function Connect({ onConnect }: { onConnect: (i: DaemonInfo) => void }) {
  const [url, setUrl] = useState("http://127.0.0.1:7878");
  const [token, setToken] = useState("");
  return (
    <div className="connect">
      <div>
        <div className="display">
          layer<span className="signal">8</span>
        </div>
        <div className="dim">
          Start the daemon with <code>pnpm daemon</code>, then paste the token from <code>~/.layer8/token</code>.
        </div>
        <input className="field" value={url} onChange={(e) => setUrl(e.target.value)} />
        <input className="field" placeholder="token" value={token} onChange={(e) => setToken(e.target.value)} autoFocus />
        <button
          className="key primary"
          disabled={!token.trim()}
          onClick={() => {
            const info = { url: url.replace(/\/$/, ""), token: token.trim() };
            rememberDaemon(info);
            onConnect(info);
          }}
        >
          Connect
        </button>
      </div>
    </div>
  );
}

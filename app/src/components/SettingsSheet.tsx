import { useEffect, useState } from "react";
import type { Config, EditorApp, TerminalApp, Workspace } from "@layer8/types";
import type { Api } from "../api";
import { home } from "../util";

type Run = <T>(p: Promise<T>) => Promise<T | undefined>;

const terminals: TerminalApp[] = ["ghostty", "wezterm", "kitty", "iterm", "terminal", "custom"];
const editors: EditorApp[] = ["code", "cursor", "zed", "idea", "custom"];

export function SettingsSheet({
  api,
  run,
  workspaces,
  onClose,
}: {
  api: Api;
  run: Run;
  workspaces: Workspace[];
  onClose: () => void;
}) {
  const [cfg, setCfg] = useState<Config | null>(null);
  const [key, setKey] = useState("");
  const [path, setPath] = useState("");
  useEffect(() => {
    api.config().then(setCfg);
  }, [api]);
  if (!cfg) return null;

  const set = <K extends keyof Config>(k: K, v: Config[K]) => setCfg({ ...cfg, [k]: v });
  const save = async () => {
    const ok = await run(api.saveConfig({ ...cfg, linearApiKey: key.trim() || cfg.linearApiKey }));
    if (ok) onClose();
  };

  return (
    <>
      <div className="scrim" onClick={onClose} />
      <div className="sheet" onKeyDown={(e) => e.key === "Escape" && onClose()}>
        <div className="sheet-head">
          <span className="display">Settings</span>
          <span style={{ flex: 1 }} />
          <button className="key" onClick={onClose}>
            Esc
          </button>
          <button className="key primary" onClick={save}>
            Save
          </button>
        </div>
        <div className="sheet-body">
          <div className="form">
            <span className="label">Workspaces</span>
            <div style={{ display: "grid", gap: 4 }}>
              {workspaces.map((w) => (
                <div key={w.id} className="list-row" style={{ cursor: "default" }}>
                  <span style={{ width: 120 }}>{w.name}</span>
                  <span className="grow faint">{home(w.path)}</span>
                  <span className="faint">{w.isGit ? `git · ${w.defaultBranch}` : "folder"}</span>
                  <button className="key danger" style={{ height: 20 }} onClick={() => run(api.removeWorkspace(w.id))}>
                    ✕
                  </button>
                </div>
              ))}
              <div style={{ display: "flex", gap: 6 }}>
                <input
                  className="field"
                  placeholder="/absolute/path/to/folder"
                  value={path}
                  onChange={(e) => setPath(e.target.value)}
                />
                <button
                  className="key"
                  disabled={!path.trim()}
                  onClick={async () => (await run(api.addWorkspace(path.trim()))) && setPath("")}
                >
                  Add
                </button>
              </div>
            </div>

            <span className="label">Linear API key</span>
            <input
              className="field"
              type="password"
              placeholder={cfg.linearApiKey ? "•••••••• (set, type to replace)" : "lin_api_…"}
              value={key}
              onChange={(e) => setKey(e.target.value)}
            />
            <span className="hint">Linear → Settings → Security & access → Personal API keys.</span>

            <span className="label">Terminal</span>
            <div className="seg">
              {terminals.map((t) => (
                <button key={t} aria-pressed={cfg.terminal === t} onClick={() => set("terminal", t)}>
                  {t}
                </button>
              ))}
            </div>
            {cfg.terminal === "custom" && (
              <>
                <span />
                <input
                  className="field"
                  placeholder="alacritty --working-directory {cwd} -e sh -c {cmd}"
                  value={cfg.terminalCommand ?? ""}
                  onChange={(e) => set("terminalCommand", e.target.value)}
                />
              </>
            )}

            <span className="label">tmux per task</span>
            <div className="seg">
              <button aria-pressed={cfg.useTmux} onClick={() => set("useTmux", true)}>on</button>
              <button aria-pressed={!cfg.useTmux} onClick={() => set("useTmux", false)}>off</button>
            </div>
            <span className="hint">Shells and dev servers survive closing the terminal window.</span>

            <span className="label">Editor</span>
            <div className="seg">
              {editors.map((t) => (
                <button key={t} aria-pressed={cfg.editor === t} onClick={() => set("editor", t)}>
                  {t}
                </button>
              ))}
            </div>
            {cfg.editor === "custom" && (
              <>
                <span />
                <input
                  className="field"
                  placeholder="nvim-qt {path}"
                  value={cfg.editorCommand ?? ""}
                  onChange={(e) => set("editorCommand", e.target.value)}
                />
              </>
            )}

            <span className="label">Parallel agents</span>
            <input
              className="field"
              type="number"
              min={1}
              max={32}
              style={{ width: 90 }}
              value={cfg.maxConcurrentSessions}
              onChange={(e) => set("maxConcurrentSessions", Math.max(1, Number(e.target.value) || 1))}
            />

            <span className="label">Worktrees in</span>
            <input className="field" value={cfg.worktreeRoot} onChange={(e) => set("worktreeRoot", e.target.value)} />
          </div>
        </div>
      </div>
    </>
  );
}

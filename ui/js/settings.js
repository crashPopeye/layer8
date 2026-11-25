import { html, useState, useEffect } from "/vendor/preact-htm.js";
import { get, put } from "./api.js";

/** useLinear reports whether a Linear API key is set, or null while it is unknown. */
export function useLinear() {
  const [connected, setConnected] = useState(null);
  useEffect(() => {
    get("/api/config").then(
      (c) => setConnected(c.linearConnected),
      () => setConnected(false),
    );
  }, []);
  return connected;
}

/** Settings is the dialog holding the Linear API key and each project's Linear write-back toggle. */
export function Settings({ projects, onClose }) {
  const [connected, setConnected] = useState(null);
  const [key, setKey] = useState("");
  const [error, setError] = useState("");
  useEffect(() => {
    get("/api/config").then((c) => setConnected(c.linearConnected), (err) => setError(err.message));
  }, []);
  useEffect(() => {
    const onKey = (e) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const saveKey = async (value) => {
    setError("");
    try {
      setConnected((await put("/api/config", { linearApiKey: value })).linearConnected);
      setKey("");
    } catch (err) {
      setError(err.message);
    }
  };
  const writeBack = (p) => (e) => put(`/api/projects/${p.id}`, { linearWriteBack: e.target.checked }).catch((err) => setError(err.message));

  return html`<div class="scrim" onClick=${(e) => e.target === e.currentTarget && onClose()}>
    <div class="dialog">
      <div class="lbl">Settings</div>
      <form class="field" onSubmit=${(e) => (e.preventDefault(), saveKey(key.trim()))}>
        <span class="lbl">Linear personal API key ${connected ? "· connected" : connected === false ? "· not connected" : ""}</span>
        <div class="row-inline">
          <input class="mono" type="password" autocomplete="off" placeholder=${connected ? "lin_api_… (replace)" : "lin_api_…"} value=${key} onInput=${(e) => setKey(e.target.value)} />
          <button class="primary" type="submit" disabled=${!key.trim()}>Save</button>
          ${connected && html`<button type="button" onClick=${() => saveKey("")}>Disconnect</button>`}
        </div>
        <span class="dim">Create one in Linear under Settings → Security & access. It is stored in config.json, readable by you only.</span>
      </form>
      ${projects.length > 0 &&
      html`<div class="field">
        <span class="lbl">Move the ticket to In Review when a PR opens</span>
        ${projects.map(
          (p) => html`<label key=${p.id} class="check"><input type="checkbox" checked=${p.linearWriteBack} onChange=${writeBack(p)} /> ${p.name}</label>`,
        )}
      </div>`}
      <div class="btns">
        ${error && html`<span class="err">${error}</span>`}
        <button type="button" onClick=${onClose}>Close</button>
      </div>
    </div>
  </div>`;
}

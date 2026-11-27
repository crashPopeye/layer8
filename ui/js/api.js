// Talks to the layer8d daemon over HTTP on 127.0.0.1 with its token.
// Inside the app, the Tauri shell tells us where it is; in a browser (development,
// served by the daemon itself) the user pastes the token once.

export const tauri = window.__TAURI__ || null;
const invoke = (cmd, args) => tauri.core.invoke(cmd, args);

let daemon = null; // { url, token }

function saved() {
  try {
    return JSON.parse(localStorage.getItem("layer8.daemon") || "null");
  } catch {
    return null;
  }
}

/** Resolves once the daemon is known; null means the browser needs a token. */
export async function connect() {
  if (tauri) {
    daemon = await invoke("daemon_info");
    return daemon;
  }
  // `make dev` opens the page with #token=…: remember it and take it out of the address bar.
  const given = new URLSearchParams(location.hash.slice(1)).get("token");
  if (given) {
    rememberToken(given);
    history.replaceState(null, "", location.pathname + location.search);
    return daemon;
  }
  const s = saved();
  daemon = s && s.token ? { url: location.origin, token: s.token } : null;
  return daemon;
}

export function rememberToken(token) {
  daemon = { url: location.origin, token };
  try {
    localStorage.setItem("layer8.daemon", JSON.stringify({ token }));
  } catch {
    /* private mode: ask again next time */
  }
}

export async function api(method, path, body) {
  const init = { method, headers: { Authorization: `Bearer ${daemon.token}` } };
  if (body !== undefined) {
    init.headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(body);
  }
  const res = await fetch(daemon.url + path, init);
  let data = null;
  try {
    data = await res.json();
  } catch {
    data = null;
  }
  if (!res.ok) throw new Error((data && data.error) || `HTTP ${res.status}`);
  return data;
}

export const get = (p) => api("GET", p);
/** Where a plugin module is served; import() can't send headers, so the token rides in the query. */
export const pluginURL = (name) => `${daemon.url}/api/plugins/${encodeURIComponent(name)}?token=${encodeURIComponent(daemon.token)}`;
export const post = (p, b = {}) => api("POST", p, b);
export const put = (p, b) => api("PUT", p, b);
export const del = (p) => api("DELETE", p);

/** Sends a file's bytes as the request body. */
export async function upload(path, file) {
  const res = await fetch(daemon.url + path, { method: "POST", headers: { Authorization: `Bearer ${daemon.token}` }, body: file });
  const data = await res.json().catch(() => null);
  if (!res.ok) throw new Error((data && data.error) || `HTTP ${res.status}`);
  return data;
}

const q = (o) =>
  Object.entries(o)
    .filter(([, v]) => v !== undefined && v !== null && v !== "")
    .map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(v)}`)
    .join("&");
export const withQuery = (path, o) => {
  const s = q(o);
  return s ? `${path}?${s}` : path;
};

// Server-sent events from the daemon. EventSource reconnects by itself (the daemon
// restarts after updates); onOpen lets the caller refetch state after a gap.
const handlers = { "layer8:state": new Set(), "layer8:events": new Set(), "layer8:ui": new Set() };
let source = null;
let openHandlers = new Set();

export function on(name, fn) {
  handlers[name].add(fn);
  return () => handlers[name].delete(fn);
}
export const onOpen = (fn) => {
  openHandlers.add(fn);
  return () => openHandlers.delete(fn);
};

export function startEvents(onStatus) {
  if (source) return;
  source = new EventSource(`${daemon.url}/api/events?token=${encodeURIComponent(daemon.token)}`);
  source.onopen = () => {
    onStatus(true);
    openHandlers.forEach((fn) => fn());
  };
  source.onerror = () => onStatus(false);
  for (const name of Object.keys(handlers)) {
    source.addEventListener(name, (e) => {
      let data = null;
      try {
        data = JSON.parse(e.data);
      } catch {
        return;
      }
      handlers[name].forEach((fn) => fn(data));
    });
  }
}

export const onSessionEvents = (fn) => on("layer8:events", (evs) => fn(evs || []));

// Shell-only features (no-ops in a browser).
export const shell = {
  available: !!tauri,
  pickFolder: () => (tauri ? invoke("pick_folder") : Promise.resolve(null)),
  checkUpdate: () => (tauri ? invoke("check_update") : Promise.resolve(null)),
  installUpdate: () => (tauri ? invoke("install_update") : Promise.resolve(null)),
  installCli: () => (tauri ? invoke("install_cli") : Promise.resolve(null)),
};

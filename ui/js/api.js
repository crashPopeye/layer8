// JSON API served by the Go app under its own origin, plus Wails events for pushes.

export async function api(method, path, body) {
  const init = { method, headers: {} };
  if (body !== undefined) {
    init.headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(body);
  }
  const res = await fetch(path, init);
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
export const post = (p, b = {}) => api("POST", p, b);
export const put = (p, b) => api("PUT", p, b);
export const del = (p) => api("DELETE", p);

const q = (o) =>
  Object.entries(o)
    .filter(([, v]) => v !== undefined && v !== null && v !== "")
    .map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(v)}`)
    .join("&");
export const withQuery = (path, o) => {
  const s = q(o);
  return s ? `${path}?${s}` : path;
};

// Wails runtime is only present inside the app window.
let runtime = null;
const runtimeReady = import("/wails/runtime.js").then(
  (m) => (runtime = m),
  () => null,
);

export async function on(name, cb) {
  await runtimeReady;
  if (!runtime) return () => {};
  return runtime.Events.On(name, (ev) => cb(ev.data));
}

// Session events pushed by Go, fanned out to whichever log is open.
const eventListeners = new Set();
export const onSessionEvents = (fn) => {
  eventListeners.add(fn);
  return () => eventListeners.delete(fn);
};
export const dispatchSessionEvents = (evs) => eventListeners.forEach((fn) => fn(evs || []));

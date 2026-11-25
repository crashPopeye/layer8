import { useEffect, useRef } from "/vendor/preact-htm.js";

/** combo names a keydown the way bindings are written: "mod+shift+n", "alt+y", "mod+1". Letters and
 * digits come from the physical key, so ⌥Y is "alt+y" even though it types "¥". */
function combo(e) {
  const key = /^(Key|Digit)/.test(e.code) ? e.code.slice(-1).toLowerCase() : e.key.toLowerCase();
  return [(e.metaKey || e.ctrlKey) && "mod", e.altKey && "alt", e.shiftKey && "shift", key].filter(Boolean).join("+");
}

/** useKeys runs bindings[combo] for each matching keydown anywhere in the window, and swallows the key. */
export function useKeys(bindings) {
  const current = useRef(bindings);
  current.current = bindings;
  useEffect(() => {
    const onKey = (e) => {
      const fn = current.current[combo(e)];
      if (!fn) return;
      e.preventDefault();
      fn(e);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
}

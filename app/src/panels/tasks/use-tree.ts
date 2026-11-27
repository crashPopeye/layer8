import { useSyncExternalStore } from "react";
import { useClient } from "@/lib/react";
import type { Client } from "@/lib/types";
import { loadTree, type TreeItem } from "./tree";

interface State {
  tree: TreeItem[];
  error: Error | null;
}

interface Store {
  state: State;
  subscribe: (s: () => void) => () => void;
}

const stores = new WeakMap<Client, Store>();

function storeFor(client: Client): Store {
  let store = stores.get(client);
  if (store) return store;
  const subscribers = new Set<() => void>();
  let latest = 0;
  let off: (() => void) | null = null;
  const set = (state: State) => {
    store!.state = state;
    subscribers.forEach((s) => s());
  };
  const load = () => {
    const n = ++latest;
    loadTree(client.getNode).then(
      (tree) => n === latest && set({ tree, error: null }),
      (error: Error) => n === latest && set({ ...store!.state, error }),
    );
  };
  store = {
    state: { tree: [], error: null },
    subscribe(s) {
      subscribers.add(s);
      if (!off) {
        load();
        off = client.subscribe((e) => {
          if (e.type === "node" && e.data.addrs.some((a) => a === "app" || a.startsWith("project:") || a.startsWith("task:"))) load();
        });
      }
      return () => {
        subscribers.delete(s);
        if (subscribers.size === 0) {
          off?.();
          off = null;
        }
      };
    },
  };
  stores.set(client, store);
  return store;
}

/** The project → task → sub-task tree with sessions, shared by every caller and read again when a project or task changes. */
export function useTree(): State {
  const store = storeFor(useClient());
  return useSyncExternalStore(store.subscribe, () => store.state);
}

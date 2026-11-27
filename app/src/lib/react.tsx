import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import type { Addr, Client, Depth, Node } from "./types";

const ClientContext = createContext<Client | null>(null);

/** Makes a Client available to useClient and useNode. */
export function ClientProvider({ client, children }: { client: Client; children: ReactNode }) {
  return <ClientContext.Provider value={client}>{children}</ClientContext.Provider>;
}

/** Returns the Client from the nearest ClientProvider. */
export function useClient(): Client {
  const client = useContext(ClientContext);
  if (!client) throw new Error("useClient outside ClientProvider");
  return client;
}

export interface NodeState {
  node: Node | null;
  error: Error | null;
}

/** Reads the node at addr and reads it again whenever a `node` event names it. */
export function useNode(addr: Addr, depth: Depth = 1): NodeState {
  const client = useClient();
  const [state, setState] = useState<NodeState>({ node: null, error: null });

  useEffect(() => {
    let live = true;
    const load = () =>
      client.getNode(addr, depth).then(
        (node) => live && setState({ node, error: null }),
        (error: Error) => live && setState({ node: null, error }),
      );
    load();
    const off = client.subscribe((e) => {
      if (e.type === "node" && e.data.addrs.includes(addr)) load();
    });
    return () => {
      live = false;
      off();
    };
  }, [client, addr, depth]);

  return state;
}

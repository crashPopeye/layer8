import { TriangleAlert } from "lucide-react";
import { Component, type ReactNode } from "react";

/** A card naming what failed and why. */
export function ErrorCard({ title, error }: { title: string; error: string }) {
  return (
    <div className="m-2 grid gap-1 rounded-md border border-bad/40 bg-bad/5 p-3 text-[12px]">
      <p className="flex items-center gap-1.5 font-medium text-bad">
        <TriangleAlert className="size-3.5" />
        {title}
      </p>
      <pre className="font-mono whitespace-pre-wrap text-muted-foreground">{error}</pre>
    </div>
  );
}

/** Renders children, or an error card once they throw while rendering. */
export class ErrorBoundary extends Component<
  { title: string; onError?: (error: Error) => void; children: ReactNode },
  { error: Error | null }
> {
  state = { error: null as Error | null };

  static getDerivedStateFromError(error: Error) {
    return { error };
  }

  componentDidCatch(error: Error) {
    this.props.onError?.(error);
  }

  render() {
    return this.state.error ? <ErrorCard title={this.props.title} error={String(this.state.error)} /> : this.props.children;
  }
}

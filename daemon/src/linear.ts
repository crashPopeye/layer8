import type { LinearIssueSummary } from "./types.js";

const endpoint = "https://api.linear.app/graphql";

export class LinearError extends Error {}

const summaryFields = `
  id identifier title url branchName priority updatedAt
  state { name type color }
  team { key }
`;

type RawSummary = Omit<LinearIssueSummary, "team"> & { team: { key: string } };

export type LinearIssueDetail = LinearIssueSummary & {
  description: string | null;
  assignee: string | null;
  labels: string[];
  parent: { identifier: string; title: string } | null;
  comments: { author: string; body: string; createdAt: string }[];
  attachments: { title: string; url: string }[];
};

export class Linear {
  constructor(private readonly apiKey: string) {}

  private async gql<T>(query: string, variables: Record<string, unknown> = {}): Promise<T> {
    const res = await fetch(endpoint, {
      method: "POST",
      // Personal API keys are sent without the Bearer prefix.
      headers: { "Content-Type": "application/json", Authorization: this.apiKey },
      body: JSON.stringify({ query, variables }),
    });
    const body = (await res.json().catch(() => null)) as { data?: T; errors?: { message: string }[] } | null;
    if (!res.ok || !body || body.errors?.length) {
      const msg = body?.errors?.map((e) => e.message).join("; ") ?? `HTTP ${res.status}`;
      throw new LinearError(`Linear: ${msg}`);
    }
    return body.data as T;
  }

  /** Open issues assigned to the viewer, most recently touched first. */
  async myIssues(): Promise<LinearIssueSummary[]> {
    const data = await this.gql<{ viewer: { assignedIssues: { nodes: RawSummary[] } } }>(
      `query {
        viewer {
          assignedIssues(
            first: 100
            orderBy: updatedAt
            filter: { state: { type: { nin: ["completed", "canceled"] } } }
          ) { nodes { ${summaryFields} } }
        }
      }`,
    );
    return data.viewer.assignedIssues.nodes.map(flatten);
  }

  async search(term: string): Promise<LinearIssueSummary[]> {
    const data = await this.gql<{ searchIssues: { nodes: RawSummary[] } }>(
      `query($term: String!) {
        searchIssues(term: $term, first: 30) { nodes { ${summaryFields} } }
      }`,
      { term },
    );
    return data.searchIssues.nodes.map(flatten);
  }

  /** Accepts a UUID or an identifier like "ENG-123". */
  async issue(id: string): Promise<LinearIssueDetail> {
    const data = await this.gql<{ issue: any }>(
      `query($id: String!) {
        issue(id: $id) {
          ${summaryFields}
          description
          assignee { name }
          labels { nodes { name } }
          parent { identifier title }
          comments(first: 100) { nodes { body createdAt user { name } } }
          attachments { nodes { title url } }
        }
      }`,
      { id },
    );
    const i = data.issue;
    return {
      ...flatten(i),
      description: i.description,
      assignee: i.assignee?.name ?? null,
      labels: i.labels.nodes.map((l: any) => l.name),
      parent: i.parent,
      comments: i.comments.nodes
        .map((c: any) => ({ author: c.user?.name ?? "unknown", body: c.body, createdAt: c.createdAt }))
        .sort((a: any, b: any) => a.createdAt.localeCompare(b.createdAt)),
      attachments: i.attachments.nodes,
    };
  }
}

const flatten = (i: RawSummary): LinearIssueSummary => ({ ...i, team: i.team.key });

/** The ticket as the agent reads it: written to the task's artifacts as ticket.md. */
export function ticketMarkdown(i: LinearIssueDetail): string {
  const lines = [
    `# ${i.identifier}: ${i.title}`,
    "",
    `- URL: ${i.url}`,
    `- State: ${i.state.name}`,
    `- Team: ${i.team}`,
    i.assignee ? `- Assignee: ${i.assignee}` : null,
    i.labels.length ? `- Labels: ${i.labels.join(", ")}` : null,
    i.parent ? `- Parent: ${i.parent.identifier} ${i.parent.title}` : null,
    "",
    "## Description",
    "",
    i.description?.trim() || "_No description._",
  ];
  if (i.attachments.length) {
    lines.push("", "## Links", "", ...i.attachments.map((a) => `- [${a.title}](${a.url})`));
  }
  if (i.comments.length) {
    lines.push("", "## Comments");
    for (const c of i.comments) lines.push("", `### ${c.author} (${c.createdAt.slice(0, 10)})`, "", c.body.trim());
  }
  return lines.filter((l) => l !== null).join("\n") + "\n";
}

---
name: tdd
description: Write the technical design for the task - architecture, contracts, data model and the decisions behind them - grounded in the research and the PRD. Writes NN-tdd-<slug>.md.
argument-hint: "[technical constraints]"
---

# Technical design

## 1. Orient

Read `layer8://here` (MCP server `layer8`) and the artifacts folder (`$LAYER8_ARTIFACTS`): the PRD defines *what*, the research defines *what exists*. Re-open the code the research points at; designs fail on details the summary skipped.

## 2. Design

- Fit the codebase: reuse its packages, patterns and conventions before inventing new ones.
- Prefer the design with the fewest moving parts that meets the PRD. Name the alternatives you rejected and why, in one line each.
- Decide the contracts first: API shapes, function signatures at package boundaries, tables and migrations, events. Write them as code blocks.
- Plan for failure: what happens on partial failure, retries, concurrency, and upgrade of existing data.

## 3. Write `NN-tdd-<slug>.md`

1. **Shape**: a Mermaid diagram of components and calls.
2. **Contracts**: types, endpoints, schemas, as code blocks.
3. **Decisions**: a table `concern | decision | why`.
4. **Example**: one request or job traced through the design with concrete values.
5. **Testing**: what is tested at which level.
6. **Risks**: what could make this wrong, and how we would notice.

Unanswerable questions go to the user with your recommendation; when unattended, take the recommendation and list it under Decisions.

## 4. Finish

Tell the user the file name and the 3 decisions that matter most. Then, if `advance` is listed on `here`:

```json
{"ops":[{"target":"here","action":"advance"}]}
```

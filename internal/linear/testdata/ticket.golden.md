# FLOW-1297: Media ingest retry

- URL: https://linear.app/scoreplay/issue/FLOW-1297
- State: In Progress
- Team: FLOW
- Assignee: Alex
- Project: Media pipeline
- Parent: FLOW-1200 Media ingest
- Labels: backend, reliability
- Branch: alex/flow-1297-media-ingest-retry

## Description

Retry failed ingests with backoff.

Keep the last error on the asset.

## Sub-issues

- FLOW-1298: Retry queue (Todo), branch `alex/flow-1298-retry-queue`
- FLOW-1299: Backoff policy (Backlog), branch `alex/flow-1299-backoff-policy`

## Attachments

- [Incident 2026-11-18](https://notion.so/incident-1118)

## Comments

### unknown (2026-11-20)

Seen on 3 clubs this week.

### Marie (2026-11-21)

Cap retries at 5.

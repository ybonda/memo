---
name: memo
description: Use when starting any task (proactively recall relevant memories), after debugging sessions, architecture decisions, or incidents (suggest storing insights), or when user says "remember this", "what do I know about", "search memories", "have I seen this before", "recall", "store this", "save this for later". Cross-project memory layer with local semantic search via MCP tools.
---

# memo

Cross-project persistent memory with local semantic search. All data on-machine at `~/.memo/`. Access via MCP tools.

## Proactive Workflow

### Phase 1: Recall (at task start)

At the START of every task, silently derive a search query from the user's request and recall using `memo_recall`:

- Surface only results with `score >= 0.70`
- Do NOT announce "searching memories" — silently recall, then naturally weave relevant results into your response
- If no results score >= 0.70, say nothing about memo

**Query derivation examples:**

| User says | Query |
| --------- | ----- |
| "Fix the CORS error in our API" | "CORS error API fix" |
| "Set up MongoDB connection pooling" | "MongoDB connection pooling configuration" |
| "Debug flaky test in CI" | "flaky test CI debugging" |

### Phase 2: Store (at task conclusion)

After resolving bugs, making architecture decisions, or handling incidents, compose a memory and ask before storing:

> "This seems worth remembering across projects — should I store it?"

If user agrees, use `memo_remember` with content, type, and tags.

**Trigger conditions for suggesting storage:**

- Debugged a non-obvious issue (root cause wasn't immediately apparent)
- Made an architecture decision with trade-offs
- Discovered a tool/library gotcha or version-specific behavior
- Found a workaround for a known limitation
- Resolved a production incident

## Auto-Tagging

First tag is always the project name (from `git remote` or directory name). Add 1-3 descriptive tags (technology, domain, pattern).

Example: `["ttsensei", "mongodb", "connection-pooling", "timeout"]`

## memo vs MEMORY.md

| Question | Answer |
|----------|--------|
| Would this help in a DIFFERENT project? | **memo** |
| Is this a project-specific convention? | **MEMORY.md** |
| Is this a reusable debugging pattern? | **memo** |
| Is this a build command or repo structure? | **MEMORY.md** |
| Is this a cross-cutting technology insight? | **memo** |
| Is this a team preference or workflow? | **MEMORY.md** |

**Rule of thumb:** "Would this help in a DIFFERENT project?" — Yes = memo, No = MEMORY.md.

## Memory Types

| Type | Use when... |
|------|-------------|
| `note` | General observations, ideas, WIP thoughts (default) |
| `bug` | Bug reports, error patterns, known issues |
| `incident` | Production incidents, outages, escalations |
| `architecture` | Architecture decisions, system design patterns |
| `ticket` | Tickets, tasks, action items, follow-ups |
| `postmortem` | Post-incident analysis, root causes, remediation steps |

## Handling Duplicates

When `memo_remember` returns `"status": "similar_exists"`:

1. Show the existing memory content to the user
2. Ask: "A similar memory already exists. Update it or keep both?"
3. If update: use `memo_update` with merged content
4. If keep: user explicitly confirms, then re-store with differentiated content

## Content Quality Rules

- **Self-contained**: Include enough context that the memory makes sense without the original conversation
- **Problem + Solution**: Always pair what went wrong with what fixed it
- **Include "why"**: Not just "use X" but "use X because Y fails under Z conditions"
- **One topic per memory**: Split multi-topic insights into separate memories
- **Specific names**: Use exact technology names and versions ("MongoDB 7.0 change streams" not "database streaming")
- **No ephemeral details**: Omit PR numbers, dates, session-specific paths
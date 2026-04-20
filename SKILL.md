---
name: memo
description: Use when starting any task (silently recall relevant memories) or when user says "remember this", "save this to memo", "what do I know about", "search memories", "have I seen this before", "recall", "store this", "forget", or asks to edit/list memories. Cross-project memory layer with local semantic search via MCP tools.
---

# memo

Cross-project persistent memory with local semantic search. All data on-machine at `~/.memo/`. Access via MCP tools.

## Phase 1: Recall (silent, at task start)

At the start of a **technical task**, derive a search query from the user's request and call `memo_recall` without narrating it.

- Skip recall entirely for conversational or meta turns ("hi", "/clear", "what did we just do", status questions).
- Surface only results with `score >= 0.70`. If nothing clears that bar, say nothing about memo at all.
- Never announce "searching memories" — weave relevant results into the response naturally, or stay silent.

**Query derivation examples:**

| User says | Query |
| --------- | ----- |
| "Fix the CORS error in our API" | "CORS error API fix" |
| "Set up MongoDB connection pooling" | "MongoDB connection pooling configuration" |
| "Debug flaky test in CI" | "flaky test CI debugging" |

## Phase 2: Ask before saving (rare, at task end)

**Do not save anything without explicit user consent.** Do not pre-compose or pitch a memory.

If and only if a task produced a **non-obvious, cross-project** insight, ask once with this exact short prompt:

> Do you want to save this to memo?

That's the entire prompt. No preamble, no summary of what would be saved, no "this seems worth remembering". The user decides.

**Ask only when all of these hold:**

- The insight is non-obvious (the root cause or resolution wasn't immediately apparent).
- It would help in a different project too, not just this repo.
- The user hasn't already declined saving earlier in the session.

**Stay silent when:**

- The fix was routine (typo, rename, obvious bug).
- The insight is project-local (build commands, repo layout, team workflow) — that belongs in `MEMORY.md`, not memo.
- You are unsure. When in doubt, do not ask. One save prompt per task, maximum.

## Saving When Asked

When the user says "save this to memo", "remember this", or answers yes to the Phase 2 prompt, **capture everything that would help future-you reconstruct the problem and solution without the original conversation**. A terse one-liner is a failure mode here.

Include in the memory body:

- **Problem** — the exact symptom, including error messages verbatim.
- **Root cause** — why it happens, not just what fixes it.
- **Fix** — the specific change, with a fenced code snippet for the key diff or reproducer.
- **Why the fix works** — the invariant or guarantee the fix relies on.
- **Specifics** — tool/library names and versions, relevant flags, commands, file paths if cross-project meaningful.
- **Gotchas** — edge cases or conditions where the fix does not apply.

Populate the `context` object on `memo_remember` with any of these that are known:

| Key | What to put there |
|-----|-------------------|
| `ticket` | Jira/issue key mentioned in session (e.g. `APP-1234`) |
| `pr` | PR URL if one is open or merged for this fix |
| `project` | Explicit project name (overrides git auto-capture when the fix is attributed to a different repo) |
| `related` | Memory ID (full UUID or 8-hex short-id) of a prior memo this extends |

Git auto-capture fills in `project` from the cwd automatically — explicit values override it on the same key.

Split multi-topic insights into **separate** `memo_remember` calls. One topic per memory.

If `memo_remember` returns `"status": "similar_exists"`, follow the **Handling Duplicates** flow below.

## MCP Tool Map

| Tool | Use when |
|------|----------|
| `memo_recall` | Start of a technical task — silent context gathering |
| `memo_search` | User explicitly asks "what do I know about X" |
| `memo_similar` | Pre-check before storing, to spot an existing near-duplicate |
| `memo_list` | User wants to browse memories by recency |
| `memo_remember` | User confirmed save |
| `memo_update` | Merging new details into an existing memory |
| `memo_forget` | User asks to delete (accepts full UUID or 8-hex short-id) |
| `memo_reconcile` | User edited the Obsidian vault (deleted or moved files) and wants the DB to reflect those structural changes |

## memo vs MEMORY.md

`memo` = cross-project insight (debugging patterns, tool gotchas, tech decisions that recur elsewhere). `MEMORY.md` = project-local convention (build commands, repo structure, team workflow). If in doubt: "would this help in a DIFFERENT project?" — yes = memo, no = MEMORY.md.

## Auto-Tagging

First tag is always the project name (from `git remote` or directory name). Add 1-3 descriptive tags (technology, domain, pattern).

Example: `["ttsensei", "mongodb", "connection-pooling", "timeout"]`

## Handling Duplicates

When `memo_remember` returns `"status": "similar_exists"`:

1. Show the existing memory content to the user.
2. Ask: "A similar memory already exists. Update it or keep both?"
3. If update: use `memo_update` with merged content.
4. If keep: user explicitly confirms, then re-store with differentiated content.

## Content Quality Rules

- **Self-contained**: enough context that the memory makes sense without the original conversation.
- **Problem + Solution**: always pair what went wrong with what fixed it.
- **Include "why"**: not just "use X" but "use X because Y fails under Z conditions".
- **One topic per memory**: split multi-topic insights into separate memories.
- **Specific names**: exact technology names and versions ("MongoDB 7.0 change streams" not "database streaming").
- **No ephemeral details**: omit session-specific paths, timestamps, or "I tried X then Y" narrative.

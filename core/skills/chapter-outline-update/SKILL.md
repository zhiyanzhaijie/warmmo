---
id: chapter-outline-update
name: Chapter Outline Update
version: 1.0.0
description: Update a chapter outline node with a chapter title, purpose, conflict, ordered beats, and ending state.
targets:
  - node-update:chapter-outline
allowed_tools:
  - canvas.get_nodes
  - workspace.search
  - story_spine.context
---
# Chapter Outline Update

Update the selected chapter outline in place.

- When the request asks for a next or continuing chapter, call `story_spine.context` with an empty query so results are ordered by recency. Use focused keywords only to recover a specific earlier fact.
- Every story-spine result has `contextRole: completed-chapter`. It is immutable history, never a draft or template for the target node. `recencyRank: 1` is the latest completed chapter.
- A next chapter must begin after the latest completed chapter's ending state and advance its unresolved hook. Do not reuse a completed chapter's title, purpose, conflict, event sequence, or ending as the new chapter output.
- Treat story-spine results as the compact source of archived progression. Call `canvas.get_nodes` only when a returned chapter node needs fuller current details.

- Set `title` to the chapter's actual title.
- Describe chapter purpose, viewpoint, opening state, central conflict, ordered event beats, character changes, ending state, and hook in `content`.
- Keep this as an outline rather than finished prose.
- Preserve all supplied entity, world, mechanism, and event facts.

Return exactly one JSON object with no markdown fence or commentary:

`{"title":"章节标题","content":"完整章节概览"}`

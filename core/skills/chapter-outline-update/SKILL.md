---
id: chapter-outline-update
name: Chapter Outline Update
version: 1.0.0
description: Update a chapter outline node with a chapter title, purpose, conflict, ordered beats, and ending state.
targets:
  - node-update:chapter-outline
allowed_tools:
  - canvas.get_nodes
---
# Chapter Outline Update

Update the selected chapter outline in place.

- Set `title` to the chapter's actual title.
- Describe chapter purpose, viewpoint, opening state, central conflict, ordered event beats, character changes, ending state, and hook in `content`.
- Keep this as an outline rather than finished prose.
- Preserve all supplied entity, world, mechanism, and event facts.

Return exactly one JSON object with no markdown fence or commentary:

`{"title":"章节标题","content":"完整章节概览"}`

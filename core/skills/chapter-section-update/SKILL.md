---
id: chapter-section-update
name: Chapter Section Update
version: 1.0.0
description: Write or revise one accepted chapter section while preserving its section outline and story continuity.
targets:
  - node-update:chapter-section
allowed_tools:
  - canvas.get_nodes
---
# Chapter Section Update

Write or revise the selected chapter section in place.

- Set `title` to the section's final readable title.
- Treat the linked section outline as the authoritative contract for purpose, beats, opening state, ending state, and target length.
- Read the target, linked section outline, and all context selected as relevant for continuity in one `canvas.get_nodes` call whenever their combined count is at most 64. Do not read selected context nodes one at a time by default.
- Produce complete narrative prose with concrete action, sensory detail, character intention, conflict, and causal progression.
- Preserve established world, character, event, location, mechanism, and timeline facts.
- Respect adjacent section continuity and do not duplicate work assigned to another section outline.

Return exactly one JSON object with no markdown fence or commentary:

`{"title":"子章节标题","content":"完整子章节正文"}`

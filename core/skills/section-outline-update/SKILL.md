---
id: section-outline-update
name: Section Outline Update
version: 1.0.0
description: Update one section outline with a precise narrative purpose, ordered beats, continuity boundaries, and target length.
targets:
  - node-update:section-outline
allowed_tools:
  - canvas.get_nodes
---
# Section Outline Update

Update the selected section outline in place.

- Set `title` to the section's working title.
- Define the section purpose, viewpoint, target length, opening state, ordered beats, conflict, turning point, ending state, and hook.
- Treat the parent chapter outline and established canvas facts as constraints.
- Read the target, parent chapter outline, and all context selected as relevant to those constraints in one `canvas.get_nodes` call whenever their combined count is at most 64. Do not read selected context nodes one at a time by default.
- Keep this as an actionable writing plan rather than finished prose.
- Make the opening and ending states explicit enough for adjacent sections to connect without rereading the entire chapter.

Return exactly one JSON object with no markdown fence or commentary:

`{"title":"子章节标题","content":"完整、可执行的子章节规划"}`

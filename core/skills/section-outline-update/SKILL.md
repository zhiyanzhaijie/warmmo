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
- Keep this as an actionable writing plan rather than finished prose.
- Make the opening and ending states explicit enough for adjacent sections to connect without rereading the entire chapter.

Return exactly one JSON object with no markdown fence or commentary:

`{"title":"子章节标题","content":"完整、可执行的子章节规划"}`

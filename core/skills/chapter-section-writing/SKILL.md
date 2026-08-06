---
id: chapter-section-writing
name: Chapter Section Writing
version: 1.0.0
description: Write and directly create one complete chapter section from a selected section outline and its supplied story context.
targets:
  - chapter-section
allowed_tools:
  - canvas.get_nodes
---
# Chapter Section Writing

Create one complete, editable chapter section node. The selected section outline is the parent writing contract.

## Workflow

1. Read the author request, the section outline, its parent chapter outline, and every continuity node inherited from that chapter outline.
2. Identify viewpoint, section purpose, opening state, active conflict, ordered beats, turning point, ending state, and hook.
3. Infer routine creative details from the request and context; do not ask for optional choices such as names or scene styling.
4. Treat the section outline as the authoritative writing contract for this section.
5. Write the complete section with concrete action, sensory detail, character intention, and causal progression.
6. Preserve facts from the canvas. When facts conflict, follow the most specific established fact and avoid inventing a contradiction.
7. Keep planning and observations out of the final prose.

The created chapter-section node must retain edges from the section outline, chapter outline, and every inherited continuity node used by this run.

## Completion

Complete only when the chapter section:

- fulfills the section purpose and required beats;
- respects supplied character, plot, worldbuilding, and timeline facts;
- reaches the specified ending state with a clear causal progression;
- introduces no unsupported permanent change to established facts;
- is ready to be persisted as a chapter-section node.

Return exactly one JSON object with no markdown fence or commentary:

`{"title":"小节标题","content":"完整正文"}`

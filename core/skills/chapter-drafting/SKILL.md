---
id: chapter-drafting
name: Chapter Drafting
version: 1.0.0
description: Draft a section candidate from an author request and selected entity, event, world, mechanism, or chapter outline nodes. Use for writing a new scene or section while preserving established canvas facts.
targets:
  - section-draft
allowed_tools:
  - canvas.get_nodes
  - canvas.create_candidate
---
# Chapter Drafting

Create an editable section draft candidate. Never replace an accepted node or revision.

## Workflow

1. Read the author request and every supplied canvas node.
2. Identify viewpoint, scene goal, active conflict, relevant characters, setting constraints, and continuity requirements.
3. This is a single-request API. Infer omitted creative details from the request and context; do not ask for routine choices such as gender, age, occupation, names, or scene styling.
4. Form a compact scene plan before drafting when the request contains multiple beats or constraints.
5. Draft the chapter with concrete action, sensory detail, character intention, and causal progression.
6. Preserve facts from the canvas. When facts conflict, follow the most specific established fact and avoid inventing a contradiction; do not stop for optional clarification.
7. Produce prose only in the final candidate. Keep planning and observations out of the chapter text.

## Completion

Complete only when the candidate:

- fulfills the requested scene purpose;
- respects supplied character, plot, worldbuilding, and timeline facts;
- has clear scene progression rather than summarizing events;
- introduces no unsupported permanent change to established facts;
- is ready to enter the canvas as a candidate revision.

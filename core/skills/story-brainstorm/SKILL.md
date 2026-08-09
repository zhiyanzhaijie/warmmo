---
id: story-brainstorm
name: Story Brainstorm
version: 1.0.0
description: Explore a large canvas and story spine to find evidence-backed relationships, unresolved hooks, conflicts, and creative directions.
targets:
  - collaborative-explore
allowed_tools:
  - canvas.search_context
  - canvas.get_nodes
  - story_spine.context
---
# Story Brainstorm

This is a read-only exploration task. Do not create or update nodes. Its result is advice, never a ProposalSet.

Return the final answer as natural-language prose, not JSON, code, or a schema dump. Organize a small number of strong, evidence-backed directions with short headings and readable paragraphs or bullet points. For each direction, explain the idea, evidence from the canvas, dramatic tension, possible payoff, required new nodes, and risks when relevant.

Every direction must distinguish existing facts from speculation. Never expose an internal field name such as `directions`, `evidenceNodeIds`, or `requiredNewNodes` as the response format.

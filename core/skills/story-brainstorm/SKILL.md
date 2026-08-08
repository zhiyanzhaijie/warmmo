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

Return exactly one JSON object:

```json
{
  "directions": [
    {
      "title": "方向标题",
      "idea": "可展开的创作方向",
      "evidenceNodeIds": [],
      "archiveIds": [],
      "tension": "戏剧张力",
      "payoff": "可能回收的伏笔",
      "requiredNewNodes": [],
      "risks": []
    }
  ]
}
```

Every direction must distinguish existing facts from speculation. Prefer a small number of strong, evidence-backed directions over a long list of generic ideas.

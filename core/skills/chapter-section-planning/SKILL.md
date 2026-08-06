---
id: chapter-section-planning
name: Chapter Section Planning
version: 1.0.0
description: Split one chapter outline into a coherent ordered batch of section outline nodes sized for the requested fiction format.
targets:
  - section-outline-batch
allowed_tools:
  - canvas.get_nodes
---
# Chapter Section Planning

Split the selected chapter outline into the smallest coherent sequence of writable section plans.

## Planning rules

1. Treat the selected chapter outline as the complete and authoritative planning contract. This phase evaluates only how the chapter should be divided.
2. Infer the appropriate section count from the chapter's events, viewpoint transitions, scene boundaries, pacing, and requested form. Do not force a fixed count.
3. Keep one dominant narrative purpose and one continuous viewpoint per section. Split when time, place, viewpoint, dramatic objective, or aftermath changes materially.
4. Avoid microscopic sections that contain only one trivial beat and oversized sections that combine multiple independent dramatic turns.
5. Make adjacent `endingState` and `openingState` compatible so later writing can proceed section by section without rereading the whole work.
6. Assign contiguous ordinals starting at 1. Use a realistic positive `targetLength` in Chinese characters for the requested short, medium, or long-form work.
7. Do not select or attach character, world, event, mechanism, or other writing context to section outline nodes. Those facts are inherited later when writing the chapter section.
8. Ask the user only when the chapter outline itself makes every viable split contradictory. Infer ordinary pacing and stylistic choices.

## Output contract

Return exactly one JSON object with no markdown fence or commentary. `chapterOutlineNodeId` must equal the selected target node ID.

```json
{
  "chapterOutlineNodeId": "selected-node-id",
  "sections": [
    {
      "title": "子章节标题",
      "outline": {
        "ordinal": 1,
        "purpose": "这一小节必须完成的叙事任务",
        "viewpoint": "视角角色及叙述限制",
        "targetLength": 1800,
        "openingState": "开始时人物、关系和局势状态",
        "beats": ["按因果顺序排列的动作或信息节拍"],
        "conflict": "推动本节的即时冲突",
        "turningPoint": "改变局势或理解的关键转折",
        "endingState": "结束时已经改变的状态",
        "hook": "推动读者进入下一节的悬念或动力"
      }
    }
  ]
}
```

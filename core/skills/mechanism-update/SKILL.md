---
id: mechanism-update
name: Mechanism Update
version: 1.0.0
description: Update a mechanism node with explicit triggers, effects, costs, limits, and edge cases.
targets:
  - node-update:mechanism
allowed_tools:
  - canvas.get_nodes
---
# Mechanism Update

Update the selected mechanism in place.

- Set `title` to the mechanism's canonical name.
- Define trigger, prerequisites, process, result, cost, limitations, failure modes, edge cases, and observable consequences in `content`.
- Prefer testable rules over vague language.
- Respect real-world laws unless the world context explicitly defines an exception.

Return exactly one JSON object with no markdown fence or commentary:

`{"title":"机制名称","content":"完整机制规则"}`

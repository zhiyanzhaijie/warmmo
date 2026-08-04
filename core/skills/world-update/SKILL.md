---
id: world-update
name: World Update
version: 1.0.0
description: Update a root or domain world node with explicit scope and stable world facts.
targets:
  - node-update:world
allowed_tools:
  - canvas.get_nodes
---
# World Update

Update the selected world node in place.

- Set `title` to the canonical world or world-domain name.
- State whether the node describes a root world or a specific domain.
- Describe scope, foundational facts, geography, history, society, culture, and governing constraints relevant to that scope.
- Keep detailed mechanisms in mechanism nodes; reference them without duplicating contradictory rules.
- Treat established facts as authoritative.

Return exactly one JSON object with no markdown fence or commentary:

`{"title":"世界观名称","content":"完整世界观设定"}`

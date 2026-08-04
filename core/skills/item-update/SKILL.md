---
id: item-update
name: Item Update
version: 1.0.0
description: Update an item node with a canonical name, concrete properties, ownership, constraints, and narrative relevance.
targets:
  - node-update:item
allowed_tools:
  - canvas.get_nodes
---
# Item Update

Update the selected item in place.

- Set `title` to the item's canonical name.
- Describe appearance, material, condition, origin, owner, function, limitations, and narrative significance in `content`.
- Keep capabilities consistent with the story's world and mechanisms.
- Do not turn the item into an event or prose scene.

Return exactly one JSON object with no markdown fence or commentary:

`{"title":"物品名称","content":"完整物品设定"}`

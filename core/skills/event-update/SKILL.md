---
id: event-update
name: Event Update
version: 1.0.0
description: Update an event node with participants, time, location, causes, progression, outcome, and consequences.
targets:
  - node-update:event
allowed_tools:
  - canvas.get_nodes
---
# Event Update

Update the selected event in place.

- Set `title` to a concise canonical event name.
- Describe participants, time, location, preconditions, causes, chronological progression, outcome, and consequences in `content`.
- Maintain clear causality and realistic behavior.
- Do not turn the event node into finished narrative prose.

Return exactly one JSON object with no markdown fence or commentary:

`{"title":"事件名称","content":"完整事件定义"}`

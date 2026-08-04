---
id: time-update
name: Time Update
version: 1.0.0
description: Update a reusable time node representing an era, period, deadline, or significant moment.
targets:
  - node-update:time
allowed_tools:
  - canvas.get_nodes
---
# Time Update

Update the selected time node in place.

- Set `title` to the canonical name of the era, period, date, or moment.
- Describe temporal range, position in chronology, social or environmental conditions, constraints, and related events in `content`.
- Keep dates and causal ordering internally consistent.
- Do not invent a timeline conflict with supplied context.

Return exactly one JSON object with no markdown fence or commentary:

`{"title":"时间名称","content":"完整时间设定"}`

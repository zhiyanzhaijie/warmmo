---
id: location-update
name: Location Update
version: 1.0.0
description: Update a location node as a reusable spatial entity and creation context.
targets:
  - node-update:location
allowed_tools:
  - canvas.get_nodes
---
# Location Update

Update the selected location in place.

- Set `title` to the location's canonical place name.
- Describe physical layout, environment, atmosphere, access, inhabitants, social function, history, and practical constraints in `content`.
- Make the location reusable as context for characters and events.
- Respect real-world rules unless an established world or mechanism explicitly overrides them.

Return exactly one JSON object with no markdown fence or commentary:

`{"title":"地点名称","content":"完整地点设定"}`

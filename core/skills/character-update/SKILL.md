---
id: character-update
name: Character Update
version: 1.0.0
description: Update a character node with a canonical name and a coherent character profile.
targets:
  - node-update:character
allowed_tools:
  - canvas.get_nodes
---
# Character Update

Update the selected character in place.

- Set `title` to the character's actual canonical name. Never keep placeholder titles such as "未命名角色" when the request establishes a character.
- Put identity, ordinary background, personality, motivation, resilience, limitations, relationships, and behavioral evidence in `content`.
- Prefer concrete traits demonstrated through choices and reactions over abstract adjectives.
- Respect realistic constraints and all established canvas facts.
- Infer routine omitted details without asking follow-up questions.

Return exactly one JSON object with no markdown fence or commentary:

`{"title":"角色姓名","content":"完整角色设定"}`

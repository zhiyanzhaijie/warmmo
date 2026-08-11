package writing

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCandidatePromptCarriesOnlyNodeRoutingMetadata(t *testing.T) {
	t.Parallel()

	state := loopState{
		input: RunInput{
			Prompt:             "补充角色 C 的秘密，但保留已有设定",
			Target:             NodeUpdateTarget("character"),
			TargetNodeID:       "character-c",
			TargetNodeType:     "character",
			TargetNodeRevision: 3,
			ContextNodeIDs:     []string{"character-c", "character-b", "world-a", "unavailable", "character-b"},
			ContextNodes: []NodeReference{
				{ID: "world-a", Type: "world"},
				{ID: "character-b", Type: "character"},
			},
		},
	}

	payload := decodePromptPayload(t, state.candidatePrompt())
	if operation := decodeString(t, payload["operation"]); operation != "update_existing_node" {
		t.Fatalf("operation = %q", operation)
	}
	assertNodeReference(t, payload["targetNode"], NodeReference{ID: "character-c", Type: "character"})
	assertNodeReferenceList(t, payload["availableContextNodes"], []NodeReference{
		{ID: "world-a", Type: "world"},
		{ID: "character-b", Type: "character"},
	})

	var priorityNodeIDs []string
	if err := json.Unmarshal(payload["priorityContextNodeIds"], &priorityNodeIDs); err != nil {
		t.Fatalf("decode priority context node IDs: %v", err)
	}
	wantPriorityNodeIDs := []string{"character-b", "world-a"}
	if strings.Join(priorityNodeIDs, ",") != strings.Join(wantPriorityNodeIDs, ",") {
		t.Fatalf("priority context node IDs = %v, want %v", priorityNodeIDs, wantPriorityNodeIDs)
	}
	for _, legacyField := range []string{"referenceContext", "availableAttachments", "attachmentPolicy"} {
		if _, exists := payload[legacyField]; exists {
			t.Fatalf("legacy full-context field %q is still present", legacyField)
		}
	}
}

func TestDerivationPromptIncludesTypedNodeReferences(t *testing.T) {
	t.Parallel()

	state := loopState{
		input: RunInput{
			Prompt:         "撰写这一节正文",
			Target:         TargetChapterSection,
			TargetNodeID:   "section-1",
			TargetNodeType: "section-outline",
			ContextNodes: []NodeReference{
				{ID: "chapter-1", Type: "chapter-outline"},
				{ID: "world-1", Type: "world"},
			},
		},
	}

	payload := decodePromptPayload(t, state.candidatePrompt())
	if operation := decodeString(t, payload["operation"]); operation != "derive_child_nodes" {
		t.Fatalf("operation = %q", operation)
	}
	assertNodeReference(t, payload["targetNode"], NodeReference{ID: "section-1", Type: "section-outline"})
	assertNodeReferenceList(t, payload["availableContextNodes"], []NodeReference{
		{ID: "chapter-1", Type: "chapter-outline"},
		{ID: "world-1", Type: "world"},
	})
	if _, exists := payload["priorityContextNodeIds"]; exists {
		t.Fatal("derivation prompt must not expose node-update priority references")
	}
}

func TestCandidatePromptReplacesNodeWithoutLeakingItsContent(t *testing.T) {
	t.Parallel()

	state := loopState{
		input: RunInput{
			Prompt:         "创建一个新角色。新的！",
			Target:         NodeUpdateTarget("character"),
			TargetNodeID:   "character-b",
			TargetNodeType: "character",
			UserResponses: []UserResponse{{
				Question: "希望什么样的角色？", Answer: "女性，男主的朋友",
			}},
		},
	}

	payload := decodePromptPayload(t, state.candidatePrompt())
	if mode := decodeString(t, payload["mutationMode"]); mode != "replace" {
		t.Fatalf("mutation mode = %q", mode)
	}
	assertNodeReference(t, payload["targetNode"], NodeReference{ID: "character-b", Type: "character"})
	assertNodeReferenceList(t, payload["availableContextNodes"], []NodeReference{})
}

func TestNodeUpdateModeKeepsOrdinaryCharacterEditsAsMerge(t *testing.T) {
	t.Parallel()

	input := RunInput{Prompt: "补充角色的童年秘密", Target: NodeUpdateTarget("character")}
	if mode := nodeUpdateMode(input); mode != "merge" {
		t.Fatalf("node update mode = %q", mode)
	}
}

func decodePromptPayload(t *testing.T, prompt string) map[string]json.RawMessage {
	t.Helper()
	payload := make(map[string]json.RawMessage)
	if err := json.Unmarshal([]byte(prompt), &payload); err != nil {
		t.Fatalf("decode prompt: %v", err)
	}
	return payload
}

func decodeString(t *testing.T, value json.RawMessage) string {
	t.Helper()
	var decoded string
	if err := json.Unmarshal(value, &decoded); err != nil {
		t.Fatalf("decode string: %v", err)
	}
	return decoded
}

func assertNodeReference(t *testing.T, value json.RawMessage, want NodeReference) {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(value, &fields); err != nil {
		t.Fatalf("decode node reference fields: %v", err)
	}
	if len(fields) != 2 {
		t.Fatalf("node reference fields = %v, want only id and type", mapsKeys(fields))
	}
	if _, exists := fields["id"]; !exists {
		t.Fatal("node reference is missing id")
	}
	if _, exists := fields["type"]; !exists {
		t.Fatal("node reference is missing type")
	}
	var got NodeReference
	if err := json.Unmarshal(value, &got); err != nil {
		t.Fatalf("decode node reference: %v", err)
	}
	if got != want {
		t.Fatalf("node reference = %+v, want %+v", got, want)
	}
}

func assertNodeReferenceList(t *testing.T, value json.RawMessage, want []NodeReference) {
	t.Helper()
	var references []json.RawMessage
	if err := json.Unmarshal(value, &references); err != nil {
		t.Fatalf("decode node references: %v", err)
	}
	if len(references) != len(want) {
		t.Fatalf("node reference count = %d, want %d", len(references), len(want))
	}
	for index := range want {
		assertNodeReference(t, references[index], want[index])
	}
}

func mapsKeys(values map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

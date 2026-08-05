package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCandidatePromptSeparatesNodeUpdateTargetFromReferences(t *testing.T) {
	t.Parallel()

	state := loopState{
		input: RunInput{
			Prompt:       "补充角色 C 的秘密，但保留已有设定",
			Target:       NodeUpdateTarget("character"),
			TargetNodeID: "character-c",
		},
		snapshot: ContextSnapshot{
			ID: "snapshot-1", WorkID: "work-1",
			Nodes: []NodeSnapshot{
				{ID: "world-a", Type: "world", Title: "世界观 A", Content: "世界规则"},
				{ID: "character-b", Type: "character", Title: "角色 B", Content: "关联角色"},
				{ID: "character-c", Revision: "3", Type: "character", Title: "角色 C", Content: "角色 C 的原有完整设定"},
			},
		},
	}

	var payload struct {
		Operation        string          `json:"operation"`
		TargetNodeID     string          `json:"targetNodeId"`
		TargetNode       NodeSnapshot    `json:"targetNode"`
		ReferenceContext ContextSnapshot `json:"referenceContext"`
		UpdatePolicy     []string        `json:"updatePolicy"`
	}
	if err := json.Unmarshal([]byte(state.candidatePrompt()), &payload); err != nil {
		t.Fatalf("decode candidate prompt: %v", err)
	}
	if payload.Operation != "update_existing_node" || payload.TargetNodeID != "character-c" {
		t.Fatalf("unexpected target metadata: %+v", payload)
	}
	if payload.TargetNode.ID != "character-c" || payload.TargetNode.Content != "角色 C 的原有完整设定" {
		t.Fatalf("target node = %+v", payload.TargetNode)
	}
	if len(payload.ReferenceContext.Nodes) != 2 {
		t.Fatalf("reference nodes = %+v", payload.ReferenceContext.Nodes)
	}
	for _, node := range payload.ReferenceContext.Nodes {
		if node.ID == payload.TargetNodeID {
			t.Fatalf("target node leaked into reference context: %+v", node)
		}
	}
	if len(payload.UpdatePolicy) == 0 {
		t.Fatal("update policy is empty")
	}
}

func TestNodeUpdateSystemPromptRequiresPreservingExistingContent(t *testing.T) {
	t.Parallel()

	prompt := nodeUpdateSystemPrompt("Character skill instructions.", "merge")
	for _, required := range []string{"authoritative baseline", "Preserve", "complete merged title and content"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("system prompt does not contain %q: %s", required, prompt)
		}
	}
}

func TestCandidatePromptReplacesCorruptedCharacterWithoutLeakingOldIdentity(t *testing.T) {
	t.Parallel()

	state := loopState{
		input: RunInput{
			Prompt:       "创建一个新角色。新的！",
			Target:       NodeUpdateTarget("character"),
			TargetNodeID: "character-b",
			UserResponses: []UserResponse{{
				Question: "希望什么样的角色？", Answer: "女性，男主的朋友",
			}},
		},
		snapshot: ContextSnapshot{
			ID: "snapshot-1", WorkID: "work-1",
			Nodes: []NodeSnapshot{
				{ID: "character-a", Revision: "8", Type: "character", Title: "林深", Content: "男主设定"},
				{ID: "character-b", Revision: "7", Type: "character", Title: "林深", Content: "被错误复制的男主设定"},
			},
		},
	}

	var payload struct {
		MutationMode string `json:"mutationMode"`
		TargetNode   struct {
			ID       string `json:"id"`
			Revision string `json:"revision"`
			Title    string `json:"title"`
			Content  string `json:"content"`
		} `json:"targetNode"`
	}
	if err := json.Unmarshal([]byte(state.candidatePrompt()), &payload); err != nil {
		t.Fatalf("decode replacement candidate prompt: %v", err)
	}
	if payload.MutationMode != "replace" {
		t.Fatalf("mutation mode = %q", payload.MutationMode)
	}
	if payload.TargetNode.ID != "character-b" || payload.TargetNode.Revision != "7" {
		t.Fatalf("unexpected target metadata: %+v", payload.TargetNode)
	}
	if payload.TargetNode.Title != "" || payload.TargetNode.Content != "" {
		t.Fatalf("old target identity leaked into replacement prompt: %+v", payload.TargetNode)
	}
	prompt := nodeUpdateSystemPrompt("Character skill instructions.", "replace")
	for _, required := range []string{"completely new node concept", "Do not preserve", "previous semantic content must be replaced"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("replacement system prompt does not contain %q: %s", required, prompt)
		}
	}
}

func TestNodeUpdateModeKeepsOrdinaryCharacterEditsAsMerge(t *testing.T) {
	t.Parallel()

	input := RunInput{Prompt: "补充角色的童年秘密", Target: NodeUpdateTarget("character")}
	if mode := nodeUpdateMode(input); mode != "merge" {
		t.Fatalf("node update mode = %q", mode)
	}
}

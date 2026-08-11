package writing

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

func ParseCollaborationPlan(value, runTarget string) (CollaborationPlan, error) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "```json")
	value = strings.TrimPrefix(value, "```")
	value = strings.TrimSuffix(value, "```")
	value = strings.TrimSpace(value)
	var plan CollaborationPlan
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return CollaborationPlan{}, fmt.Errorf("decode collaboration plan: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return CollaborationPlan{}, errors.New("collaboration plan must contain exactly one JSON object")
	}
	if strings.TrimSpace(plan.Intent) == "" || strings.TrimSpace(plan.Brief) == "" {
		return CollaborationPlan{}, errors.New("collaboration plan requires intent and brief")
	}
	if strings.TrimSpace(plan.CreatorSkillID) == "" {
		return CollaborationPlan{}, errors.New("collaboration plan requires creatorSkillId")
	}
	switch plan.OutputKind {
	case "proposal", "advice", "prose":
	default:
		return CollaborationPlan{}, errors.New("collaboration plan outputKind must be proposal, advice, or prose")
	}
	if plan.WriterRequired && plan.OutputKind != "prose" {
		return CollaborationPlan{}, errors.New("writerRequired is only valid for prose output")
	}
	if runTarget == TargetCollaborativeExplore || plan.OutputKind == "advice" {
		if plan.CreatorSkillID != "story-brainstorm" || plan.OutputKind != "advice" || plan.WriterRequired {
			return CollaborationPlan{}, errors.New("divergent exploration requires story-brainstorm advice without a writer")
		}
		plan.CreatorTarget = TargetCollaborativeExplore
		return plan, nil
	}
	if strings.TrimSpace(plan.CreatorTarget) == "" {
		plan.CreatorTarget = TargetCollaborativeTargeted
	}
	if plan.CreatorTarget != TargetCollaborativeTargeted {
		return CollaborationPlan{}, fmt.Errorf("collaboration plan creatorTarget must be %q", TargetCollaborativeTargeted)
	}
	if plan.OutputKind == "proposal" {
		if plan.CreatorSkillID != "chapter-creator" && plan.CreatorSkillID != "entity-creator" {
			return CollaborationPlan{}, errors.New("proposal output requires chapter-creator or entity-creator")
		}
		if plan.WriterRequired {
			return CollaborationPlan{}, errors.New("proposal output cannot request a writer")
		}
	}
	if plan.OutputKind == "prose" && plan.CreatorSkillID != "prose-creator" {
		return CollaborationPlan{}, errors.New("prose output requires prose-creator")
	}
	return plan, nil
}

func ValidateProposalSet(value string) error {
	if !json.Valid([]byte(value)) {
		return errors.New("proposal set must be valid JSON")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(value), &fields); err != nil || fields == nil {
		return errors.New("proposal set must be a JSON object")
	}
	for _, field := range []string{"baseRevisions", "nodes", "updates", "edges", "reasons", "questions"} {
		if _, exists := fields[field]; !exists {
			return fmt.Errorf("proposal set requires top-level field %q", field)
		}
	}
	var proposal ProposalSet
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&proposal); err != nil {
		return fmt.Errorf("decode proposal set: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("proposal set must contain exactly one JSON object")
	}
	if len(proposal.Nodes) == 0 && len(proposal.Updates) == 0 {
		return errors.New("proposal set must contain at least one node or update")
	}
	if len(proposal.Nodes) > 1 {
		return errors.New("proposal set must contain at most one node per generation")
	}
	for index, node := range proposal.Nodes {
		if strings.TrimSpace(node.ClientID) == "" || strings.TrimSpace(node.Kind) == "" ||
			strings.TrimSpace(node.Title) == "" || strings.TrimSpace(node.Content) == "" {
			return fmt.Errorf("proposal node %d requires clientId, kind, title, and content", index)
		}
	}
	for index, update := range proposal.Updates {
		if strings.TrimSpace(update.NodeID) == "" || update.BaseRevision < 1 ||
			strings.TrimSpace(update.Title) == "" || strings.TrimSpace(update.Content) == "" {
			return fmt.Errorf("proposal update %d requires nodeId, positive baseRevision, title, and content", index)
		}
	}
	for index, edge := range proposal.Edges {
		if strings.TrimSpace(edge.SourceID) == "" || strings.TrimSpace(edge.TargetID) == "" || strings.TrimSpace(edge.Kind) == "" {
			return fmt.Errorf("proposal edge %d requires sourceId, targetId, and kind", index)
		}
	}
	return nil
}

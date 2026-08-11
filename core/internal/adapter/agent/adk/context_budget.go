package adk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/model"
	"google.golang.org/genai"

	appharness "warmmo/core/internal/application/harness"
)

const (
	compactionArtifactKind = "context_compaction_v1"
	maxRecallBytes         = 6 * 1024
	maxSummaryBytes        = 4 * 1024
)

type contextBudgeter struct {
	policy    appharness.ContextPolicy
	memory    appharness.MemoryPolicy
	memories  appharness.MemoryStore
	artifacts appharness.ArtifactStore
	runID     string
	turnID    string
	agentID   string
	workID    string
	query     string
	persist   func(context.Context, json.RawMessage) error

	recallMu        sync.Mutex
	memoryFrozen    bool
	frozenMemoryIDs []string
	recallLoaded    bool
	recalled        []appharness.MemoryRecord
	recallErr       error
}

func (b *contextBudgeter) beforeModel(ctx agent.CallbackContext, request *model.LLMRequest) (*model.LLMResponse, error) {
	if request == nil {
		return nil, errors.New("LLM request is required for context budgeting")
	}
	if err := validateContextPolicy(b.policy); err != nil {
		return nil, err
	}
	recalled, recallContent, err := b.recall(ctx)
	if err != nil {
		return nil, err
	}
	overhead := estimateRequestOverhead(request)
	available := b.policy.ModelWindowTokens - b.policy.ReservedOutputTokens - b.policy.SafetyMarginTokens - overhead
	if recallContent != nil {
		available -= estimateContentTokens(recallContent)
	}
	if available < 1024 {
		return nil, fmt.Errorf("%w: system and tool declarations leave %d tokens", appharness.ErrContextBudgetExceeded, available)
	}
	before := overhead + estimateContentsTokens(request.Contents)
	if recallContent != nil {
		before += estimateContentTokens(recallContent)
	}
	compacted, removed, hashes, summary, err := compactContents(request.Contents, available, b.policy.RecentContents)
	if err != nil {
		return nil, err
	}
	var summaryRef *appharness.ArtifactRef
	if removed > 0 {
		artifact, err := b.saveCompaction(ctx, hashes, summary, before, available)
		if err != nil {
			return nil, err
		}
		summaryRef = &artifact.Ref
		compacted[0] = compactionContent(summary, artifact.Ref)
	}
	if recallContent != nil {
		compacted = append([]*genai.Content{recallContent}, compacted...)
	}
	after := overhead + estimateContentsTokens(compacted)
	if after > b.policy.ModelWindowTokens-b.policy.ReservedOutputTokens-b.policy.SafetyMarginTokens {
		return nil, fmt.Errorf("%w after compaction: estimated=%d", appharness.ErrContextBudgetExceeded, after)
	}
	request.Contents = compacted
	recallPerformed := b.recallEnabled()
	if removed > 0 || recallPerformed {
		memoryIDs := make([]string, len(recalled))
		for index, memory := range recalled {
			memoryIDs[index] = memory.ID
		}
		manifest := appharness.CompactionManifest{
			Version: "1", Estimator: "conservative_utf8_v1",
			EstimatedTokensBefore: before, EstimatedTokensAfter: after,
			AvailableTokens: available, RemovedContents: removed,
			RemovedContentHashes: hashes, SummaryArtifact: summaryRef,
			MemoryRecallPerformed: recallPerformed, RecalledMemoryIDs: memoryIDs,
			CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
		}
		encoded, err := json.Marshal(manifest)
		if err != nil {
			return nil, fmt.Errorf("encode compaction manifest: %w", err)
		}
		if b.persist != nil {
			if err := b.persist(ctx, encoded); err != nil {
				return nil, fmt.Errorf("persist compaction manifest: %w", err)
			}
		}
	}
	return nil, nil
}

func (b *contextBudgeter) recall(ctx context.Context) ([]appharness.MemoryRecord, *genai.Content, error) {
	if !b.recallEnabled() {
		return nil, nil, nil
	}
	b.recallMu.Lock()
	defer b.recallMu.Unlock()
	if !b.recallLoaded {
		if b.memoryFrozen {
			b.recalled, b.recallErr = b.memories.Load(ctx, b.workID, b.frozenMemoryIDs)
		} else {
			b.recalled, b.recallErr = b.memories.Recall(ctx, appharness.MemoryRecallQuery{
				WorkID: b.workID, Query: b.query, Limit: 6,
			})
		}
		b.recallLoaded = true
	}
	if b.recallErr != nil {
		return nil, nil, fmt.Errorf("recall agent memory: %w", b.recallErr)
	}
	if len(b.recalled) == 0 {
		return nil, nil, nil
	}
	memories := append([]appharness.MemoryRecord(nil), b.recalled...)
	var builder strings.Builder
	builder.WriteString("# Recalled Memory\nThese are prior non-authoritative notes. Verify them against current canvas evidence before relying on them.\n")
	kept := make([]appharness.MemoryRecord, 0, len(memories))
	for _, memory := range memories {
		line := fmt.Sprintf("- [%s:%s] %s\n", memory.Kind, memory.ID, strings.TrimSpace(memory.Content))
		if builder.Len()+len(line) > maxRecallBytes {
			break
		}
		builder.WriteString(line)
		kept = append(kept, memory)
	}
	if len(kept) == 0 {
		return nil, nil, nil
	}
	return kept, genai.NewContentFromText(strings.TrimSpace(builder.String()), genai.RoleUser), nil
}

func (b *contextBudgeter) recallEnabled() bool {
	return b.memory.Recall && b.policy.Memory == "recall" && b.memories != nil && strings.TrimSpace(b.workID) != ""
}

func frozenRecallFromManifest(encoded json.RawMessage) (bool, []string, error) {
	if len(encoded) == 0 || string(encoded) == "null" {
		return false, nil, nil
	}
	var manifest appharness.CompactionManifest
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		return false, nil, fmt.Errorf("decode checkpoint compaction manifest: %w", err)
	}
	frozen := manifest.MemoryRecallPerformed || len(manifest.RecalledMemoryIDs) > 0
	return frozen, append([]string(nil), manifest.RecalledMemoryIDs...), nil
}

func compactContents(contents []*genai.Content, available, recent int) ([]*genai.Content, int, []string, string, error) {
	if estimateContentsTokens(contents) <= available {
		return append([]*genai.Content(nil), contents...), 0, nil, "", nil
	}
	if len(contents) < 3 {
		return nil, 0, nil, "", fmt.Errorf("%w: current request cannot be compacted safely", appharness.ErrContextBudgetExceeded)
	}
	cutoff := len(contents) - recent
	if cutoff < 1 {
		cutoff = 1
	}
	if splitsFunctionPair(contents, cutoff) {
		if cutoff > 1 {
			cutoff--
		} else {
			cutoff++
		}
	}
	placeholderRef := appharness.ArtifactRef{
		ID: strings.Repeat("x", 96), Kind: compactionArtifactKind, SchemaVersion: "1",
	}
	for cutoff > 0 && cutoff < len(contents) {
		removedContents := contents[:cutoff]
		summary := summarizeContents(removedContents)
		candidate := append(
			[]*genai.Content{compactionContent(summary, placeholderRef)},
			contents[cutoff:]...,
		)
		if estimateContentsTokens(candidate) <= available {
			hashes := make([]string, len(removedContents))
			for index, content := range removedContents {
				hashes[index] = contentHash(content)
			}
			compacted := make([]*genai.Content, 1, 1+len(contents)-cutoff)
			compacted = append(compacted, contents[cutoff:]...)
			return compacted, cutoff, hashes, summary, nil
		}
		cutoff++
		if splitsFunctionPair(contents, cutoff) {
			cutoff++
		}
	}
	return nil, 0, nil, "", fmt.Errorf("%w: recent contents exceed the available window", appharness.ErrContextBudgetExceeded)
}

func splitsFunctionPair(contents []*genai.Content, cutoff int) bool {
	return cutoff > 0 && cutoff < len(contents) &&
		contentHasFunctionResponse(contents[cutoff]) && contentHasFunctionCall(contents[cutoff-1])
}

func (b *contextBudgeter) saveCompaction(
	ctx context.Context,
	hashes []string,
	summary string,
	before int,
	available int,
) (appharness.Artifact, error) {
	if b.artifacts == nil {
		return appharness.Artifact{}, errors.New("artifact store is required for context compaction")
	}
	payload, err := json.Marshal(map[string]any{
		"summary": summary, "removedContentHashes": hashes,
		"estimatedTokensBefore": before, "availableTokens": available,
	})
	if err != nil {
		return appharness.Artifact{}, err
	}
	digest := sha256.Sum256(payload)
	id := b.turnID + "-context-" + hex.EncodeToString(digest[:8])
	return b.artifacts.SaveArtifact(ctx, appharness.Artifact{
		Ref:   appharness.ArtifactRef{ID: id, Kind: compactionArtifactKind, SchemaVersion: "1"},
		RunID: b.runID, TurnID: b.turnID, AgentID: b.agentID, Payload: payload,
	})
}

func compactionContent(summary string, ref appharness.ArtifactRef) *genai.Content {
	text := fmt.Sprintf("# Compacted Session Context\n%s\nFull compaction record: artifact %s (%s).", summary, ref.ID, ref.Kind)
	return genai.NewContentFromText(text, genai.RoleUser)
}

func summarizeContents(contents []*genai.Content) string {
	var builder strings.Builder
	for _, content := range contents {
		if content == nil {
			continue
		}
		role := string(content.Role)
		if role == "" {
			role = "user"
		}
		for _, part := range content.Parts {
			if part == nil {
				continue
			}
			line := ""
			switch {
			case part.Text != "":
				line = fmt.Sprintf("%s: %s", role, truncateUTF8(strings.TrimSpace(part.Text), 512))
			case part.FunctionCall != nil:
				line = fmt.Sprintf("%s called %s (args %s)", role, part.FunctionCall.Name, hashAny(part.FunctionCall.Args))
			case part.FunctionResponse != nil:
				line = fmt.Sprintf("tool %s returned %s", part.FunctionResponse.Name, toolResponseSummary(part.FunctionResponse.Response))
			}
			if line == "" || builder.Len()+len(line)+2 > maxSummaryBytes {
				continue
			}
			builder.WriteString("- ")
			builder.WriteString(line)
			builder.WriteByte('\n')
		}
	}
	if builder.Len() == 0 {
		return "Earlier session contents were compacted; use the artifact hashes for audit."
	}
	return strings.TrimSpace(builder.String())
}

func toolResponseSummary(response map[string]any) string {
	if metadata, ok := response[toolMetadataKey].(map[string]any); ok {
		if summary, ok := metadata["summary"].(string); ok && summary != "" {
			return truncateUTF8(summary, 512)
		}
	}
	return "payload " + hashAny(response)
}

func estimateRequestOverhead(request *model.LLMRequest) int {
	tokens := 0
	if request.Config != nil {
		if request.Config.SystemInstruction != nil {
			tokens += estimateContentTokens(request.Config.SystemInstruction)
		}
		if encoded, err := json.Marshal(request.Config.Tools); err == nil {
			tokens += estimateTextTokens(string(encoded))
		}
	}
	return tokens
}

func estimateContentsTokens(contents []*genai.Content) int {
	total := 0
	for _, content := range contents {
		total += estimateContentTokens(content)
	}
	return total
}

func estimateContentTokens(content *genai.Content) int {
	if content == nil {
		return 0
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return 0
	}
	return estimateTextTokens(string(encoded)) + 4
}

func estimateTextTokens(value string) int {
	ascii := 0
	nonASCII := 0
	for len(value) > 0 {
		current, size := utf8.DecodeRuneInString(value)
		if current <= 127 {
			ascii++
		} else {
			nonASCII++
		}
		value = value[size:]
	}
	return (ascii+3)/4 + nonASCII
}

func contentHasFunctionCall(content *genai.Content) bool {
	if content == nil {
		return false
	}
	for _, part := range content.Parts {
		if part != nil && part.FunctionCall != nil {
			return true
		}
	}
	return false
}

func contentHasFunctionResponse(content *genai.Content) bool {
	if content == nil {
		return false
	}
	for _, part := range content.Parts {
		if part != nil && part.FunctionResponse != nil {
			return true
		}
	}
	return false
}

func contentHash(content *genai.Content) string {
	encoded, _ := json.Marshal(content)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func hashAny(value any) string {
	encoded, _ := json.Marshal(value)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:8])
}

func validateContextPolicy(policy appharness.ContextPolicy) error {
	if policy.History != "session" || (policy.Memory != "none" && policy.Memory != "recall") ||
		policy.MaxToolResultBytes < 1024 || policy.ModelWindowTokens < 8192 ||
		policy.ReservedOutputTokens < 1024 || policy.SafetyMarginTokens < 512 ||
		policy.RecentContents < 2 || policy.ReservedOutputTokens+policy.SafetyMarginTokens >= policy.ModelWindowTokens {
		return errors.New("invalid context policy")
	}
	return nil
}

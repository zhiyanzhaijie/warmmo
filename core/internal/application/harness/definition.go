package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
)

type AgentTier string

const (
	AgentTierChat      AgentTier = "chat"
	AgentTierReasoning AgentTier = "reasoning"
	AgentTierWorker    AgentTier = "worker"
)

type PromptSpec struct {
	ID string `json:"id"`
}

type ModelPolicy struct {
	Hint string `json:"hint"`
}

type ContextPolicy struct {
	History              string `json:"history"`
	Memory               string `json:"memory"`
	MaxToolResultBytes   int    `json:"maxToolResultBytes"`
	ModelWindowTokens    int    `json:"modelWindowTokens"`
	ReservedOutputTokens int    `json:"reservedOutputTokens"`
	SafetyMarginTokens   int    `json:"safetyMarginTokens"`
	RecentContents       int    `json:"recentContents"`
}

type MemoryPolicy struct {
	Recall   bool `json:"recall"`
	Remember bool `json:"remember"`
}

type ArtifactSchema struct {
	Kind          string         `json:"kind"`
	SchemaVersion string         `json:"schemaVersion"`
	Schema        map[string]any `json:"schema"`
}

type OutputContract struct {
	Kind      string           `json:"kind"`
	Artifacts []ArtifactSchema `json:"artifacts,omitempty"`
}

const OutputKindArtifact = "artifact"
const OutputKindText = "text"

func (c OutputContract) Artifact(kind string) (ArtifactSchema, bool) {
	for _, artifact := range c.Artifacts {
		if artifact.Kind == kind {
			return artifact, true
		}
	}
	return ArtifactSchema{}, false
}

type AgentDefinition struct {
	ID              string         `json:"id"`
	Version         string         `json:"version"`
	Description     string         `json:"description"`
	Name            string         `json:"name"`
	Tier            AgentTier      `json:"tier"`
	Model           ModelPolicy    `json:"model"`
	Prompt          PromptSpec     `json:"prompt"`
	Tools           []string       `json:"tools"`
	ControlTools    []string       `json:"controlTools,omitempty"`
	AllowedChildren []string       `json:"allowedChildren,omitempty"`
	Budget          BudgetPolicy   `json:"budget"`
	Context         ContextPolicy  `json:"context"`
	Memory          MemoryPolicy   `json:"memory"`
	Output          OutputContract `json:"output"`
}

type RegisteredDefinition struct {
	Definition AgentDefinition
	Hash       string
}

type DefinitionRegistry struct {
	mu          sync.RWMutex
	definitions map[string]RegisteredDefinition
}

func NewDefinitionRegistry(definitions ...AgentDefinition) (*DefinitionRegistry, error) {
	registry := &DefinitionRegistry{definitions: make(map[string]RegisteredDefinition, len(definitions))}
	for _, definition := range definitions {
		if err := registry.Register(definition); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func (r *DefinitionRegistry) Register(definition AgentDefinition) error {
	if r == nil {
		return errors.New("definition registry is required")
	}
	definition = cloneDefinition(definition)
	if err := validateDefinition(definition); err != nil {
		return err
	}
	hash, err := StableHash(definition)
	if err != nil {
		return fmt.Errorf("hash agent definition %q: %w", definition.ID, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.definitions[definition.ID]; exists {
		return fmt.Errorf("duplicate agent definition %q", definition.ID)
	}
	r.definitions[definition.ID] = RegisteredDefinition{Definition: definition, Hash: hash}
	return nil
}

func (r *DefinitionRegistry) Resolve(id string) (RegisteredDefinition, error) {
	if r == nil {
		return RegisteredDefinition{}, errors.New("definition registry is required")
	}
	r.mu.RLock()
	registered, ok := r.definitions[strings.TrimSpace(id)]
	r.mu.RUnlock()
	if !ok {
		return RegisteredDefinition{}, fmt.Errorf("agent definition not found: %s", id)
	}
	registered.Definition = cloneDefinition(registered.Definition)
	return registered, nil
}

func (r *DefinitionRegistry) ValidateGraph() error {
	if r == nil {
		return errors.New("definition registry is required")
	}
	r.mu.RLock()
	definitions := make(map[string]RegisteredDefinition, len(r.definitions))
	for id, definition := range r.definitions {
		definitions[id] = definition
	}
	r.mu.RUnlock()
	for id, registered := range definitions {
		for _, childID := range registered.Definition.AllowedChildren {
			_, ok := definitions[childID]
			if !ok {
				return fmt.Errorf("agent definition %q references unknown child %q", id, childID)
			}
			if registered.Definition.Tier == AgentTierWorker {
				return fmt.Errorf("worker agent definition %q cannot delegate", id)
			}
		}
	}
	visiting := make(map[string]bool, len(definitions))
	visited := make(map[string]bool, len(definitions))
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return fmt.Errorf("agent definition child cycle contains %q", id)
		}
		if visited[id] {
			return nil
		}
		visiting[id] = true
		for _, childID := range definitions[id].Definition.AllowedChildren {
			if err := visit(childID); err != nil {
				return err
			}
		}
		visiting[id] = false
		visited[id] = true
		return nil
	}
	ids := make([]string, 0, len(definitions))
	for id := range definitions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func StableHash(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func validateDefinition(definition AgentDefinition) error {
	if strings.TrimSpace(definition.ID) == "" || strings.TrimSpace(definition.Version) == "" ||
		strings.TrimSpace(definition.Name) == "" || strings.TrimSpace(definition.Description) == "" ||
		strings.TrimSpace(definition.Prompt.ID) == "" {
		return errors.New("agent definition id, version, name, description and prompt are required")
	}
	switch definition.Tier {
	case AgentTierChat, AgentTierReasoning, AgentTierWorker:
	default:
		return fmt.Errorf("agent definition %q has invalid tier %q", definition.ID, definition.Tier)
	}
	if definition.Budget.MaxModelCalls <= 0 || definition.Budget.MaxToolCalls < 0 ||
		definition.Budget.MaxSideEffectCalls < 0 || definition.Budget.MaxDuration <= 0 ||
		definition.Budget.MaxToolResultBytes < 1024 {
		return fmt.Errorf("agent definition %q has invalid budget", definition.ID)
	}
	if definition.Context.MaxToolResultBytes < 1024 || definition.Context.MaxToolResultBytes > definition.Budget.MaxToolResultBytes ||
		definition.Context.ModelWindowTokens < 8192 || definition.Context.ReservedOutputTokens < 1024 ||
		definition.Context.SafetyMarginTokens < 512 || definition.Context.RecentContents < 2 ||
		definition.Context.ReservedOutputTokens+definition.Context.SafetyMarginTokens >= definition.Context.ModelWindowTokens {
		return fmt.Errorf("agent definition %q has invalid context result budget", definition.ID)
	}
	if definition.Context.History != "session" ||
		(definition.Context.Memory != "none" && definition.Context.Memory != "recall") ||
		definition.Memory.Recall != (definition.Context.Memory == "recall") {
		return fmt.Errorf("agent definition %q has an invalid context or memory mode", definition.ID)
	}
	if hasDuplicateOrEmpty(definition.Tools) || hasDuplicateOrEmpty(definition.ControlTools) || hasDuplicateOrEmpty(definition.AllowedChildren) {
		return fmt.Errorf("agent definition %q has empty or duplicate tool/child IDs", definition.ID)
	}
	if strings.TrimSpace(definition.Model.Hint) == "" {
		return fmt.Errorf("agent definition %q requires a model policy hint", definition.ID)
	}
	if definition.Output.Kind == OutputKindText {
		if len(definition.Output.Artifacts) != 0 {
			return fmt.Errorf("text agent definition %q cannot declare artifact schemas", definition.ID)
		}
		return nil
	}
	if definition.Output.Kind != OutputKindArtifact || len(definition.Output.Artifacts) == 0 {
		return fmt.Errorf("agent definition %q has an invalid output contract", definition.ID)
	}
	seenKinds := make(map[string]struct{}, len(definition.Output.Artifacts))
	for _, artifact := range definition.Output.Artifacts {
		if strings.TrimSpace(artifact.Kind) == "" || strings.TrimSpace(artifact.SchemaVersion) == "" || len(artifact.Schema) == 0 {
			return fmt.Errorf("agent definition %q has an incomplete artifact schema", definition.ID)
		}
		if _, exists := seenKinds[artifact.Kind]; exists {
			return fmt.Errorf("agent definition %q has duplicate artifact kind %q", definition.ID, artifact.Kind)
		}
		seenKinds[artifact.Kind] = struct{}{}
		encoded, err := json.Marshal(artifact.Schema)
		if err != nil {
			return fmt.Errorf("encode artifact schema %q: %w", artifact.Kind, err)
		}
		var schema jsonschema.Schema
		if err := json.Unmarshal(encoded, &schema); err != nil {
			return fmt.Errorf("decode artifact schema %q: %w", artifact.Kind, err)
		}
		if _, err := schema.Resolve(nil); err != nil {
			return fmt.Errorf("resolve artifact schema %q: %w", artifact.Kind, err)
		}
	}
	return nil
}

func cloneDefinition(definition AgentDefinition) AgentDefinition {
	definition.Tools = append([]string(nil), definition.Tools...)
	definition.ControlTools = append([]string(nil), definition.ControlTools...)
	definition.AllowedChildren = append([]string(nil), definition.AllowedChildren...)
	definition.Output.Artifacts = append([]ArtifactSchema(nil), definition.Output.Artifacts...)
	for index := range definition.Output.Artifacts {
		encoded, err := json.Marshal(definition.Output.Artifacts[index].Schema)
		if err == nil {
			var schema map[string]any
			if json.Unmarshal(encoded, &schema) == nil {
				definition.Output.Artifacts[index].Schema = schema
			}
		}
	}
	return definition
}

func hasDuplicateOrEmpty(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return true
		}
		if _, exists := seen[value]; exists {
			return true
		}
		seen[value] = struct{}{}
	}
	return false
}

package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
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
	ReservedOutputTokens int    `json:"reservedOutputTokens"`
}

type MemoryPolicy struct {
	Recall   bool `json:"recall"`
	Remember bool `json:"remember"`
}

type AgentDefinition struct {
	ID           string        `json:"id"`
	Version      string        `json:"version"`
	Description  string        `json:"description"`
	Name         string        `json:"name"`
	Tier         AgentTier     `json:"tier"`
	Model        ModelPolicy   `json:"model"`
	Prompt       PromptSpec    `json:"prompt"`
	Tools        []string      `json:"tools"`
	ControlTools []string      `json:"controlTools,omitempty"`
	Budget       BudgetPolicy  `json:"budget"`
	Context      ContextPolicy `json:"context"`
	Memory       MemoryPolicy  `json:"memory"`
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
		definition.Context.ReservedOutputTokens < 1024 {
		return fmt.Errorf("agent definition %q has invalid context result budget", definition.ID)
	}
	if definition.Context.History != "session" ||
		(definition.Context.Memory != "none" && definition.Context.Memory != "recall") ||
		definition.Memory.Recall != (definition.Context.Memory == "recall") {
		return fmt.Errorf("agent definition %q has an invalid context or memory mode", definition.ID)
	}
	if hasDuplicateOrEmpty(definition.Tools) || hasDuplicateOrEmpty(definition.ControlTools) {
		return fmt.Errorf("agent definition %q has empty or duplicate tool IDs", definition.ID)
	}
	if strings.TrimSpace(definition.Model.Hint) == "" {
		return fmt.Errorf("agent definition %q requires a model policy hint", definition.ID)
	}
	return nil
}

func cloneDefinition(definition AgentDefinition) AgentDefinition {
	definition.Tools = append([]string(nil), definition.Tools...)
	definition.ControlTools = append([]string(nil), definition.ControlTools...)
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

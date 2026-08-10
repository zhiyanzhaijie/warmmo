package writing

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

const maxSkillFileSize = 256 * 1024

var (
	ErrSkillNotFound = errors.New("skill not found")
	ErrInvalidSkill  = errors.New("invalid skill")
)

type Skill struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Description  string   `json:"description"`
	Targets      []string `json:"targets"`
	Instructions string   `json:"-"`
	AllowedTools []string `json:"allowedTools"`
}

type SkillMatch struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

type SkillCatalog interface {
	Search(context.Context, string, string) ([]SkillMatch, error)
	Load(context.Context, string) (Skill, error)
}

type Catalog struct {
	mu     sync.RWMutex
	skills map[string]skillEntry
}

type skillEntry struct {
	metadata Skill
	path     string
}

type skillFrontMatter struct {
	ID           string   `yaml:"id"`
	Name         string   `yaml:"name"`
	Version      string   `yaml:"version"`
	Description  string   `yaml:"description"`
	Targets      []string `yaml:"targets"`
	AllowedTools []string `yaml:"allowed_tools"`
}

func NewCatalog() *Catalog {
	return &Catalog{skills: make(map[string]skillEntry)}
}

func LoadCatalog(directory string) (*Catalog, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("read skill directory: %w", err)
	}
	catalog := NewCatalog()
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		skill, err := loadSkillFile(filepath.Join(directory, entry.Name(), "SKILL.md"))
		if err != nil {
			return nil, fmt.Errorf("load skill %q: %w", entry.Name(), err)
		}
		if skill.ID != entry.Name() {
			return nil, fmt.Errorf("%w: skill id %q must match directory %q", ErrInvalidSkill, skill.ID, entry.Name())
		}
		skill.Instructions = ""
		if err := catalog.register(skill, filepath.Join(directory, entry.Name(), "SKILL.md")); err != nil {
			return nil, err
		}
	}
	if catalog.Len() == 0 {
		return nil, fmt.Errorf("%w: no skills found in %s", ErrInvalidSkill, directory)
	}
	return catalog, nil
}

func (c *Catalog) Register(skill Skill) error {
	if err := validateSkill(skill); err != nil {
		return err
	}
	return c.register(skill, "")
}

func (c *Catalog) register(skill Skill, path string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.skills[skill.ID]; exists {
		return fmt.Errorf("%w: duplicate skill id %q", ErrInvalidSkill, skill.ID)
	}
	c.skills[skill.ID] = skillEntry{metadata: skill, path: path}
	return nil
}

func (c *Catalog) Search(_ context.Context, target, _ string) ([]SkillMatch, error) {
	target = strings.ToLower(strings.TrimSpace(target))
	c.mu.RLock()
	matches := make([]SkillMatch, 0, len(c.skills))
	for _, entry := range c.skills {
		skill := entry.metadata
		if !supportsTarget(skill.Targets, target) {
			continue
		}
		matches = append(matches, SkillMatch{ID: skill.ID, Name: skill.Name, Version: skill.Version, Description: skill.Description})
	}
	c.mu.RUnlock()
	sort.Slice(matches, func(i, j int) bool { return matches[i].ID < matches[j].ID })
	return matches, nil
}

func (c *Catalog) Load(_ context.Context, skillID string) (Skill, error) {
	c.mu.RLock()
	entry, ok := c.skills[skillID]
	c.mu.RUnlock()
	if !ok {
		return Skill{}, ErrSkillNotFound
	}
	if entry.path == "" {
		return entry.metadata, nil
	}
	skill, err := loadSkillFile(entry.path)
	if err != nil {
		return Skill{}, fmt.Errorf("reload skill %q: %w", skillID, err)
	}
	return skill, nil
}

func (c *Catalog) Len() int {
	c.mu.RLock()
	length := len(c.skills)
	c.mu.RUnlock()
	return length
}

func supportsTarget(targets []string, target string) bool {
	for _, candidate := range targets {
		if candidate == "*" || strings.EqualFold(candidate, target) {
			return true
		}
	}
	return false
}

func loadSkillFile(path string) (Skill, error) {
	file, err := os.Open(path)
	if err != nil {
		return Skill{}, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxSkillFileSize+1))
	if err != nil {
		return Skill{}, fmt.Errorf("read SKILL.md: %w", err)
	}
	if len(content) > maxSkillFileSize {
		return Skill{}, fmt.Errorf("%w: SKILL.md exceeds %d bytes", ErrInvalidSkill, maxSkillFileSize)
	}
	frontMatter, instructions, err := splitSkillDocument(content)
	if err != nil {
		return Skill{}, err
	}
	var metadata skillFrontMatter
	decoder := yaml.NewDecoder(bytes.NewReader(frontMatter))
	decoder.KnownFields(true)
	if err := decoder.Decode(&metadata); err != nil {
		return Skill{}, fmt.Errorf("%w: decode front matter: %v", ErrInvalidSkill, err)
	}
	skill := Skill{
		ID: strings.TrimSpace(metadata.ID), Name: strings.TrimSpace(metadata.Name),
		Version: strings.TrimSpace(metadata.Version), Description: strings.TrimSpace(metadata.Description),
		Targets: cleanStrings(metadata.Targets), Instructions: strings.TrimSpace(string(instructions)),
		AllowedTools: cleanStrings(metadata.AllowedTools),
	}
	return skill, validateSkill(skill)
}

func splitSkillDocument(content []byte) ([]byte, []byte, error) {
	content = bytes.TrimPrefix(content, []byte("\xef\xbb\xbf"))
	lines := bytes.Split(content, []byte("\n"))
	if len(lines) < 3 || strings.TrimSpace(string(lines[0])) != "---" {
		return nil, nil, fmt.Errorf("%w: SKILL.md must start with YAML front matter", ErrInvalidSkill)
	}
	for index := 1; index < len(lines); index++ {
		if strings.TrimSpace(string(lines[index])) != "---" {
			continue
		}
		frontMatter := bytes.Join(lines[1:index], []byte("\n"))
		body := bytes.Join(lines[index+1:], []byte("\n"))
		if len(bytes.TrimSpace(body)) == 0 {
			return nil, nil, fmt.Errorf("%w: SKILL.md instructions are required", ErrInvalidSkill)
		}
		return frontMatter, body, nil
	}
	return nil, nil, fmt.Errorf("%w: YAML front matter is not closed", ErrInvalidSkill)
}

func validateSkill(skill Skill) error {
	if skill.ID == "" || skill.Name == "" || skill.Version == "" || skill.Description == "" {
		return fmt.Errorf("%w: id, name, version and description are required", ErrInvalidSkill)
	}
	if len(skill.Targets) == 0 {
		return fmt.Errorf("%w: skill %q requires at least one target", ErrInvalidSkill, skill.ID)
	}
	if skill.Instructions == "" {
		return fmt.Errorf("%w: skill %q instructions are required", ErrInvalidSkill, skill.ID)
	}
	return nil
}

func cleanStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

package writing

import "context"

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

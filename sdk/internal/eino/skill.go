package eino

import (
	"context"
	"fmt"

	einoskill "github.com/cloudwego/eino/adk/middlewares/skill"
)

type Skill struct {
	Name        string
	Description string
	Content     string
}

type memorySkillBackend struct {
	skills []Skill
	byName map[string]Skill
}

var _ einoskill.Backend = (*memorySkillBackend)(nil)

func newMemorySkillBackend(skills []Skill) (*memorySkillBackend, error) {
	backend := &memorySkillBackend{
		skills: make([]Skill, 0, len(skills)),
		byName: make(map[string]Skill, len(skills)),
	}
	for _, skill := range skills {
		if skill.Name == "" {
			return nil, fmt.Errorf("Skill name is empty")
		}
		if _, duplicate := backend.byName[skill.Name]; duplicate {
			continue
		}
		backend.byName[skill.Name] = skill
		backend.skills = append(backend.skills, skill)
	}
	return backend, nil
}

func (b *memorySkillBackend) List(context.Context) ([]einoskill.FrontMatter, error) {
	frontMatter := make([]einoskill.FrontMatter, 0, len(b.skills))
	for _, skill := range b.skills {
		frontMatter = append(frontMatter, einoskill.FrontMatter{
			Name:        skill.Name,
			Description: skill.Description,
		})
	}
	return frontMatter, nil
}

func (b *memorySkillBackend) Get(_ context.Context, name string) (einoskill.Skill, error) {
	skill, exists := b.byName[name]
	if !exists {
		return einoskill.Skill{}, fmt.Errorf("Skill %q is not available to this Agent", name)
	}
	return einoskill.Skill{
		FrontMatter: einoskill.FrontMatter{
			Name:        skill.Name,
			Description: skill.Description,
		},
		Content: skill.Content,
	}, nil
}

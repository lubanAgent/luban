package eino

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	einoskill "github.com/cloudwego/eino/adk/middlewares/skill"
)

type Skill struct {
	Name        string
	Description string
	Content     string
}

type SkillInfo struct {
	Name        string
	Description string
}

type SkillBackend interface {
	List(context.Context) ([]SkillInfo, error)
	Get(context.Context, string) (Skill, error)
}

type SkillBackendSource struct {
	Name    string
	Backend SkillBackend
}

type einoSkillBackend struct {
	staticSkills map[string]Skill
	sources      []SkillBackendSource

	mu     sync.RWMutex
	owners map[string]SkillBackend
}

var _ einoskill.Backend = (*einoSkillBackend)(nil)

func newEinoSkillBackend(skills []Skill, sources []SkillBackendSource) (*einoSkillBackend, error) {
	backend := &einoSkillBackend{
		staticSkills: make(map[string]Skill, len(skills)),
		sources:      append([]SkillBackendSource(nil), sources...),
		owners:       make(map[string]SkillBackend),
	}
	for _, skill := range skills {
		if strings.TrimSpace(skill.Name) == "" {
			return nil, fmt.Errorf("Skill name is empty")
		}
		if _, duplicate := backend.staticSkills[skill.Name]; duplicate {
			return nil, fmt.Errorf("duplicate static Skill %q", skill.Name)
		}
		backend.staticSkills[skill.Name] = skill
	}
	for _, source := range backend.sources {
		if strings.TrimSpace(source.Name) == "" || source.Backend == nil {
			return nil, fmt.Errorf("SkillBackend source must have a name and implementation")
		}
	}
	return backend, nil
}

func (b *einoSkillBackend) List(ctx context.Context) ([]einoskill.FrontMatter, error) {
	frontMatter := make([]einoskill.FrontMatter, 0, len(b.staticSkills))
	owners := make(map[string]SkillBackend)
	// Sort static Skills so the generated tool description is stable across
	// map iteration order.
	staticNames := make([]string, 0, len(b.staticSkills))
	for name := range b.staticSkills {
		staticNames = append(staticNames, name)
	}
	sort.Strings(staticNames)
	for _, name := range staticNames {
		skill := b.staticSkills[name]
		frontMatter = append(frontMatter, einoskill.FrontMatter{
			Name:        skill.Name,
			Description: skill.Description,
		})
	}

	for _, source := range b.sources {
		available, err := source.Backend.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("SkillBackend %q list: %w", source.Name, err)
		}
		for _, skill := range available {
			if strings.TrimSpace(skill.Name) == "" {
				return nil, fmt.Errorf("SkillBackend %q returned a Skill with an empty name", source.Name)
			}
			if strings.TrimSpace(skill.Description) == "" {
				return nil, fmt.Errorf("SkillBackend %q Skill %q has an empty description", source.Name, skill.Name)
			}
			if _, duplicate := owners[skill.Name]; duplicate {
				return nil, fmt.Errorf("duplicate dynamic Skill name %q", skill.Name)
			}
			if _, duplicate := b.staticSkills[skill.Name]; duplicate {
				return nil, fmt.Errorf("dynamic Skill %q conflicts with a registered Skill", skill.Name)
			}
			owners[skill.Name] = source.Backend
			frontMatter = append(frontMatter, einoskill.FrontMatter{
				Name:        skill.Name,
				Description: skill.Description,
			})
		}
	}

	b.mu.Lock()
	b.owners = owners
	b.mu.Unlock()
	return frontMatter, nil
}

func (b *einoSkillBackend) Get(ctx context.Context, name string) (einoskill.Skill, error) {
	if skill, exists := b.staticSkills[name]; exists {
		return einoskill.Skill{
			FrontMatter: einoskill.FrontMatter{
				Name:        skill.Name,
				Description: skill.Description,
			},
			Content: skill.Content,
		}, nil
	}

	b.mu.RLock()
	source, exists := b.owners[name]
	b.mu.RUnlock()
	if !exists {
		if _, err := b.List(ctx); err != nil {
			return einoskill.Skill{}, err
		}
		b.mu.RLock()
		source, exists = b.owners[name]
		b.mu.RUnlock()
	}
	if !exists {
		return einoskill.Skill{}, fmt.Errorf("Skill %q is not available to this Agent", name)
	}
	skill, err := source.Get(ctx, name)
	if err != nil {
		return einoskill.Skill{}, fmt.Errorf("load Skill %q: %w", name, err)
	}
	if skill.Name != name {
		return einoskill.Skill{}, fmt.Errorf("SkillBackend returned Skill %q for requested name %q", skill.Name, name)
	}
	if strings.TrimSpace(skill.Description) == "" || strings.TrimSpace(skill.Content) == "" {
		return einoskill.Skill{}, fmt.Errorf("SkillBackend returned incomplete Skill %q", name)
	}
	return einoskill.Skill{
		FrontMatter: einoskill.FrontMatter{
			Name:        skill.Name,
			Description: skill.Description,
		},
		Content: skill.Content,
	}, nil
}

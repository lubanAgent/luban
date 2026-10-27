// Package server loads and hosts configured Luban Agents. Business functions
// depend on the sdk package, never on this service package.
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/lubanAgent/luban/models"
	luban "github.com/lubanAgent/luban/sdk"
	"gopkg.in/yaml.v2"
)

type serviceConfig struct {
	HTTP struct {
		AutoRegister  *bool    `yaml:"autoRegister"`
		RoutePrefix   string   `yaml:"routePrefix"`
		Address       string   `yaml:"address"`
		ExcludeAgents []string `yaml:"excludeAgents"`
	} `yaml:"http"`
	BackgroundAgents []string                           `yaml:"backgroundAgents"`
	LLM              map[string][]luban.ModelConnection `yaml:"llm"`
	Databases        map[string]models.DatabaseConfig   `yaml:"databases"`
}

type assetsSnapshot struct {
	service serviceConfig
	agents  luban.Config
	skills  map[string]map[string]luban.Skill
}

var directoryKey = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

func loadAssets(assets fs.FS) (assetsSnapshot, error) {
	var result assetsSnapshot
	if assets == nil {
		return result, fmt.Errorf("luban server: Assets must not be nil")
	}
	raw, err := fs.ReadFile(assets, "conf/luban.yaml")
	if err != nil {
		return result, fmt.Errorf("read conf/luban.yaml: %w", err)
	}
	if err := yaml.UnmarshalStrict(raw, &result.service); err != nil {
		return result, fmt.Errorf("invalid conf/luban.yaml: check field names and value types")
	}
	if result.service.HTTP.RoutePrefix == "" {
		result.service.HTTP.RoutePrefix = "/luban/ai/agent"
	}
	prefix := result.service.HTTP.RoutePrefix
	parts := strings.Split(strings.TrimPrefix(prefix, "/"), "/")
	if !strings.HasPrefix(prefix, "/") || len(parts) != 3 {
		return result, fmt.Errorf("http.routePrefix must contain exactly three path segments")
	}
	for _, part := range parts {
		if !directoryKey.MatchString(part) {
			return result, fmt.Errorf("http.routePrefix contains an invalid path segment")
		}
	}
	if result.service.HTTP.Address == "" {
		result.service.HTTP.Address = ":8080"
	}
	entries, err := fs.ReadDir(assets, "agents")
	if err != nil {
		return result, fmt.Errorf("read agents: %w", err)
	}
	result.agents.Agents = make(map[string]luban.AgentConfig)
	result.skills = make(map[string]map[string]luban.Skill)
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		key := entry.Name()
		if !directoryKey.MatchString(key) {
			return result, fmt.Errorf("invalid Agent directory %q", key)
		}
		file := path.Join("agents", key, "agent.json")
		raw, err := fs.ReadFile(assets, file)
		if err != nil {
			return result, fmt.Errorf("read %s: %w", file, err)
		}
		var agent luban.AgentConfig
		if err := json.Unmarshal(raw, &agent); err != nil {
			return result, fmt.Errorf("invalid %s: check JSON field types and object flow nodes", file)
		}
		if len(result.service.LLM) == 1 && agent.ProjectKey == "" {
			for project := range result.service.LLM {
				agent.ProjectKey = project
			}
		}
		result.agents.Agents[key] = agent
		local, err := loadSkills(assets, path.Join("agents", key, "skills"))
		if err != nil {
			return result, err
		}
		// Skills are owned by this Agent and cannot leak across Agent scopes.
		result.skills[key] = local
	}
	if len(result.agents.Agents) == 0 {
		return result, fmt.Errorf("agents must contain at least one Agent directory")
	}
	return result, nil
}

// resolveConnections only requires credentials for connections used by the
// configured Agents. A model-specific injection wins over configuration;
// an injected project default only replaces the configured default connection.
func resolveConnections(snapshot assetsSnapshot, injected map[string]map[string]luban.ModelInvoker, defaults map[string]luban.ModelInvoker) (map[string][]luban.ModelConnection, error) {
	used := make(map[string]map[string]bool)
	for _, agent := range snapshot.agents.Agents {
		project := agent.ProjectKey
		if project == "" && len(snapshot.service.LLM) == 1 {
			for key := range snapshot.service.LLM {
				project = key
			}
		}
		if used[project] == nil {
			used[project] = make(map[string]bool)
		}
		for _, node := range agent.Flow {
			if node.ModelName != "" {
				used[project][node.ModelName] = true
			}
			for _, child := range node.SubAgents {
				if child.ModelName != "" {
					used[project][child.ModelName] = true
				}
			}
		}
	}
	result := make(map[string][]luban.ModelConnection)
	for project, connections := range snapshot.service.LLM {
		explicit := make(map[string]bool)
		for _, connection := range connections {
			for _, model := range connection.ModelList {
				explicit[model] = true
			}
		}
		for index, connection := range connections {
			selected := false
			if len(connection.ModelList) == 0 {
				for model := range used[project] {
					if injected[project][model] == nil && defaults[project] == nil && !explicit[model] {
						selected = true
						break
					}
				}
			} else {
				for _, model := range connection.ModelList {
					if used[project][model] && injected[project][model] == nil {
						selected = true
						break
					}
				}
			}
			if !selected {
				continue
			}
			var err error
			if connection.BaseURL, err = expandEnvironment(connection.BaseURL); err != nil {
				return nil, fmt.Errorf("llm project %q connection[%d]: %w", project, index, err)
			}
			if connection.APIKey, err = expandEnvironment(connection.APIKey); err != nil {
				return nil, fmt.Errorf("llm project %q connection[%d]: %w", project, index, err)
			}
			// A connection shared with injected models still serves its other
			// selected models; the Runtime chooses the injected model first.
			result[project] = append(result[project], connection)
		}
	}
	return result, nil
}

func expandEnvironment(value string) (string, error) {
	missing := make(map[string]bool)
	expanded := os.Expand(value, func(key string) string {
		result, ok := os.LookupEnv(key)
		if !ok || result == "" {
			missing[key] = true
		}
		return result
	})
	if len(missing) > 0 {
		keys := make([]string, 0, len(missing))
		for key := range missing {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return "", fmt.Errorf("missing environment variables: %s", strings.Join(keys, ", "))
	}
	return expanded, nil
}

func loadSkills(assets fs.FS, directory string) (map[string]luban.Skill, error) {
	result := make(map[string]luban.Skill)
	entries, err := fs.ReadDir(assets, directory)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return result, nil
		}
		return nil, fmt.Errorf("read %s: %w", directory, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		file := path.Join(directory, entry.Name(), "SKILL.md")
		raw, err := fs.ReadFile(assets, file)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", file, err)
		}
		text := strings.ReplaceAll(string(raw), "\r\n", "\n")
		if !strings.HasPrefix(text, "---\n") {
			return nil, fmt.Errorf("%s requires YAML front matter", file)
		}
		front, body, ok := strings.Cut(text[4:], "\n---\n")
		var meta struct {
			Description string `yaml:"description"`
		}
		if !ok || yaml.Unmarshal([]byte(front), &meta) != nil || strings.TrimSpace(meta.Description) == "" || strings.TrimSpace(body) == "" {
			return nil, fmt.Errorf("%s requires description and nonempty content", file)
		}
		result[entry.Name()] = luban.Skill{Name: entry.Name(), Description: meta.Description, Content: body}
	}
	return result, nil
}

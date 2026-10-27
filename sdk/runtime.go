package luban

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"

	"gorm.io/gorm"
)

// Options configures a Runtime. Config and the Models map are copied by New;
// ModelInvoker implementations themselves remain owned by the caller.
type Options struct {
	Config   Config
	Models   map[string]ModelInvoker
	Projects map[string][]ModelConnection
	// ProjectModels overrides selected models with host-owned invokers scoped
	// to a project. This lets a company reuse an existing model gateway client
	// without making the SDK depend on that client's package.
	ProjectModels map[string]map[string]ModelInvoker
	// ProjectDefaults supplies one service-owned fallback invoker per project.
	ProjectDefaults map[string]ModelInvoker
	HTTPDoer        HTTPDoer
	Databases       map[string]*gorm.DB
	// Resources holds arbitrary business clients. New copies the map, not the
	// clients; callers own their concurrency safety and lifecycle.
	Resources     map[string]any
	Skills        map[string]map[string]Skill
	SkillBackends map[string]SkillBackend
	TraceWriter   TraceWriter
}

// Runtime holds immutable business registrations and configuration snapshots.
// A Runtime is safe for concurrent use after Register succeeds.
type Runtime struct {
	config          Config
	models          map[string]ModelInvoker
	projects        map[string][]ModelConnection
	projectRoutes   map[string]projectModelRoute
	projectModels   map[string]map[string]ModelInvoker
	projectDefaults map[string]ModelInvoker
	databases       map[string]*gorm.DB
	resources       map[string]any
	traceWriter     TraceWriter

	registerMu      sync.Mutex
	registerAttempt bool
	registered      atomic.Bool
	funcs           map[string]registeredFunction
	tools           map[string]registeredTool
	skills          map[string]Skill
	skillBackends   map[string]SkillBackend
	agents          map[string]agentRegistry

	configCache *configCache
}

type registeredTool struct {
	value       Tool
	name        string
	description string
}

// New creates a Runtime and copies the supplied configuration and model map.
// Business functions, Tools, and Skills are added with Register.
func New(options Options) (*Runtime, error) {
	resources, err := copyResources(options.Resources)
	if err != nil {
		return nil, err
	}
	models := make(map[string]ModelInvoker, len(options.Models))
	for key, model := range options.Models {
		if key == "" {
			return nil, fmt.Errorf("luban: model name must not be empty")
		}
		if isNilInterface(model) {
			return nil, fmt.Errorf("luban: model %q is nil", key)
		}
		models[key] = model
	}
	projects, routes, err := buildProjectRoutes(options.Projects, options.HTTPDoer)
	if err != nil {
		return nil, err
	}
	projectModels := make(map[string]map[string]ModelInvoker, len(options.ProjectModels))
	for project, configuredModels := range options.ProjectModels {
		if strings.TrimSpace(project) == "" || len(configuredModels) == 0 {
			return nil, fmt.Errorf("luban: custom model project %q must have a name and at least one model", project)
		}
		projectModels[project] = make(map[string]ModelInvoker, len(configuredModels))
		for modelName, model := range configuredModels {
			if strings.TrimSpace(modelName) == "" {
				return nil, fmt.Errorf("luban: custom model project %q has an empty model name", project)
			}
			if isNilInterface(model) {
				return nil, fmt.Errorf("luban: custom model project %q model %q is nil", project, modelName)
			}
			projectModels[project][modelName] = model
		}
	}
	projectDefaults := make(map[string]ModelInvoker, len(options.ProjectDefaults))
	for project, model := range options.ProjectDefaults {
		if strings.TrimSpace(project) == "" || isNilInterface(model) {
			return nil, fmt.Errorf("luban: project %q requires a non-nil default model", project)
		}
		projectDefaults[project] = model
	}
	databases := make(map[string]*gorm.DB, len(options.Databases))
	for name, db := range options.Databases {
		if name == "" {
			return nil, fmt.Errorf("luban: database name must not be empty")
		}
		if db == nil {
			return nil, fmt.Errorf("luban: database %q is nil", name)
		}
		databases[name] = db
	}

	skillBackends := make(map[string]SkillBackend, len(options.SkillBackends))
	for key, backend := range options.SkillBackends {
		if key == "" {
			return nil, fmt.Errorf("luban: SkillBackend name must not be empty")
		}
		if isNilInterface(backend) {
			return nil, fmt.Errorf("luban: SkillBackend %q is nil", key)
		}
		skillBackends[key] = backend
	}
	agents, err := registeredAgentSnapshot(options.Skills)
	if err != nil {
		return nil, err
	}

	return &Runtime{
		config:          cloneConfig(options.Config),
		models:          models,
		projects:        projects,
		projectRoutes:   routes,
		projectModels:   projectModels,
		projectDefaults: projectDefaults,
		databases:       databases,
		resources:       resources,
		traceWriter:     options.TraceWriter,
		funcs:           make(map[string]registeredFunction),
		tools:           make(map[string]registeredTool),
		skills:          make(map[string]Skill),
		skillBackends:   skillBackends,
		agents:          agents,
		configCache:     newConfigCache(),
	}, nil
}

// Register installs the business functions, Tools, and Skills used by the
// Runtime. It may be called once. Registrations are committed only if every
// entry passes validation.
func (rt *Runtime) Register(funcs map[string]any, tools []Tool, skills []Skill) error {
	if rt == nil {
		return fmt.Errorf("luban: cannot register on a nil Runtime")
	}

	rt.registerMu.Lock()
	defer rt.registerMu.Unlock()
	if rt.registerAttempt {
		return fmt.Errorf("luban: Register may only be called once")
	}
	rt.registerAttempt = true

	registeredFuncs := make(map[string]registeredFunction, len(funcs))
	for key, fn := range funcs {
		if key == "" {
			return fmt.Errorf("luban: function key must not be empty")
		}
		info, err := inspectFunction(key, fn)
		if err != nil {
			return err
		}
		registeredFuncs[key] = info
	}

	registeredTools := make(map[string]registeredTool, len(tools))
	for i, tool := range tools {
		if isNilInterface(tool) {
			return fmt.Errorf("luban: Tool at index %d is nil", i)
		}
		name, description, err := inspectTool(tool)
		if err != nil {
			return fmt.Errorf("luban: Tool at index %d: %w", i, err)
		}
		if name == "" {
			return fmt.Errorf("luban: Tool at index %d has an empty name", i)
		}
		if description == "" {
			return fmt.Errorf("luban: Tool %q has an empty description", name)
		}
		if _, exists := registeredTools[name]; exists {
			return fmt.Errorf("luban: duplicate Tool name %q", name)
		}
		registeredTools[name] = registeredTool{
			value:       tool,
			name:        name,
			description: description,
		}
	}

	registeredSkills := make(map[string]Skill, len(skills))
	for i, skill := range skills {
		if skill.Name == "" {
			return fmt.Errorf("luban: Skill at index %d has an empty name", i)
		}
		if skill.Description == "" {
			return fmt.Errorf("luban: Skill %q has an empty description", skill.Name)
		}
		if skill.Content == "" {
			return fmt.Errorf("luban: Skill %q has empty content", skill.Name)
		}
		if _, exists := registeredSkills[skill.Name]; exists {
			return fmt.Errorf("luban: duplicate Skill name %q", skill.Name)
		}
		if _, exists := rt.skillBackends[skill.Name]; exists {
			return fmt.Errorf("luban: Skill %q conflicts with a SkillBackend name", skill.Name)
		}
		registeredSkills[skill.Name] = skill
	}

	rt.funcs = registeredFuncs
	rt.tools = registeredTools
	rt.skills = registeredSkills
	rt.registered.Store(true)
	return nil
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	ref := reflect.ValueOf(value)
	switch ref.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return ref.IsNil()
	default:
		return false
	}
}

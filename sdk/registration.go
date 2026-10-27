package luban

import (
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync"
)

type agentRegistry struct {
	functions map[string]registeredFunction
	tools     map[string]registeredTool
	skills    map[string]Skill
}

var packageRegistry = struct {
	sync.Mutex
	functions map[string]map[string]any
	tools     map[string][]Tool
	errors    []error
}{functions: make(map[string]map[string]any), tools: make(map[string][]Tool)}

// RegisterFuncs records named package functions during an Agent package's init.
// The calling package must be <module>/agents/<agentKey>. Errors are reported by
// New rather than panicking during package initialization.
func RegisterFuncs(functions ...any) {
	pc, _, _, ok := runtime.Caller(1)
	packageRegistry.Lock()
	defer packageRegistry.Unlock()
	agent, err := registrationOwner(pc, ok)
	if err != nil {
		packageRegistry.errors = append(packageRegistry.errors, err)
		return
	}
	if packageRegistry.functions[agent] == nil {
		packageRegistry.functions[agent] = make(map[string]any)
	}
	for _, fn := range functions {
		name, err := registeredFunctionName(fn)
		if err != nil {
			packageRegistry.errors = append(packageRegistry.errors, fmt.Errorf("luban: Agent %q: %w", agent, err))
			continue
		}
		if _, exists := packageRegistry.functions[agent][name]; exists {
			packageRegistry.errors = append(packageRegistry.errors, fmt.Errorf("luban: Agent %q duplicate function %q", agent, name))
			continue
		}
		packageRegistry.functions[agent][name] = fn
	}
}

// RegisterTools records Tool implementations in the calling Agent package.
// Registration alone does not expose a Tool to the model: tools is a whitelist.
func RegisterTools(tools ...Tool) {
	pc, _, _, ok := runtime.Caller(1)
	packageRegistry.Lock()
	defer packageRegistry.Unlock()
	agent, err := registrationOwner(pc, ok)
	if err != nil {
		packageRegistry.errors = append(packageRegistry.errors, err)
		return
	}
	packageRegistry.tools[agent] = append(packageRegistry.tools[agent], tools...)
}

func registrationOwner(pc uintptr, ok bool) (string, error) {
	if ok {
		if symbol := runtime.FuncForPC(pc); symbol != nil {
			name := symbol.Name()
			lastSlash := strings.LastIndex(name, "/")
			if dot := strings.Index(name[lastSlash+1:], "."); dot >= 0 {
				pkg := name[:lastSlash+1+dot]
				if marker := strings.LastIndex(pkg, "/agents/"); marker >= 0 {
					agent := pkg[marker+len("/agents/"):]
					if agent != "" && !strings.Contains(agent, "/") {
						return agent, nil
					}
				}
				return "", fmt.Errorf("luban: registration caller %q must be an agents/<agentKey> package", pkg)
			}
		}
	}
	return "", fmt.Errorf("luban: cannot identify Agent registration package")
}

func registeredFunctionName(fn any) (string, error) {
	if isNilInterface(fn) || reflect.TypeOf(fn).Kind() != reflect.Func {
		return "", fmt.Errorf("registered value must be a named package function")
	}
	symbol := runtime.FuncForPC(reflect.ValueOf(fn).Pointer())
	if symbol == nil {
		return "", fmt.Errorf("registered function has no Go symbol")
	}
	qualified := symbol.Name()
	local := qualified[strings.LastIndex(qualified, "/")+1:]
	parts := strings.Split(local, ".")
	if len(parts) != 2 || strings.ContainsAny(parts[1], "-[]()") {
		return "", fmt.Errorf("%q must be a package function, not a closure or method", qualified)
	}
	if _, err := inspectFunction(parts[1], fn); err != nil {
		return "", err
	}
	return parts[1], nil
}

func registeredAgentSnapshot(skills map[string]map[string]Skill) (map[string]agentRegistry, error) {
	packageRegistry.Lock()
	defer packageRegistry.Unlock()
	if err := errors.Join(packageRegistry.errors...); err != nil {
		return nil, err
	}
	keys := make(map[string]bool)
	for key := range packageRegistry.functions {
		keys[key] = true
	}
	for key := range packageRegistry.tools {
		keys[key] = true
	}
	for key := range skills {
		keys[key] = true
	}
	result := make(map[string]agentRegistry, len(keys))
	for key := range keys {
		// Reuse the same validation as explicit SDK registration.
		temporary := &Runtime{}
		var localSkills []Skill
		for name, skill := range skills[key] {
			if skill.Name != name {
				return nil, fmt.Errorf("luban: Agent %q Skill key %q differs from name %q", key, name, skill.Name)
			}
			localSkills = append(localSkills, skill)
		}
		if err := temporary.Register(packageRegistry.functions[key], packageRegistry.tools[key], localSkills); err != nil {
			return nil, fmt.Errorf("luban: Agent %q: %w", key, err)
		}
		result[key] = agentRegistry{functions: temporary.funcs, tools: temporary.tools, skills: temporary.skills}
	}
	return result, nil
}

func (rt *Runtime) functionsFor(agent string) map[string]registeredFunction {
	if scope, exists := rt.agents[agent]; exists {
		return scope.functions
	}
	return rt.funcs
}

func (rt *Runtime) toolsFor(agent string) map[string]registeredTool {
	if scope, exists := rt.agents[agent]; exists {
		return scope.tools
	}
	return rt.tools
}

func (rt *Runtime) skillsFor(agent string) map[string]Skill {
	if scope, exists := rt.agents[agent]; exists {
		return scope.skills
	}
	return rt.skills
}

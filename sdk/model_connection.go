package luban

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// HTTPDoer sends model requests. Implementations must support concurrent use.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// ModelConnection connects a project's models to one protocol endpoint.
// An empty ModelList denotes the project's default connection.
type ModelConnection struct {
	ModelList []string `json:"modelList,omitempty" yaml:"modelList,omitempty"`
	BaseURL   string   `json:"baseUrl" yaml:"baseUrl"`
	APIKey    string   `json:"apiKey" yaml:"apiKey"`
	Protocol  string   `json:"protocol,omitempty" yaml:"protocol,omitempty"`
}

type projectModelRoute struct {
	models   map[string]ModelInvoker
	fallback ModelInvoker
}

type protocolModel struct {
	connection ModelConnection
	doer       HTTPDoer
}

// NewProtocolInvoker provides the tested default HTTP protocol implementation
// to service adapters. Business code can replace it with its own ModelInvoker.
func NewProtocolInvoker(connection ModelConnection, doer HTTPDoer) (ModelInvoker, error) {
	_, routes, err := buildProjectRoutes(map[string][]ModelConnection{
		"default": {connection},
	}, doer)
	if err != nil {
		return nil, err
	}
	route := routes["default"]
	if route.fallback != nil {
		return route.fallback, nil
	}
	return route.models[connection.ModelList[0]], nil
}

func buildProjectRoutes(projects map[string][]ModelConnection, doer HTTPDoer) (map[string][]ModelConnection, map[string]projectModelRoute, error) {
	if isNilInterface(doer) {
		doer = &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}}
	}
	cloned := make(map[string][]ModelConnection, len(projects))
	routes := make(map[string]projectModelRoute, len(projects))
	for project, connections := range projects {
		if strings.TrimSpace(project) == "" || len(connections) == 0 {
			return nil, nil, fmt.Errorf("luban: project %q requires a name and at least one model connection", project)
		}
		route := projectModelRoute{models: make(map[string]ModelInvoker)}
		for index, connection := range connections {
			location := fmt.Sprintf("luban: project %q connection[%d]", project, index)
			parsed, err := url.Parse(connection.BaseURL)
			if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
				return nil, nil, fmt.Errorf("%s requires an HTTP(S) baseUrl without credentials, query or fragment", location)
			}
			if strings.TrimSpace(connection.APIKey) == "" {
				return nil, nil, fmt.Errorf("%s requires apiKey", location)
			}
			if connection.Protocol == "" {
				connection.Protocol = "openai"
			}
			if connection.Protocol != "openai" && connection.Protocol != "anthropic" {
				return nil, nil, fmt.Errorf("%s has unsupported protocol %q", location, connection.Protocol)
			}
			connection.ModelList = append([]string(nil), connection.ModelList...)
			invoker := &protocolModel{connection: connection, doer: doer}
			if len(connection.ModelList) == 0 {
				if route.fallback != nil {
					return nil, nil, fmt.Errorf("%s duplicates the default connection", location)
				}
				route.fallback = invoker
			}
			for _, name := range connection.ModelList {
				if name == "" || route.models[name] != nil {
					return nil, nil, fmt.Errorf("%s has empty or duplicate model name %q", location, name)
				}
				route.models[name] = invoker
			}
			cloned[project] = append(cloned[project], connection)
		}
		routes[project] = route
	}
	return cloned, routes, nil
}

func (m *protocolModel) Name() string { return m.connection.Protocol }

func (m *protocolModel) Invoke(ctx *gin.Context, request ModelRequest) (ModelResponse, error) {
	if m.connection.Protocol == "anthropic" {
		return m.invokeAnthropic(ctx, request)
	}
	return m.invokeOpenAI(ctx, request)
}

func (rt *Runtime) modelFor(projectKey, modelName string) (ModelInvoker, error) {
	if projectKey == "" {
		projects := rt.projectKeys()
		if len(projects) == 1 {
			projectKey = projects[0]
		}
	}
	if model := rt.projectModels[projectKey][modelName]; model != nil {
		return model, nil
	}
	if model := rt.projectDefaults[projectKey]; model != nil {
		route := rt.projectRoutes[projectKey]
		if route.models[modelName] == nil {
			return model, nil
		}
	}
	if len(rt.projectRoutes) == 0 && len(rt.projectModels) == 0 {
		if projectKey != "" {
			return nil, fmt.Errorf("luban: project %q was not configured", projectKey)
		}
		if model := rt.models[modelName]; model != nil {
			return model, nil
		}
		return nil, fmt.Errorf("luban: model %q was not registered", modelName)
	}
	route, exists := rt.projectRoutes[projectKey]
	if !exists {
		if _, customProjectExists := rt.projectModels[projectKey]; customProjectExists {
			return nil, fmt.Errorf("luban: project %q has no invoker for model %q", projectKey, modelName)
		}
		return nil, fmt.Errorf("luban: projectKey %q must identify one configured project", projectKey)
	}
	if model := route.models[modelName]; model != nil {
		return model, nil
	}
	if route.fallback != nil {
		return route.fallback, nil
	}
	return nil, fmt.Errorf("luban: project %q has no connection for model %q", projectKey, modelName)
}

func (rt *Runtime) hasProjectModelRouting() bool {
	return len(rt.projectRoutes) > 0 || len(rt.projectModels) > 0 || len(rt.projectDefaults) > 0
}

func (rt *Runtime) projectKeys() []string {
	keys := make([]string, 0, len(rt.projectRoutes)+len(rt.projectModels)+len(rt.projectDefaults))
	seen := make(map[string]struct{}, cap(keys))
	for key := range rt.projectRoutes {
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	for key := range rt.projectModels {
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	for key := range rt.projectDefaults {
		if _, exists := seen[key]; exists {
			continue
		}
		keys = append(keys, key)
	}
	return keys
}

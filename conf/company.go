// Package conf loads this service's company resource configuration.
package conf

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"git.yukework.com/pkg/golib/v2/base"
	"git.yukework.com/pkg/golib/v2/middleware"
	serverhttp "git.yukework.com/pkg/golib/v2/server/http"
	"git.yukework.com/pkg/golib/v2/zlog"
	"gopkg.in/yaml.v2"
)

// ModelHTTP contains transport settings only. Projects, model names, URLs,
// protocols and keys have a single source: conf/luban.yaml.
type ModelHTTP struct {
	Timeout        time.Duration `yaml:"timeout"`
	ConnectTimeout time.Duration `yaml:"connectTimeout"`
	Retry          int           `yaml:"retry"`
}

// Company contains service infrastructure, without any mandatory business DB.
type Company struct {
	Basic struct {
		Log       zlog.LogConfig          `yaml:"log"`
		AccessLog middleware.LoggerConfig `yaml:"accessLog"`
		Server    serverhttp.ServerConfig `yaml:"server"`
		Pprof     base.PprofConfig        `yaml:"pprof"`
		GCPercent uint32                  `yaml:"gcPercent"`
	}
	API struct {
		ModelHTTP ModelHTTP `yaml:"modelHTTP"`
	}
	Resource struct {
		Mysql map[string]base.MysqlConf `yaml:"mysql"`
	}
	Custom struct {
		Ips struct {
			Domain     string `yaml:"domain"`
			AppID      string `yaml:"app_id"`
			AppSecret  string `yaml:"app_secret"`
			ServerName string `yaml:"server_name"`
		} `yaml:"ips"`
	}
}

// LoadCompany reads conf/mount in this repository or the deployed service.
// Environment variables are expanded after YAML parsing, so punctuation in
// passwords cannot change the YAML structure. Errors never include values.
func LoadCompany(root string, httpService bool) (*Company, error) {
	result := &Company{}
	for _, file := range []struct {
		name   string
		target any
	}{
		{"config.yaml", &result.Basic},
		{"api.yaml", &result.API},
		{"resource.yaml", &result.Resource},
		{"custom.yaml", &result.Custom},
	} {
		// CLI execution needs model/business resources, not HTTP login. Do not
		// expand IPS secrets or require custom.yaml when there is no listener.
		if file.name == "custom.yaml" && !httpService {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, "conf", "mount", file.name))
		if err != nil {
			if os.IsNotExist(err) && (file.name == "resource.yaml" || file.name == "api.yaml") {
				continue
			}
			return nil, fmt.Errorf("read conf/mount/%s: %w", file.name, err)
		}
		var document any
		if yaml.Unmarshal(raw, &document) != nil {
			return nil, fmt.Errorf("invalid conf/mount/%s", file.name)
		}
		document, err = expandValues(document)
		if err != nil {
			return nil, fmt.Errorf("conf/mount/%s: %w", file.name, err)
		}
		expanded, err := yaml.Marshal(document)
		if err != nil || yaml.UnmarshalStrict(expanded, file.target) != nil {
			return nil, fmt.Errorf("invalid fields or types in conf/mount/%s", file.name)
		}
	}
	if result.API.ModelHTTP.Timeout < 0 || result.API.ModelHTTP.ConnectTimeout < 0 || result.API.ModelHTTP.Retry < 0 {
		return nil, fmt.Errorf("conf/mount/api.yaml modelHTTP timeouts and retry must not be negative")
	}
	ips := result.Custom.Ips
	if httpService && (ips.Domain == "" || ips.AppID == "" || ips.AppSecret == "" || ips.ServerName == "") {
		return nil, fmt.Errorf("conf/mount/custom.yaml requires IPS domain, app_id, app_secret and server_name")
	}
	if result.Basic.Log.Level == "" {
		result.Basic.Log.Level = "info"
	}
	return result, nil
}

func expandValues(value any) (any, error) {
	switch value := value.(type) {
	case string:
		missing := false
		expanded := os.Expand(value, func(key string) string {
			text := os.Getenv(key)
			if text == "" {
				missing = true
			}
			return text
		})
		if missing {
			return nil, fmt.Errorf("a required environment variable is empty or unset")
		}
		return expanded, nil
	case map[any]any:
		for key, child := range value {
			expanded, err := expandValues(child)
			if err != nil {
				return nil, err
			}
			value[key] = expanded
		}
	case []any:
		for index, child := range value {
			expanded, err := expandValues(child)
			if err != nil {
				return nil, err
			}
			value[index] = expanded
		}
	}
	return value, nil
}

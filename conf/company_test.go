package conf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompanyConfigIsSelfContained(t *testing.T) {
	for key, value := range map[string]string{
		"LUBAN_OPENPROXY_DOMAIN": "https://gateway.invalid",
		"LUBAN_OPENPROXY_KEY":    "fake-key",
		"LUBAN_DB_ADDR":          "127.0.0.1:3306",
		"LUBAN_DB_NAME":          "strategy",
		"LUBAN_DB_USER":          "test",
		"LUBAN_DB_PASSWORD":      "password: with # special\ncharacters",
	} {
		t.Setenv(key, value)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "conf/mount"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config.yaml", "api.yaml"} {
		raw, err := os.ReadFile(filepath.Join("..", "conf/mount", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "conf/mount", name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "conf/mount/resource.yaml"), []byte("mysql:\n  admissions:\n    password: ${LUBAN_DB_PASSWORD}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadCompany(root)
	if err != nil {
		t.Fatal(err)
	}
	if config.API.ModelHTTP.Timeout != 120*time.Second ||
		config.Resource.Mysql["admissions"].Password != os.Getenv("LUBAN_DB_PASSWORD") ||
		config.Basic.Server.ReadTimeout != 30*time.Second ||
		config.Basic.Server.WriteTimeout != 120*time.Second ||
		config.Basic.Server.GracefulTimeout != 30*time.Second {
		t.Fatal("local company resources were not loaded correctly")
	}
	// Missing business credentials fail before clients are created.
	t.Setenv("LUBAN_DB_PASSWORD", "")
	if _, err := LoadCompany(root); err == nil {
		t.Fatal("missing database password accepted")
	}
}

func TestInvalidCompanyConfigDoesNotExposeContents(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "conf", "mount")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "config.yaml"), []byte("log: [sensitive-value"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadCompany(root)
	if err == nil || strings.Contains(err.Error(), "sensitive-value") {
		t.Fatalf("unsafe configuration error: %v", err)
	}
}

func TestCompanyCLIAllowsNoAPIOrBusinessDBConfiguration(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "conf/mount"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "conf/mount/config.yaml"), []byte("log:\n  level: info\n"), 0600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadCompany(root)
	if err != nil || len(config.Resource.Mysql) != 0 {
		t.Fatalf("pure Agent required business resources: %+v %v", config, err)
	}
}

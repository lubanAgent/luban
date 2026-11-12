package main

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var secretField = regexp.MustCompile(`(?i)^\s*(apiKey|app_secret|secret|token|password|passwd|authorization)\s*:\s*(.*?)\s*$`)

func TestCommittedYAMLContainsNoLiteralSecrets(t *testing.T) {
	err := filepath.WalkDir("conf", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || (!strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml")) {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for lineNumber := 1; scanner.Scan(); lineNumber++ {
			match := secretField.FindStringSubmatch(scanner.Text())
			if match == nil {
				continue
			}
			value := strings.TrimSpace(match[2])
			if value == "" || strings.HasPrefix(value, "${") {
				continue
			}
			t.Errorf("%s:%d contains a literal %s value; use ${ENV_NAME}", path, lineNumber, match[1])
		}
		return scanner.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
}

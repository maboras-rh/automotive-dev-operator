/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package container

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// LoadIgnorePatterns loads patterns from both .gitignore and .dockerignore
// following Shipwright's approach. .dockerignore takes precedence over .gitignore.
func LoadIgnorePatterns(contextDir string) ([]string, error) {
	// Start with historical defaults
	patterns := []string{".git", ".svn", "node_modules"}

	// Load .gitignore first (like Shipwright does)
	gitignorePath := filepath.Join(contextDir, ".gitignore")
	gitignorePatterns, err := readIgnoreFile(gitignorePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	patterns = append(patterns, gitignorePatterns...)

	// Load .dockerignore (takes precedence over .gitignore)
	dockerignorePath := filepath.Join(contextDir, ".dockerignore")
	dockerignorePatterns, err := readIgnoreFile(dockerignorePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	patterns = append(patterns, dockerignorePatterns...)

	return patterns, nil
}

// readIgnoreFile reads and parses patterns from an ignore file
func readIgnoreFile(filePath string) ([]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	var patterns []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, filepath.ToSlash(line))
	}

	return patterns, scanner.Err()
}

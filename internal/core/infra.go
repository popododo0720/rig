package core

import (
	"log"
	"os"
	"path/filepath"
	"strings"
)

func loadInfraFiles(patterns []string) map[string]string {
	files := make(map[string]string)
	if len(patterns) == 0 {
		return files
	}

	// Resolve the working directory as the allowed base for path validation.
	cwd, err := os.Getwd()
	if err != nil {
		log.Printf("[engine] cannot determine working directory: %v", err)
		return files
	}

	for _, pattern := range patterns {
		// Block absolute paths and obvious traversal patterns.
		if filepath.IsAbs(pattern) || strings.Contains(pattern, "..") {
			log.Printf("[engine] blocked infra pattern %q: absolute or traversal path", pattern)
			continue
		}

		matches, err := filepath.Glob(pattern)
		if err != nil {
			log.Printf("[engine] invalid infra glob pattern %q: %v", pattern, err)
			continue
		}

		for _, path := range matches {
			// Validate resolved path stays within working directory.
			absPath, err := filepath.Abs(path)
			if err != nil {
				log.Printf("[engine] cannot resolve %q: %v", path, err)
				continue
			}
			if !strings.HasPrefix(absPath, cwd+string(filepath.Separator)) && absPath != cwd {
				log.Printf("[engine] blocked infra file %q: outside working directory", path)
				continue
			}

			content, err := os.ReadFile(path)
			if err != nil {
				log.Printf("[engine] failed to read infra file %q: %v", path, err)
				continue
			}
			files[path] = string(content)
		}
	}

	return files
}

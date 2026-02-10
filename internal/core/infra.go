package core

import (
	"log"
	"os"
	"path/filepath"
)

func loadInfraFiles(patterns []string) map[string]string {
	files := make(map[string]string)
	if len(patterns) == 0 {
		return files
	}

	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			log.Printf("[engine] invalid infra glob pattern %q: %v", pattern, err)
			continue
		}

		for _, path := range matches {
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

package variable

import (
	"os"
	"reflect"
	"regexp"
	"strings"

	"github.com/rigdev/rig/internal/config"
)

var varPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// shellUnsafeChars are characters that could enable shell injection.
var shellUnsafeChars = strings.NewReplacer(
	"`", "",
	"$", "",
	"!", "",
	"&", "",
	"|", "",
	";", "",
	"\n", " ",
	"\r", "",
)

// sanitizeForShell strips dangerous shell metacharacters from variable values
// that will be interpolated into shell commands.
func sanitizeForShell(val string) string {
	return shellUnsafeChars.Replace(val)
}

// Resolve replaces ${VAR_NAME} patterns in template with values from vars map.
// If a variable is not found in vars, it checks os.Getenv as fallback.
// If still not found, the original ${VAR_NAME} is preserved.
// Values from the vars map (user-controlled inputs like issue titles) are
// sanitized to prevent shell injection.
func Resolve(template string, vars map[string]string) string {
	return varPattern.ReplaceAllStringFunc(template, func(match string) string {
		// Extract variable name from ${...}
		varName := match[2 : len(match)-1]

		// Check if it's an env: prefixed variable
		if len(varName) > 4 && varName[:4] == "env:" {
			envVar := varName[4:]
			if val, ok := vars[envVar]; ok {
				return sanitizeForShell(val)
			}
			if val := os.Getenv(envVar); val != "" {
				return val // env vars are trusted
			}
			return match
		}

		// Regular variable lookup — sanitize since vars may contain user input
		if val, ok := vars[varName]; ok {
			return sanitizeForShell(val)
		}

		// Fallback to environment variable — trusted source, no sanitization
		if val := os.Getenv(varName); val != "" {
			return val
		}

		// Not found, preserve original
		return match
	})
}

// UnresolvedVars returns a list of variable names that are not in the vars map.
// It detects ${VAR_NAME} patterns and checks which ones are missing.
func UnresolvedVars(template string, vars map[string]string) []string {
	matches := varPattern.FindAllStringSubmatch(template, -1)
	var unresolved []string
	seen := make(map[string]bool)

	for _, match := range matches {
		varName := match[1]

		// Handle env: prefix
		if len(varName) > 4 && varName[:4] == "env:" {
			varName = varName[4:]
		}

		// Skip if already seen
		if seen[varName] {
			continue
		}
		seen[varName] = true

		// Check if variable is in vars map or environment
		if _, ok := vars[varName]; !ok {
			if os.Getenv(varName) == "" {
				unresolved = append(unresolved, varName)
			}
		}
	}

	return unresolved
}

// ResolveAll recursively traverses a Config struct and resolves all string fields.
// It returns a new Config with all ${VAR_NAME} patterns replaced.
func ResolveAll(cfg *config.Config, vars map[string]string) *config.Config {
	if cfg == nil {
		return nil
	}

	// Use reflection to create a deep copy with resolved values
	resolved := resolveValue(reflect.ValueOf(cfg), vars)
	return resolved.Interface().(*config.Config)
}

// resolveValue recursively resolves values in a reflect.Value
func resolveValue(v reflect.Value, vars map[string]string) reflect.Value {
	switch v.Kind() {
	case reflect.String:
		return reflect.ValueOf(Resolve(v.String(), vars))

	case reflect.Struct:
		// Create a new struct of the same type
		newStruct := reflect.New(v.Type()).Elem()
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			resolvedField := resolveValue(field, vars)
			newStruct.Field(i).Set(resolvedField)
		}
		return newStruct

	case reflect.Slice:
		// Create a new slice
		newSlice := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			elem := v.Index(i)
			resolvedElem := resolveValue(elem, vars)
			newSlice.Index(i).Set(resolvedElem)
		}
		return newSlice

	case reflect.Map:
		// Create a new map
		newMap := reflect.MakeMap(v.Type())
		for _, key := range v.MapKeys() {
			val := v.MapIndex(key)
			resolvedVal := resolveValue(val, vars)
			newMap.SetMapIndex(key, resolvedVal)
		}
		return newMap

	case reflect.Ptr:
		// Dereference, resolve, and re-wrap
		if v.IsNil() {
			return v
		}
		elem := v.Elem()
		resolvedElem := resolveValue(elem, vars)
		newPtr := reflect.New(elem.Type())
		newPtr.Elem().Set(resolvedElem)
		return newPtr

	case reflect.Interface:
		// Handle interface types
		if v.IsNil() {
			return v
		}
		elem := v.Elem()
		resolvedElem := resolveValue(elem, vars)
		return resolvedElem

	default:
		// For other types (int, bool, etc.), return as-is
		return v
	}
}

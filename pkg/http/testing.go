package http

import "strings"

// ParsePathParams extracts path parameters from URL patterns.
// This is a testing utility for matching URL patterns.
// Example: "/users/:id" matches "/users/123" -> {"id": "123"}
func ParsePathParams(pattern, path string) map[string]string {
	params := make(map[string]string)

	patternParts := strings.Split(strings.Trim(pattern, "/"), "/")
	pathParts := strings.Split(strings.Trim(path, "/"), "/")

	if len(patternParts) != len(pathParts) {
		return params
	}

	for i, part := range patternParts {
		if strings.HasPrefix(part, ":") {
			paramName := strings.TrimPrefix(part, ":")
			params[paramName] = pathParts[i]
		} else if part != pathParts[i] {
			return make(map[string]string) // No match
		}
	}

	return params
}

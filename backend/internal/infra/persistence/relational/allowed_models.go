package relational

import (
	"encoding/json"
	"strings"
)

func encodeAllowedModels(values []string) string {
	cleaned := normalizeAllowedModels(values)
	if len(cleaned) == 0 {
		return ""
	}
	raw, err := json.Marshal(cleaned)
	if err != nil {
		return ""
	}
	return string(raw)
}

func decodeAllowedModels(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil
	}
	return normalizeAllowedModels(values)
}

func normalizeAllowedModels(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

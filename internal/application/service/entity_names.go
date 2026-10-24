package service

import "reliquary/internal/types"

func normalizeEntityName(value string) string { return types.NormalizeEntityKey(value) }
func appendUniqueEntityString(items []string, value string) []string {
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}

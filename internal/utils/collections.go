package utils

import (
	"maps"
	"slices"
	"strings"
)

// ToInterfaceSlice converts a slice of strings to a slice of empty interfaces.
func ToInterfaceSlice[T any](slice []T) []interface{} {
	interfaceSlice := make([]interface{}, len(slice))
	for i, v := range slice {
		interfaceSlice[i] = v
	}
	return interfaceSlice
}

// []string -> string, " join, space separated
func StringSliceJoin(slice []string) string {
	result := make([]string, len(slice))
	for i, v := range slice {
		result[i] = `"` + v + `"`
	}
	return strings.Join(result, " ")
}

func GetAttrs[A, B any](extract func(A) B, attrs ...A) []B {
	result := make([]B, len(attrs))
	for i, attr := range attrs {
		result[i] = extract(attr)
	}
	return result
}

// Deduplicate removes duplicates from a slice based on a key function
// T: the type of elements in the slice
// K: the type of key used for deduplication
func Deduplicate[T any, K comparable](keyFunc func(T) K, items ...T) []T {
	seen := make(map[K]T)
	for _, item := range items {
		key := keyFunc(item)
		if _, exists := seen[key]; !exists {
			seen[key] = item
		}
	}
	return slices.Collect(maps.Values(seen))
}

// ScoreComparable is an interface for types that have a Score method returning float64
type ScoreComparable interface {
	GetScore() float64
}

// DeduplicateWithScore removes duplicates from a slice based on a key function,
// keeping the item with the highest score for each key, then sorts by score descending
// T: the type of elements in the slice (must implement ScoreComparable)
// K: the type of key used for deduplication
func DeduplicateWithScore[T ScoreComparable, K comparable](keyFunc func(T) K, items ...T) []T {
	seen := make(map[K]T)
	for _, item := range items {
		key := keyFunc(item)
		if existing, exists := seen[key]; !exists {
			seen[key] = item
		} else if item.GetScore() > existing.GetScore() {
			seen[key] = item
		}
	}
	result := slices.Collect(maps.Values(seen))
	// Sort by score descending
	slices.SortFunc(result, func(a, b T) int {
		scoreA := a.GetScore()
		scoreB := b.GetScore()
		if scoreA > scoreB {
			return -1
		} else if scoreA < scoreB {
			return 1
		}
		return 0
	})
	return result
}

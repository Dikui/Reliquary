// Package sourceloc exposes saved Markdown coordinate assignment.
package sourceloc

import "reliquary/internal/types"

func Assign(body string, chunks []*types.Chunk) { types.AssignSourceLocations(body, chunks) }

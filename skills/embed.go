// Package skills embeds the Reliquary agent skill files.
package skills

import "embed"

// Files contains the skill directory that can also be copied to agent clients.
//
//go:embed reliquary
var Files embed.FS

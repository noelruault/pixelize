// Package palettes ships example palettes and resolves palette names
// against the user's config dir, then the embedded examples.
package palettes

import "embed"

//go:embed *.csv
var Embedded embed.FS

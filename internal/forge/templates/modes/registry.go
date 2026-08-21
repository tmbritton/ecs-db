// Package modes holds the per-mode content components the shell renders.
//
// Modes register themselves here rather than the shell knowing about them,
// which is what keeps the shell free of mode-specific knowledge.
package modes

import "github.com/a-h/templ"

// Registry maps a mode slug to its content. Keeping it a lookup rather than a
// switch means the handler does not grow a branch per mode, and
// registry_test.go can check it against the mode table in both directions.
var Registry = map[string]func() templ.Component{
	"map":     Map,
	"tiles":   Tiles,
	"ents":    Ents,
	"schema":  Schema,
	"agents":  Agents,
	"sprites": Sprites,
}

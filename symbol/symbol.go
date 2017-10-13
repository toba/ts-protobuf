package symbol

import "github.com/toba/ts-protobuf/generator"

// symbol is an interface representing an exported Go symbol.
type Symbol interface {
	// GenerateAlias should generate an appropriate alias for the symbol from the
	// named package.
	GenerateAlias(g *generator.Generator, pkg string)
}

package main

// symbol is an interface representing an exported Go symbol.
type symbol interface {
	// GenerateAlias should generate an appropriate alias for the symbol from the
	// named package.
	GenerateAlias(g *Generator, pkg string)
}

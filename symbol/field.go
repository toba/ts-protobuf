package symbol

import "github.com/toba/ts-protobuf/generator"

type constOrVarSymbol struct {
	sym  string
	typ  string // either "const" or "var"
	cast string // if non-empty, a type cast is required (used for enums)
}

func (cs constOrVarSymbol) GenerateAlias(g *generator.Generator, pkg string) {
	v := pkg + "." + cs.sym
	if cs.cast != "" {
		v = cs.cast + "(" + v + ")"
	}
	g.P(cs.typ, " ", cs.sym, " = ", v)
}

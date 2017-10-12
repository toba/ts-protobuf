package main

type (
	// symbol is an interface representing an exported Go symbol.
	symbol interface {
		// GenerateAlias should generate an appropriate alias
		// for the symbol from the named package.
		GenerateAlias(g *Generator, pkg string)
	}

	enumSymbol struct {
		name   string
		proto3 bool // Whether this came from a proto3 file.
	}

	messageSymbol struct {
		sym                         string
		hasExtensions, isMessageSet bool
		hasOneof                    bool
		getters                     []getterSymbol
	}

	getterSymbol struct {
		name     string
		typ      string
		typeName string // canonical name in proto world; empty for proto.Message and similar
		genType  bool   // whether typ contains a generated type (message/group/enum)
	}

	constOrVarSymbol struct {
		sym  string
		typ  string // either "const" or "var"
		cast string // if non-empty, a type cast is required (used for enums)
	}
)

func (es enumSymbol) GenerateAlias(g *Generator, pkg string) {
	s := es.name
	g.P("type ", s, " ", pkg, ".", s)
	g.P("var ", s, "_name = ", pkg, ".", s, "_name")
	g.P("var ", s, "_value = ", pkg, ".", s, "_value")
	g.P("func (x ", s, ") String() string { return (", pkg, ".", s, ")(x).String() }")
	if !es.proto3 {
		g.P("func (x ", s, ") Enum() *", s, "{ return (*", s, ")((", pkg, ".", s, ")(x).Enum()) }")
		g.P("func (x *", s, ") UnmarshalJSON(data []byte) error { return (*", pkg, ".", s, ")(x).UnmarshalJSON(data) }")
	}
}

func (cs constOrVarSymbol) GenerateAlias(g *Generator, pkg string) {
	v := pkg + "." + cs.sym
	if cs.cast != "" {
		v = cs.cast + "(" + v + ")"
	}
	g.P(cs.typ, " ", cs.sym, " = ", v)
}

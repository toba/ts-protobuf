package symbol

import "github.com/toba/ts-protobuf/generator"

type enumSymbol struct {
	name   string
	proto3 bool // Whether this came from a proto3 file.
}

func (es enumSymbol) GenerateAlias(g *generator.Generator, pkg string) {
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

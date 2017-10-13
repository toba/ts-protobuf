package symbol

type getterSymbol struct {
	name     string
	typ      string
	typeName string // canonical name in proto world; empty for proto.Message and similar
	genType  bool   // whether typ contains a generated type (message/group/enum)
}

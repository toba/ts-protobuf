package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/golang/protobuf/protoc-gen-go/descriptor"
)

// Generate the enum definitions for this EnumDescriptor.
func (g *Generator) generateEnum(enum *enumDescriptor) {
	// The full type name
	typeName := enum.TypeName()
	// The full type name, CamelCased.
	ccTypeName := CamelCaseSlice(typeName)
	ccPrefix := enum.prefix()

	g.PrintComments(enum.path)
	g.P("type ", ccTypeName, " int32")
	g.file.addExport(enum, enumSymbol{ccTypeName, enum.proto3()})
	g.P("const (")
	g.In()
	for i, e := range enum.Value {
		g.PrintComments(fmt.Sprintf("%s,%d,%d", enum.path, enumValuePath, i))

		name := ccPrefix + *e.Name
		g.P(name, " ", ccTypeName, " = ", e.Number)
		g.file.addExport(enum, constOrVarSymbol{name, "const", ccTypeName})
	}
	g.Out()
	g.P(")")
	g.P("var ", ccTypeName, "_name = map[int32]string{")
	g.In()
	generated := make(map[int32]bool) // avoid duplicate values
	for _, e := range enum.Value {
		duplicate := ""
		if _, present := generated[*e.Number]; present {
			duplicate = "// Duplicate value: "
		}
		g.P(duplicate, e.Number, ": ", strconv.Quote(*e.Name), ",")
		generated[*e.Number] = true
	}
	g.Out()
	g.P("}")
	g.P("var ", ccTypeName, "_value = map[string]int32{")
	g.In()
	for _, e := range enum.Value {
		g.P(strconv.Quote(*e.Name), ": ", e.Number, ",")
	}
	g.Out()
	g.P("}")

	if !enum.proto3() {
		g.P("func (x ", ccTypeName, ") Enum() *", ccTypeName, " {")
		g.In()
		g.P("p := new(", ccTypeName, ")")
		g.P("*p = x")
		g.P("return p")
		g.Out()
		g.P("}")
	}

	g.P("func (x ", ccTypeName, ") String() string {")
	g.In()
	g.P("return ", g.Pkg["proto"], ".EnumName(", ccTypeName, "_name, int32(x))")
	g.Out()
	g.P("}")

	if !enum.proto3() {
		g.P("func (x *", ccTypeName, ") UnmarshalJSON(data []byte) error {")
		g.In()
		g.P("value, err := ", g.Pkg["proto"], ".UnmarshalJSONEnum(", ccTypeName, `_value, data, "`, ccTypeName, `")`)
		g.P("if err != nil {")
		g.In()
		g.P("return err")
		g.Out()
		g.P("}")
		g.P("*x = ", ccTypeName, "(value)")
		g.P("return nil")
		g.Out()
		g.P("}")
	}

	var indexes []string
	for m := enum.message; m != nil; m = m.parent {
		// XXX: skip groups?
		indexes = append([]string{strconv.Itoa(m.index)}, indexes...)
	}
	indexes = append(indexes, strconv.Itoa(enum.index))
	g.P("func (", ccTypeName, ") EnumDescriptor() ([]byte, []int) { return ", g.file.VarName(), ", []int{", strings.Join(indexes, ", "), "} }")
	if enum.file.GetPackage() == "google.protobuf" && enum.GetName() == "NullValue" {
		g.P("func (", ccTypeName, `) XXX_WellKnownType() string { return "`, enum.GetName(), `" }`)
	}

	g.P()
}

func (g *Generator) generateEnumRegistration(enum *enumDescriptor) {
	// We always print the full (proto-world) package name here.
	pkg := enum.File().GetPackage()
	if pkg != "" {
		pkg += "."
	}
	// The full type name
	typeName := enum.TypeName()
	// The full type name, CamelCased.
	ccTypeName := CamelCaseSlice(typeName)
	g.addInitf("%s.RegisterEnum(%q, %[3]s_name, %[3]s_value)", g.Pkg["proto"], pkg+ccTypeName, ccTypeName)
}

func (g *Generator) buildNestedEnums(descs []*messageDescriptor, enums []*enumDescriptor) {
	for _, desc := range descs {
		if len(desc.EnumType) != 0 {
			for _, enum := range enums {
				if enum.message == desc {
					desc.enums = append(desc.enums, enum)
				}
			}
			if len(desc.enums) != len(desc.EnumType) {
				g.Fail("internal error: enum nesting failure for", desc.GetName())
			}
		}
	}
}

type enumSymbol struct {
	name   string
	proto3 bool // Whether this came from a proto3 file.
}

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

// EnumDescriptor describes an enum. If it's at top level, its parent will be
// nil. Otherwise it will be the descriptor of the message in which it is
// defined.
type enumDescriptor struct {
	common
	*descriptor.EnumDescriptorProto
	message   *messageDescriptor // The containing message, if any.
	typeNames []string           // Cached typename vector.
	index     int                // The index into the container, whether the file or a message.
	path      string             // The SourceCodeInfo path as comma-separated integers.
}

// newEnum constructs an EnumDescriptor.
func newEnum(desc *descriptor.EnumDescriptorProto, msg *messageDescriptor, file *descriptor.FileDescriptorProto, index int) *enumDescriptor {
	ed := &enumDescriptor{
		common:              common{file},
		EnumDescriptorProto: desc,
		message:             msg,
		index:               index,
	}
	if msg == nil {
		ed.path = fmt.Sprintf("%d,%d", enumPath, index)
	} else {
		ed.path = fmt.Sprintf("%s,%d,%d", msg.path, messageEnumPath, index)
	}
	return ed
}

// wrapEnums builds a slice of EnumDescriptors defined within a file.
func wrapEnums(file *descriptor.FileDescriptorProto, descs []*messageDescriptor) []*enumDescriptor {
	sl := make([]*enumDescriptor, 0, len(file.EnumType)+10)
	// Top-level enums.
	for i, enum := range file.EnumType {
		sl = append(sl, newEnum(enum, nil, file, i))
	}
	// Enums within messages. Enums within embedded messages appear in the outer-most message.
	for _, nested := range descs {
		for i, enum := range nested.EnumType {
			sl = append(sl, newEnum(enum, nested, file, i))
		}
	}
	return sl
}

// TypeName returns the elements of the dotted type name.
// The package name is not part of this name.
func (e *enumDescriptor) TypeName() (s []string) {
	if e.typeNames != nil {
		return e.typeNames
	}
	name := e.GetName()
	if e.message == nil {
		s = make([]string, 1)
	} else {
		pname := e.message.TypeName()
		s = make([]string, len(pname)+1)
		copy(s, pname)
	}
	s[len(s)-1] = name
	e.typeNames = s
	return s
}

// Everything but the last element of the full type name, CamelCased.
// The values of type Foo.Bar are call Foo_value1... not Foo_Bar_value1... .
func (e *enumDescriptor) prefix() string {
	if e.message == nil {
		// If the enum is not part of a message, the prefix is just the type name.
		return CamelCase(*e.Name) + "_"
	}
	typeName := e.TypeName()
	return CamelCaseSlice(typeName[0:len(typeName)-1]) + "_"
}

// The integer value of the named constant in this enumerated type.
func (e *enumDescriptor) integerValueAsString(name string) string {
	for _, c := range e.Value {
		if c.GetName() == name {
			return fmt.Sprint(c.GetNumber())
		}
	}
	log.Fatal("cannot find value for enum constant")
	return ""
}

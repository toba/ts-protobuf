package main

import (
	"strings"

	"github.com/toba/ts-protobuf/descriptor"
	"github.com/toba/ts-protobuf/symbol"
)

// ExtensionDescriptor describes an extension. If it's at top level, its
// parent will be nil. Otherwise it will be the descriptor of the message in
// which it is defined.
type extensionDescriptor struct {
	common
	*descriptor.FieldDescriptorProto
	message *messageDescriptor // The containing message, if any.
}

// TypeName returns the elements of the dotted type name.
// The package name is not part of this name.
func (e *extensionDescriptor) TypeName() (s []string) {
	name := e.GetName()
	if e.message == nil {
		// top-level extension
		s = make([]string, 1)
	} else {
		pname := e.message.TypeName()
		s = make([]string, len(pname)+1)
		copy(s, pname)
	}
	s[len(s)-1] = name
	return s
}

// DescName returns the variable name used for the generated descriptor.
func (e *extensionDescriptor) DescName() string {
	// The full type name.
	typeName := e.TypeName()
	// Each scope of the extension is individually CamelCased, and all are joined
	// with "_" with an "E_" prefix.
	for i, s := range typeName {
		typeName[i] = CamelCase(s)
	}
	return "E_" + strings.Join(typeName, "_")
}

// Return a slice of all the top-level ExtensionDescriptors defined within this
// file.
func wrapExtensions(file *descriptor.FileDescriptorProto) []*extensionDescriptor {
	var sl []*=extensionDescriptor
	for _, field := range file.Extension {
		sl = append(sl, &extensionDescriptor{common{file}, field, nil})
	}
	return sl
}

func (g *Generator) generateExtension(ext *extensionDescriptor) {
	ccTypeName := ext.DescName()

	extObj := g.ObjectNamed(*ext.Extendee)
	var extDesc *messageDescriptor
	if id, ok := extObj.(*importDescriptor); ok {
		// This is extending a publicly imported message.
		// We need the underlying type for goTag.
		extDesc = id.o.(*messageDescriptor)
	} else {
		extDesc = extObj.(*messageDescriptor)
	}
	extendedType := "*" + g.TypeName(extObj) // always use the original
	field := ext.FieldDescriptorProto
	fieldType, wireType := g.GoType(ext.parent, field)
	tag := g.goTag(extDesc, field, wireType)
	g.RecordTypeUse(*ext.Extendee)
	if n := ext.FieldDescriptorProto.TypeName; n != nil {
		// foreign extension type
		g.RecordTypeUse(*n)
	}

	typeName := ext.TypeName()

	// Special case for proto2 message sets: If this extension is extending
	// proto2_bridge.MessageSet, and its final name component is "message_set_extension",
	// then drop that last component.
	mset := false
	if extendedType == "*proto2_bridge.MessageSet" && typeName[len(typeName)-1] == "message_set_extension" {
		typeName = typeName[:len(typeName)-1]
		mset = true
	}

	// For text formatting, the package must be exactly what the .proto file declares,
	// ignoring overrides such as the go_package option, and with no dot/underscore mapping.
	extName := strings.Join(typeName, ".")
	if g.file.Package != nil {
		extName = *g.file.Package + "." + extName
	}

	g.P("var ", ccTypeName, " = &", g.Pkg["proto"], ".ExtensionDesc{")
	g.In()
	g.P("ExtendedType: (", extendedType, ")(nil),")
	g.P("ExtensionType: (", fieldType, ")(nil),")
	g.P("Field: ", field.Number, ",")
	g.P(`Name: "`, extName, `",`)
	g.P("Tag: ", tag, ",")
	g.P(`Filename: "`, g.file.GetName(), `",`)

	g.Out()
	g.P("}")
	g.P()

	if mset {
		// Generate a bit more code to register with message_set.go.
		g.addInitf("%s.RegisterMessageSetType((%s)(nil), %d, %q)", g.Pkg["proto"], fieldType, *field.Number, extName)
	}

	g.file.addExport(ext, constOrVarSymbol{ccTypeName, "var", ""})
}

func (g *Generator) generateExtensionRegistration(ext *extensionDescriptor) {
	g.addInitf("%s.RegisterExtension(%s)", g.Pkg["proto"], ext.DescName())
}

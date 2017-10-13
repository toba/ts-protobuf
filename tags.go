package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/golang/protobuf/protoc-gen-go/descriptor"
)

// The tag is a string like "varint,2,opt,name=fieldname,def=7" that
// identifies details of the field for the protocol buffer marshaling and unmarshaling
// code.  The fields are:
//	wire encoding
//	protocol tag number
//	opt,req,rep for optional, required, or repeated
//	packed whether the encoding is "packed" (optional; repeated primitives only)
//	name= the original declared name
//	enum= the name of the enum type if it is an enum-typed field.
//	proto3 if this field is in a proto3 message
//	def= string representation of the default value, if any.
// The default value must be in a representation that can be used at run-time
// to generate the default value. Thus bools become 0 and 1, for instance.
func (g *Generator) goTag(message *messageDescriptor, field *descriptor.FieldDescriptorProto, wiretype string) string {
	optrepreq := ""
	switch {
	case isOptional(field):
		optrepreq = "opt"
	case isRequired(field):
		optrepreq = "req"
	case isRepeated(field):
		optrepreq = "rep"
	}
	var defaultValue string
	if dv := field.DefaultValue; dv != nil { // set means an explicit default
		defaultValue = *dv
		// Some types need tweaking.
		switch *field.Type {
		case descriptor.FieldDescriptorProto_TYPE_BOOL:
			if defaultValue == "true" {
				defaultValue = "1"
			} else {
				defaultValue = "0"
			}
		case descriptor.FieldDescriptorProto_TYPE_STRING,
			descriptor.FieldDescriptorProto_TYPE_BYTES:
			// Nothing to do. Quoting is done for the whole tag.
		case descriptor.FieldDescriptorProto_TYPE_ENUM:
			// For enums we need to provide the integer constant.
			obj := g.ObjectNamed(field.GetTypeName())
			if id, ok := obj.(*ImportedDescriptor); ok {
				// It is an enum that was publicly imported.
				// We need the underlying type.
				obj = id.o
			}
			enum, ok := obj.(*enumDescriptor)
			if !ok {
				log.Printf("obj is a %T", obj)
				if id, ok := obj.(*ImportedDescriptor); ok {
					log.Printf("id.o is a %T", id.o)
				}
				g.Fail("unknown enum type", CamelCaseSlice(obj.TypeName()))
			}
			defaultValue = enum.integerValueAsString(defaultValue)
		}
		defaultValue = ",def=" + defaultValue
	}
	enum := ""
	if *field.Type == descriptor.FieldDescriptorProto_TYPE_ENUM {
		// We avoid using obj.PackageName(), because we want to use the
		// original (proto-world) package name.
		obj := g.ObjectNamed(field.GetTypeName())
		if id, ok := obj.(*ImportedDescriptor); ok {
			obj = id.o
		}
		enum = ",enum="
		if pkg := obj.File().GetPackage(); pkg != "" {
			enum += pkg + "."
		}
		enum += CamelCaseSlice(obj.TypeName())
	}
	packed := ""
	if (field.Options != nil && field.Options.GetPacked()) ||
		// Per https://developers.google.com/protocol-buffers/docs/proto3#simple:
		// "In proto3, repeated fields of scalar numeric types use packed encoding by default."
		(message.proto3() && (field.Options == nil || field.Options.Packed == nil) &&
			isRepeated(field) && isScalar(field)) {
		packed = ",packed"
	}
	fieldName := field.GetName()
	name := fieldName
	if *field.Type == descriptor.FieldDescriptorProto_TYPE_GROUP {
		// We must use the type name for groups instead of
		// the field name to preserve capitalization.
		// type_name in FieldDescriptorProto is fully-qualified,
		// but we only want the local part.
		name = *field.TypeName
		if i := strings.LastIndex(name, "."); i >= 0 {
			name = name[i+1:]
		}
	}
	if json := field.GetJsonName(); json != "" && json != name {
		// TODO: escaping might be needed, in which case
		// perhaps this should be in its own "json" tag.
		name += ",json=" + json
	}
	name = ",name=" + name
	if message.proto3() {
		// We only need the extra tag for []byte fields;
		// no need to add noise for the others.
		if *field.Type == descriptor.FieldDescriptorProto_TYPE_BYTES {
			name += ",proto3"
		}

	}
	oneof := ""
	if field.OneofIndex != nil {
		oneof = ",oneof"
	}
	return strconv.Quote(fmt.Sprintf("%s,%d,%s%s%s%s%s%s",
		wiretype,
		field.GetNumber(),
		optrepreq,
		packed,
		name,
		enum,
		oneof,
		defaultValue))
}

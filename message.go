package main

import (
	"bytes"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/golang/protobuf/protoc-gen-go/descriptor"
)

// Method names that may be generated. Fields with these names get an
// underscore appended. Any change to this set is a potential incompatible
// API change because it changes generated field names.
var methodNames = [...]string{
	"Reset",
	"String",
	"ProtoMessage",
	"Marshal",
	"Unmarshal",
	"ExtensionRangeArray",
	"ExtensionMap",
	"Descriptor",
}

// Generate the type and default constant definitions for this Descriptor.
func (g *Generator) generateMessage(message *messageDescriptor) {
	// The full type name
	typeName := message.TypeName()
	// The full type name, CamelCased.
	ccTypeName := CamelCaseSlice(typeName)

	usedNames := make(map[string]bool)
	for _, n := range methodNames {
		usedNames[n] = true
	}
	fieldNames := make(map[*descriptor.FieldDescriptorProto]string)
	fieldGetterNames := make(map[*descriptor.FieldDescriptorProto]string)
	fieldTypes := make(map[*descriptor.FieldDescriptorProto]string)
	mapFieldTypes := make(map[*descriptor.FieldDescriptorProto]string)

	oneofFieldName := make(map[int32]string)                           // indexed by oneof_index field of FieldDescriptorProto
	oneofDisc := make(map[int32]string)                                // name of discriminator method
	oneofTypeName := make(map[*descriptor.FieldDescriptorProto]string) // without star
	oneofInsertPoints := make(map[int32]int)                           // oneof_index => offset of g.Buffer

	g.PrintComments(message.path)
	g.P("type ", ccTypeName, " struct {")
	g.In()

	// allocNames finds a conflict-free variation of the given strings,
	// consistently mutating their suffixes.
	// It returns the same number of strings.
	allocNames := func(ns ...string) []string {
	Loop:
		for {
			for _, n := range ns {
				if usedNames[n] {
					for i := range ns {
						ns[i] += "_"
					}
					continue Loop
				}
			}
			for _, n := range ns {
				usedNames[n] = true
			}
			return ns
		}
	}

	for i, field := range message.Field {
		// Allocate the getter and the field at the same time so name
		// collisions create field/method consistent names.
		// TODO: This allocation occurs based on the order of the fields
		// in the proto file, meaning that a change in the field
		// ordering can change generated Method/Field names.
		base := CamelCase(*field.Name)
		ns := allocNames(base, "Get"+base)
		fieldName, fieldGetterName := ns[0], ns[1]
		typename, wiretype := g.GoType(message, field)
		jsonName := *field.Name
		tag := fmt.Sprintf("protobuf:%s json:%q", g.goTag(message, field, wiretype), jsonName+",omitempty")

		fieldNames[field] = fieldName
		fieldGetterNames[field] = fieldGetterName

		oneof := field.OneofIndex != nil
		if oneof && oneofFieldName[*field.OneofIndex] == "" {
			odp := message.OneofDecl[int(*field.OneofIndex)]
			fname := allocNames(CamelCase(odp.GetName()))[0]

			// This is the first field of a oneof we haven't seen before.
			// Generate the union field.
			com := g.PrintComments(fmt.Sprintf("%s,%d,%d", message.path, messageOneofPath, *field.OneofIndex))
			if com {
				g.P("//")
			}
			g.P("// Types that are valid to be assigned to ", fname, ":")
			// Generate the rest of this comment later,
			// when we've computed any disambiguation.
			oneofInsertPoints[*field.OneofIndex] = g.Buffer.Len()

			dname := "is" + ccTypeName + "_" + fname
			oneofFieldName[*field.OneofIndex] = fname
			oneofDisc[*field.OneofIndex] = dname
			tag := `protobuf_oneof:"` + odp.GetName() + `"`
			g.P(fname, " ", dname, " `", tag, "`")
		}

		if *field.Type == descriptor.FieldDescriptorProto_TYPE_MESSAGE {
			desc := g.ObjectNamed(field.GetTypeName())
			if d, ok := desc.(*messageDescriptor); ok && d.GetOptions().GetMapEntry() {
				// Figure out the Go types and tags for the key and value types.
				keyField, valField := d.Field[0], d.Field[1]
				keyType, keyWire := g.GoType(d, keyField)
				valType, valWire := g.GoType(d, valField)
				keyTag, valTag := g.goTag(d, keyField, keyWire), g.goTag(d, valField, valWire)

				// We don't use stars, except for message-typed values.
				// Message and enum types are the only two possibly foreign types used in maps,
				// so record their use. They are not permitted as map keys.
				keyType = strings.TrimPrefix(keyType, "*")
				switch *valField.Type {
				case descriptor.FieldDescriptorProto_TYPE_ENUM:
					valType = strings.TrimPrefix(valType, "*")
					g.RecordTypeUse(valField.GetTypeName())
				case descriptor.FieldDescriptorProto_TYPE_MESSAGE:
					g.RecordTypeUse(valField.GetTypeName())
				default:
					valType = strings.TrimPrefix(valType, "*")
				}

				typename = fmt.Sprintf("map[%s]%s", keyType, valType)
				mapFieldTypes[field] = typename // record for the getter generation

				tag += fmt.Sprintf(" protobuf_key:%s protobuf_val:%s", keyTag, valTag)
			}
		}

		fieldTypes[field] = typename

		if oneof {
			tname := ccTypeName + "_" + fieldName
			// It is possible for this to collide with a message or enum
			// nested in this message. Check for collisions.
			for {
				ok := true
				for _, desc := range message.nested {
					if CamelCaseSlice(desc.TypeName()) == tname {
						ok = false
						break
					}
				}
				for _, enum := range message.enums {
					if CamelCaseSlice(enum.TypeName()) == tname {
						ok = false
						break
					}
				}
				if !ok {
					tname += "_"
					continue
				}
				break
			}

			oneofTypeName[field] = tname
			continue
		}

		g.PrintComments(fmt.Sprintf("%s,%d,%d", message.path, messageFieldPath, i))
		g.P(fieldName, "\t", typename, "\t`", tag, "`")
		g.RecordTypeUse(field.GetTypeName())
	}
	if len(message.ExtensionRange) > 0 {
		g.P(g.Pkg["proto"], ".XXX_InternalExtensions `json:\"-\"`")
	}
	if !message.proto3() {
		g.P("XXX_unrecognized\t[]byte `json:\"-\"`")
	}
	g.Out()
	g.P("}")

	// Update g.Buffer to list valid oneof types.
	// We do this down here, after we've disambiguated the oneof type names.
	// We go in reverse order of insertion point to avoid invalidating offsets.
	for oi := int32(len(message.OneofDecl)); oi >= 0; oi-- {
		ip := oneofInsertPoints[oi]
		all := g.Buffer.Bytes()
		rem := all[ip:]
		g.Buffer = bytes.NewBuffer(all[:ip:ip]) // set cap so we don't scribble on rem
		for _, field := range message.Field {
			if field.OneofIndex == nil || *field.OneofIndex != oi {
				continue
			}
			g.P("//\t*", oneofTypeName[field])
		}
		g.Buffer.Write(rem)
	}

	// Reset, String and ProtoMessage methods.
	g.P("func (m *", ccTypeName, ") Reset() { *m = ", ccTypeName, "{} }")
	g.P("func (m *", ccTypeName, ") String() string { return ", g.Pkg["proto"], ".CompactTextString(m) }")
	g.P("func (*", ccTypeName, ") ProtoMessage() {}")
	var indexes []string
	for m := message; m != nil; m = m.parent {
		indexes = append([]string{strconv.Itoa(m.index)}, indexes...)
	}
	g.P("func (*", ccTypeName, ") Descriptor() ([]byte, []int) { return ", g.file.VarName(), ", []int{", strings.Join(indexes, ", "), "} }")
	// TODO: Revisit the decision to use a XXX_WellKnownType method
	// if we change descriptor.MessageName to work with multiple equivalents.
	if message.file.GetPackage() == "google.protobuf" && wellKnownTypes[message.GetName()] {
		g.P("func (*", ccTypeName, `) XXX_WellKnownType() string { return "`, message.GetName(), `" }`)
	}

	// Extension support methods
	var hasExtensions, isMessageSet bool
	if len(message.ExtensionRange) > 0 {
		hasExtensions = true
		// message_set_wire_format only makes sense when extensions are defined.
		if opts := message.Options; opts != nil && opts.GetMessageSetWireFormat() {
			isMessageSet = true
			g.P()
			g.P("func (m *", ccTypeName, ") Marshal() ([]byte, error) {")
			g.In()
			g.P("return ", g.Pkg["proto"], ".MarshalMessageSet(&m.XXX_InternalExtensions)")
			g.Out()
			g.P("}")
			g.P("func (m *", ccTypeName, ") Unmarshal(buf []byte) error {")
			g.In()
			g.P("return ", g.Pkg["proto"], ".UnmarshalMessageSet(buf, &m.XXX_InternalExtensions)")
			g.Out()
			g.P("}")
			g.P("func (m *", ccTypeName, ") MarshalJSON() ([]byte, error) {")
			g.In()
			g.P("return ", g.Pkg["proto"], ".MarshalMessageSetJSON(&m.XXX_InternalExtensions)")
			g.Out()
			g.P("}")
			g.P("func (m *", ccTypeName, ") UnmarshalJSON(buf []byte) error {")
			g.In()
			g.P("return ", g.Pkg["proto"], ".UnmarshalMessageSetJSON(buf, &m.XXX_InternalExtensions)")
			g.Out()
			g.P("}")
			g.P("// ensure ", ccTypeName, " satisfies descriptor.Marshaler and descriptor.Unmarshaler")
			g.P("var _ ", g.Pkg["proto"], ".Marshaler = (*", ccTypeName, ")(nil)")
			g.P("var _ ", g.Pkg["proto"], ".Unmarshaler = (*", ccTypeName, ")(nil)")
		}

		g.P()
		g.P("var extRange_", ccTypeName, " = []", g.Pkg["proto"], ".ExtensionRange{")
		g.In()
		for _, r := range message.ExtensionRange {
			end := fmt.Sprint(*r.End - 1) // make range inclusive on both ends
			g.P("{", r.Start, ", ", end, "},")
		}
		g.Out()
		g.P("}")
		g.P("func (*", ccTypeName, ") ExtensionRangeArray() []", g.Pkg["proto"], ".ExtensionRange {")
		g.In()
		g.P("return extRange_", ccTypeName)
		g.Out()
		g.P("}")
	}

	// Default constants
	defNames := make(map[*descriptor.FieldDescriptorProto]string)
	for _, field := range message.Field {
		def := field.GetDefaultValue()
		if def == "" {
			continue
		}
		fieldname := "Default_" + ccTypeName + "_" + CamelCase(*field.Name)
		defNames[field] = fieldname
		typename, _ := g.GoType(message, field)
		if typename[0] == '*' {
			typename = typename[1:]
		}
		kind := "const "
		switch {
		case typename == "bool":
		case typename == "string":
			def = strconv.Quote(def)
		case typename == "[]byte":
			def = "[]byte(" + strconv.Quote(unescape(def)) + ")"
			kind = "var "
		case def == "inf", def == "-inf", def == "nan":
			// These names are known to, and defined by, the protocol language.
			switch def {
			case "inf":
				def = "math.Inf(1)"
			case "-inf":
				def = "math.Inf(-1)"
			case "nan":
				def = "math.NaN()"
			}
			if *field.Type == descriptor.FieldDescriptorProto_TYPE_FLOAT {
				def = "float32(" + def + ")"
			}
			kind = "var "
		case *field.Type == descriptor.FieldDescriptorProto_TYPE_ENUM:
			// Must be an enum.  Need to construct the prefixed name.
			obj := g.ObjectNamed(field.GetTypeName())
			var enum *enumDescriptor
			if id, ok := obj.(*importDescriptor); ok {
				// The enum type has been publicly imported.
				enum, _ = id.o.(*enumDescriptor)
			} else {
				enum, _ = obj.(*enumDescriptor)
			}
			if enum == nil {
				log.Printf("don't know how to generate constant for %s", fieldname)
				continue
			}
			def = g.DefaultPackageName(obj) + enum.prefix() + def
		}
		g.P(kind, fieldname, " ", typename, " = ", def)
		g.file.addExport(message, constOrVarSymbol{fieldname, kind, ""})
	}
	g.P()

	// Oneof per-field types, discriminants and getters.
	//
	// Generate unexported named types for the discriminant interfaces.
	// We shouldn't have to do this, but there was (~19 Aug 2015) a compiler/linker bug
	// that was triggered by using anonymous interfaces here.
	// TODO: Revisit this and consider reverting back to anonymous interfaces.
	for oi := range message.OneofDecl {
		dname := oneofDisc[int32(oi)]
		g.P("type ", dname, " interface { ", dname, "() }")
	}
	g.P()
	for _, field := range message.Field {
		if field.OneofIndex == nil {
			continue
		}
		_, wiretype := g.GoType(message, field)
		tag := "protobuf:" + g.goTag(message, field, wiretype)
		g.P("type ", oneofTypeName[field], " struct{ ", fieldNames[field], " ", fieldTypes[field], " `", tag, "` }")
		g.RecordTypeUse(field.GetTypeName())
	}
	g.P()
	for _, field := range message.Field {
		if field.OneofIndex == nil {
			continue
		}
		g.P("func (*", oneofTypeName[field], ") ", oneofDisc[*field.OneofIndex], "() {}")
	}
	g.P()
	for oi := range message.OneofDecl {
		fname := oneofFieldName[int32(oi)]
		g.P("func (m *", ccTypeName, ") Get", fname, "() ", oneofDisc[int32(oi)], " {")
		g.P("if m != nil { return m.", fname, " }")
		g.P("return nil")
		g.P("}")
	}
	g.P()

	// Field getters
	var getters []getterSymbol
	for _, field := range message.Field {
		oneof := field.OneofIndex != nil

		fname := fieldNames[field]
		typename, _ := g.GoType(message, field)
		if t, ok := mapFieldTypes[field]; ok {
			typename = t
		}
		mname := fieldGetterNames[field]
		star := ""
		if needsStar(*field.Type) && typename[0] == '*' {
			typename = typename[1:]
			star = "*"
		}

		// Only export getter symbols for basic types,
		// and for messages and enums in the same package.
		// Groups are not exported.
		// Foreign types can't be hoisted through a public import because
		// the importer may not already be importing the defining .descriptor.
		// As an example, imagine we have an import tree like this:
		//   A.proto -> B.proto -> C.proto
		// If A publicly imports B, we need to generate the getters from B in A's output,
		// but if one such getter returns something from C then we cannot do that
		// because A is not importing C already.
		var getter, genType bool
		switch *field.Type {
		case descriptor.FieldDescriptorProto_TYPE_GROUP:
			getter = false
		case descriptor.FieldDescriptorProto_TYPE_MESSAGE, descriptor.FieldDescriptorProto_TYPE_ENUM:
			// Only export getter if its return type is in this package.
			getter = g.ObjectNamed(field.GetTypeName()).PackageName() == message.PackageName()
			genType = true
		default:
			getter = true
		}
		if getter {
			getters = append(getters, getterSymbol{
				name:     mname,
				typ:      typename,
				typeName: field.GetTypeName(),
				genType:  genType,
			})
		}

		g.P("func (m *", ccTypeName, ") "+mname+"() "+typename+" {")
		g.In()
		def, hasDef := defNames[field]
		typeDefaultIsNil := false // whether this field type's default value is a literal nil unless specified
		switch *field.Type {
		case descriptor.FieldDescriptorProto_TYPE_BYTES:
			typeDefaultIsNil = !hasDef
		case descriptor.FieldDescriptorProto_TYPE_GROUP, descriptor.FieldDescriptorProto_TYPE_MESSAGE:
			typeDefaultIsNil = true
		}
		if isRepeated(field) {
			typeDefaultIsNil = true
		}
		if typeDefaultIsNil && !oneof {
			// A bytes field with no explicit default needs less generated code,
			// as does a message or group field, or a repeated field.
			g.P("if m != nil {")
			g.In()
			g.P("return m." + fname)
			g.Out()
			g.P("}")
			g.P("return nil")
			g.Out()
			g.P("}")
			g.P()
			continue
		}
		if !oneof {
			if message.proto3() {
				g.P("if m != nil {")
			} else {
				g.P("if m != nil && m." + fname + " != nil {")
			}
			g.In()
			g.P("return " + star + "m." + fname)
			g.Out()
			g.P("}")
		} else {
			uname := oneofFieldName[*field.OneofIndex]
			tname := oneofTypeName[field]
			g.P("if x, ok := m.Get", uname, "().(*", tname, "); ok {")
			g.P("return x.", fname)
			g.P("}")
		}
		if hasDef {
			if *field.Type != descriptor.FieldDescriptorProto_TYPE_BYTES {
				g.P("return " + def)
			} else {
				// The default is a []byte var.
				// Make a copy when returning it to be safe.
				g.P("return append([]byte(nil), ", def, "...)")
			}
		} else {
			switch *field.Type {
			case descriptor.FieldDescriptorProto_TYPE_BOOL:
				g.P("return false")
			case descriptor.FieldDescriptorProto_TYPE_STRING:
				g.P(`return ""`)
			case descriptor.FieldDescriptorProto_TYPE_GROUP,
				descriptor.FieldDescriptorProto_TYPE_MESSAGE,
				descriptor.FieldDescriptorProto_TYPE_BYTES:
				// This is only possible for oneof fields.
				g.P("return nil")
			case descriptor.FieldDescriptorProto_TYPE_ENUM:
				// The default default for an enum is the first value in the enum,
				// not zero.
				obj := g.ObjectNamed(field.GetTypeName())
				var enum *enumDescriptor
				if id, ok := obj.(*importDescriptor); ok {
					// The enum type has been publicly imported.
					enum, _ = id.o.(*enumDescriptor)
				} else {
					enum, _ = obj.(*enumDescriptor)
				}
				if enum == nil {
					log.Printf("don't know how to generate getter for %s", field.GetName())
					continue
				}
				if len(enum.Value) == 0 {
					g.P("return 0 // empty enum")
				} else {
					first := enum.Value[0].GetName()
					g.P("return ", g.DefaultPackageName(obj)+enum.prefix()+first)
				}
			default:
				g.P("return 0")
			}
		}
		g.Out()
		g.P("}")
		g.P()
	}

	if !message.group {
		ms := &messageSymbol{
			sym:           ccTypeName,
			hasExtensions: hasExtensions,
			isMessageSet:  isMessageSet,
			hasOneof:      len(message.OneofDecl) > 0,
			getters:       getters,
		}
		g.file.addExport(message, ms)
	}

	// Oneof functions
	if len(message.OneofDecl) > 0 {
		fieldWire := make(map[*descriptor.FieldDescriptorProto]string)

		// method
		enc := "_" + ccTypeName + "_OneofMarshaler"
		dec := "_" + ccTypeName + "_OneofUnmarshaler"
		size := "_" + ccTypeName + "_OneofSizer"
		encSig := "(msg " + g.Pkg["proto"] + ".Message, b *" + g.Pkg["proto"] + ".Buffer) error"
		decSig := "(msg " + g.Pkg["proto"] + ".Message, tag, wire int, b *" + g.Pkg["proto"] + ".Buffer) (bool, error)"
		sizeSig := "(msg " + g.Pkg["proto"] + ".Message) (n int)"

		g.P("// XXX_OneofFuncs is for the internal use of the proto package.")
		g.P("func (*", ccTypeName, ") XXX_OneofFuncs() (func", encSig, ", func", decSig, ", func", sizeSig, ", []interface{}) {")
		g.P("return ", enc, ", ", dec, ", ", size, ", []interface{}{")
		for _, field := range message.Field {
			if field.OneofIndex == nil {
				continue
			}
			g.P("(*", oneofTypeName[field], ")(nil),")
		}
		g.P("}")
		g.P("}")
		g.P()

		// marshaler
		g.P("func ", enc, encSig, " {")
		g.P("m := msg.(*", ccTypeName, ")")
		for oi, odp := range message.OneofDecl {
			g.P("// ", odp.GetName())
			fname := oneofFieldName[int32(oi)]
			g.P("switch x := m.", fname, ".(type) {")
			for _, field := range message.Field {
				if field.OneofIndex == nil || int(*field.OneofIndex) != oi {
					continue
				}
				g.P("case *", oneofTypeName[field], ":")
				var wire, pre, post string
				val := "x." + fieldNames[field] // overridden for TYPE_BOOL
				canFail := false                // only TYPE_MESSAGE and TYPE_GROUP can fail
				switch *field.Type {
				case descriptor.FieldDescriptorProto_TYPE_DOUBLE:
					wire = "WireFixed64"
					pre = "b.EncodeFixed64(" + g.Pkg["math"] + ".Float64bits("
					post = "))"
				case descriptor.FieldDescriptorProto_TYPE_FLOAT:
					wire = "WireFixed32"
					pre = "b.EncodeFixed32(uint64(" + g.Pkg["math"] + ".Float32bits("
					post = ")))"
				case descriptor.FieldDescriptorProto_TYPE_INT64,
					descriptor.FieldDescriptorProto_TYPE_UINT64:
					wire = "WireVarint"
					pre, post = "b.EncodeVarint(uint64(", "))"
				case descriptor.FieldDescriptorProto_TYPE_INT32,
					descriptor.FieldDescriptorProto_TYPE_UINT32,
					descriptor.FieldDescriptorProto_TYPE_ENUM:
					wire = "WireVarint"
					pre, post = "b.EncodeVarint(uint64(", "))"
				case descriptor.FieldDescriptorProto_TYPE_FIXED64,
					descriptor.FieldDescriptorProto_TYPE_SFIXED64:
					wire = "WireFixed64"
					pre, post = "b.EncodeFixed64(uint64(", "))"
				case descriptor.FieldDescriptorProto_TYPE_FIXED32,
					descriptor.FieldDescriptorProto_TYPE_SFIXED32:
					wire = "WireFixed32"
					pre, post = "b.EncodeFixed32(uint64(", "))"
				case descriptor.FieldDescriptorProto_TYPE_BOOL:
					// bool needs special handling.
					g.P("t := uint64(0)")
					g.P("if ", val, " { t = 1 }")
					val = "t"
					wire = "WireVarint"
					pre, post = "b.EncodeVarint(", ")"
				case descriptor.FieldDescriptorProto_TYPE_STRING:
					wire = "WireBytes"
					pre, post = "b.EncodeStringBytes(", ")"
				case descriptor.FieldDescriptorProto_TYPE_GROUP:
					wire = "WireStartGroup"
					pre, post = "b.Marshal(", ")"
					canFail = true
				case descriptor.FieldDescriptorProto_TYPE_MESSAGE:
					wire = "WireBytes"
					pre, post = "b.EncodeMessage(", ")"
					canFail = true
				case descriptor.FieldDescriptorProto_TYPE_BYTES:
					wire = "WireBytes"
					pre, post = "b.EncodeRawBytes(", ")"
				case descriptor.FieldDescriptorProto_TYPE_SINT32:
					wire = "WireVarint"
					pre, post = "b.EncodeZigzag32(uint64(", "))"
				case descriptor.FieldDescriptorProto_TYPE_SINT64:
					wire = "WireVarint"
					pre, post = "b.EncodeZigzag64(uint64(", "))"
				default:
					g.Fail("unhandled oneof field type ", field.Type.String())
				}
				fieldWire[field] = wire
				g.P("b.EncodeVarint(", field.Number, "<<3|", g.Pkg["proto"], ".", wire, ")")
				if !canFail {
					g.P(pre, val, post)
				} else {
					g.P("if err := ", pre, val, post, "; err != nil {")
					g.P("return err")
					g.P("}")
				}
				if *field.Type == descriptor.FieldDescriptorProto_TYPE_GROUP {
					g.P("b.EncodeVarint(", field.Number, "<<3|", g.Pkg["proto"], ".WireEndGroup)")
				}
			}
			g.P("case nil:")
			g.P("default: return ", g.Pkg["fmt"], `.Errorf("`, ccTypeName, ".", fname, ` has unexpected type %T", x)`)
			g.P("}")
		}
		g.P("return nil")
		g.P("}")
		g.P()

		// unmarshaler
		g.P("func ", dec, decSig, " {")
		g.P("m := msg.(*", ccTypeName, ")")
		g.P("switch tag {")
		for _, field := range message.Field {
			if field.OneofIndex == nil {
				continue
			}
			odp := message.OneofDecl[int(*field.OneofIndex)]
			g.P("case ", field.Number, ": // ", odp.GetName(), ".", *field.Name)
			g.P("if wire != ", g.Pkg["proto"], ".", fieldWire[field], " {")
			g.P("return true, ", g.Pkg["proto"], ".ErrInternalBadWireType")
			g.P("}")
			lhs := "x, err" // overridden for TYPE_MESSAGE and TYPE_GROUP
			var dec, cast, cast2 string
			switch *field.Type {
			case descriptor.FieldDescriptorProto_TYPE_DOUBLE:
				dec, cast = "b.DecodeFixed64()", g.Pkg["math"]+".Float64frombits"
			case descriptor.FieldDescriptorProto_TYPE_FLOAT:
				dec, cast, cast2 = "b.DecodeFixed32()", "uint32", g.Pkg["math"]+".Float32frombits"
			case descriptor.FieldDescriptorProto_TYPE_INT64:
				dec, cast = "b.DecodeVarint()", "int64"
			case descriptor.FieldDescriptorProto_TYPE_UINT64:
				dec = "b.DecodeVarint()"
			case descriptor.FieldDescriptorProto_TYPE_INT32:
				dec, cast = "b.DecodeVarint()", "int32"
			case descriptor.FieldDescriptorProto_TYPE_FIXED64:
				dec = "b.DecodeFixed64()"
			case descriptor.FieldDescriptorProto_TYPE_FIXED32:
				dec, cast = "b.DecodeFixed32()", "uint32"
			case descriptor.FieldDescriptorProto_TYPE_BOOL:
				dec = "b.DecodeVarint()"
				// handled specially below
			case descriptor.FieldDescriptorProto_TYPE_STRING:
				dec = "b.DecodeStringBytes()"
			case descriptor.FieldDescriptorProto_TYPE_GROUP:
				g.P("msg := new(", fieldTypes[field][1:], ")") // drop star
				lhs = "err"
				dec = "b.DecodeGroup(msg)"
				// handled specially below
			case descriptor.FieldDescriptorProto_TYPE_MESSAGE:
				g.P("msg := new(", fieldTypes[field][1:], ")") // drop star
				lhs = "err"
				dec = "b.DecodeMessage(msg)"
				// handled specially below
			case descriptor.FieldDescriptorProto_TYPE_BYTES:
				dec = "b.DecodeRawBytes(true)"
			case descriptor.FieldDescriptorProto_TYPE_UINT32:
				dec, cast = "b.DecodeVarint()", "uint32"
			case descriptor.FieldDescriptorProto_TYPE_ENUM:
				dec, cast = "b.DecodeVarint()", fieldTypes[field]
			case descriptor.FieldDescriptorProto_TYPE_SFIXED32:
				dec, cast = "b.DecodeFixed32()", "int32"
			case descriptor.FieldDescriptorProto_TYPE_SFIXED64:
				dec, cast = "b.DecodeFixed64()", "int64"
			case descriptor.FieldDescriptorProto_TYPE_SINT32:
				dec, cast = "b.DecodeZigzag32()", "int32"
			case descriptor.FieldDescriptorProto_TYPE_SINT64:
				dec, cast = "b.DecodeZigzag64()", "int64"
			default:
				g.Fail("unhandled oneof field type ", field.Type.String())
			}
			g.P(lhs, " := ", dec)
			val := "x"
			if cast != "" {
				val = cast + "(" + val + ")"
			}
			if cast2 != "" {
				val = cast2 + "(" + val + ")"
			}
			switch *field.Type {
			case descriptor.FieldDescriptorProto_TYPE_BOOL:
				val += " != 0"
			case descriptor.FieldDescriptorProto_TYPE_GROUP,
				descriptor.FieldDescriptorProto_TYPE_MESSAGE:
				val = "msg"
			}
			g.P("m.", oneofFieldName[*field.OneofIndex], " = &", oneofTypeName[field], "{", val, "}")
			g.P("return true, err")
		}
		g.P("default: return false, nil")
		g.P("}")
		g.P("}")
		g.P()

		// sizer
		g.P("func ", size, sizeSig, " {")
		g.P("m := msg.(*", ccTypeName, ")")
		for oi, odp := range message.OneofDecl {
			g.P("// ", odp.GetName())
			fname := oneofFieldName[int32(oi)]
			g.P("switch x := m.", fname, ".(type) {")
			for _, field := range message.Field {
				if field.OneofIndex == nil || int(*field.OneofIndex) != oi {
					continue
				}
				g.P("case *", oneofTypeName[field], ":")
				val := "x." + fieldNames[field]
				var wire, varint, fixed string
				switch *field.Type {
				case descriptor.FieldDescriptorProto_TYPE_DOUBLE:
					wire = "WireFixed64"
					fixed = "8"
				case descriptor.FieldDescriptorProto_TYPE_FLOAT:
					wire = "WireFixed32"
					fixed = "4"
				case descriptor.FieldDescriptorProto_TYPE_INT64,
					descriptor.FieldDescriptorProto_TYPE_UINT64,
					descriptor.FieldDescriptorProto_TYPE_INT32,
					descriptor.FieldDescriptorProto_TYPE_UINT32,
					descriptor.FieldDescriptorProto_TYPE_ENUM:
					wire = "WireVarint"
					varint = val
				case descriptor.FieldDescriptorProto_TYPE_FIXED64,
					descriptor.FieldDescriptorProto_TYPE_SFIXED64:
					wire = "WireFixed64"
					fixed = "8"
				case descriptor.FieldDescriptorProto_TYPE_FIXED32,
					descriptor.FieldDescriptorProto_TYPE_SFIXED32:
					wire = "WireFixed32"
					fixed = "4"
				case descriptor.FieldDescriptorProto_TYPE_BOOL:
					wire = "WireVarint"
					fixed = "1"
				case descriptor.FieldDescriptorProto_TYPE_STRING:
					wire = "WireBytes"
					fixed = "len(" + val + ")"
					varint = fixed
				case descriptor.FieldDescriptorProto_TYPE_GROUP:
					wire = "WireStartGroup"
					fixed = g.Pkg["proto"] + ".Size(" + val + ")"
				case descriptor.FieldDescriptorProto_TYPE_MESSAGE:
					wire = "WireBytes"
					g.P("s := ", g.Pkg["proto"], ".Size(", val, ")")
					fixed = "s"
					varint = fixed
				case descriptor.FieldDescriptorProto_TYPE_BYTES:
					wire = "WireBytes"
					fixed = "len(" + val + ")"
					varint = fixed
				case descriptor.FieldDescriptorProto_TYPE_SINT32:
					wire = "WireVarint"
					varint = "(uint32(" + val + ") << 1) ^ uint32((int32(" + val + ") >> 31))"
				case descriptor.FieldDescriptorProto_TYPE_SINT64:
					wire = "WireVarint"
					varint = "uint64(" + val + " << 1) ^ uint64((int64(" + val + ") >> 63))"
				default:
					g.Fail("unhandled oneof field type ", field.Type.String())
				}
				g.P("n += ", g.Pkg["proto"], ".SizeVarint(", field.Number, "<<3|", g.Pkg["proto"], ".", wire, ")")
				if varint != "" {
					g.P("n += ", g.Pkg["proto"], ".SizeVarint(uint64(", varint, "))")
				}
				if fixed != "" {
					g.P("n += ", fixed)
				}
				if *field.Type == descriptor.FieldDescriptorProto_TYPE_GROUP {
					g.P("n += ", g.Pkg["proto"], ".SizeVarint(", field.Number, "<<3|", g.Pkg["proto"], ".WireEndGroup)")
				}
			}
			g.P("case nil:")
			g.P("default:")
			g.P("panic(", g.Pkg["fmt"], ".Sprintf(\"proto: unexpected type %T in oneof\", x))")
			g.P("}")
		}
		g.P("return n")
		g.P("}")
		g.P()
	}

	for _, ext := range message.extensions {
		g.generateExtension(ext)
	}

	fullName := strings.Join(message.TypeName(), ".")
	if g.file.Package != nil {
		fullName = *g.file.Package + "." + fullName
	}

	g.addInitf("%s.RegisterType((*%s)(nil), %q)", g.Pkg["proto"], ccTypeName, fullName)
}

// Scan the messages in this file.  For each one, build the slice of nested descriptors
func (g *Generator) buildNestedMessages(descs []*messageDescriptor) {
	for _, desc := range descs {
		if len(desc.NestedType) != 0 {
			for _, nest := range descs {
				if nest.parent == desc {
					desc.nested = append(desc.nested, nest)
				}
			}
			if len(desc.nested) != len(desc.NestedType) {
				g.Fail("internal error: nesting failure for", desc.GetName())
			}
		}
	}
}

type messageSymbol struct {
	sym           string
	hasExtensions bool
	isMessageSet  bool
	hasOneof      bool
	getters       []getterSymbol
}

func (ms *messageSymbol) GenerateAlias(g *Generator, pkg string) {
	remoteSym := pkg + "." + ms.sym

	g.P("type ", ms.sym, " ", remoteSym)
	g.P("func (m *", ms.sym, ") Reset() { (*", remoteSym, ")(m).Reset() }")
	g.P("func (m *", ms.sym, ") String() string { return (*", remoteSym, ")(m).String() }")
	g.P("func (*", ms.sym, ") ProtoMessage() {}")
	if ms.hasExtensions {
		g.P("func (*", ms.sym, ") ExtensionRangeArray() []", g.Pkg["proto"], ".ExtensionRange ",
			"{ return (*", remoteSym, ")(nil).ExtensionRangeArray() }")
		if ms.isMessageSet {
			g.P("func (m *", ms.sym, ") Marshal() ([]byte, error) ",
				"{ return (*", remoteSym, ")(m).Marshal() }")
			g.P("func (m *", ms.sym, ") Unmarshal(buf []byte) error ",
				"{ return (*", remoteSym, ")(m).Unmarshal(buf) }")
		}
	}
	if ms.hasOneof {
		// Oneofs and public imports do not mix well.
		// We can make them work okay for the binary format,
		// but they're going to break weirdly for text/JSON.
		enc := "_" + ms.sym + "_OneofMarshaler"
		dec := "_" + ms.sym + "_OneofUnmarshaler"
		size := "_" + ms.sym + "_OneofSizer"
		encSig := "(msg " + g.Pkg["proto"] + ".Message, b *" + g.Pkg["proto"] + ".Buffer) error"
		decSig := "(msg " + g.Pkg["proto"] + ".Message, tag, wire int, b *" + g.Pkg["proto"] + ".Buffer) (bool, error)"
		sizeSig := "(msg " + g.Pkg["proto"] + ".Message) int"
		g.P("func (m *", ms.sym, ") XXX_OneofFuncs() (func", encSig, ", func", decSig, ", func", sizeSig, ", []interface{}) {")
		g.P("return ", enc, ", ", dec, ", ", size, ", nil")
		g.P("}")

		g.P("func ", enc, encSig, " {")
		g.P("m := msg.(*", ms.sym, ")")
		g.P("m0 := (*", remoteSym, ")(m)")
		g.P("enc, _, _, _ := m0.XXX_OneofFuncs()")
		g.P("return enc(m0, b)")
		g.P("}")

		g.P("func ", dec, decSig, " {")
		g.P("m := msg.(*", ms.sym, ")")
		g.P("m0 := (*", remoteSym, ")(m)")
		g.P("_, dec, _, _ := m0.XXX_OneofFuncs()")
		g.P("return dec(m0, tag, wire, b)")
		g.P("}")

		g.P("func ", size, sizeSig, " {")
		g.P("m := msg.(*", ms.sym, ")")
		g.P("m0 := (*", remoteSym, ")(m)")
		g.P("_, _, size, _ := m0.XXX_OneofFuncs()")
		g.P("return size(m0)")
		g.P("}")
	}
	for _, get := range ms.getters {

		if get.typeName != "" {
			g.RecordTypeUse(get.typeName)
		}
		typ := get.typ
		val := "(*" + remoteSym + ")(m)." + get.name + "()"
		if get.genType {
			// typ will be "*pkg.T" (message/group) or "pkg.T" (enum)
			// or "map[t]*pkg.T" (map to message/enum).
			// The first two of those might have a "[]" prefix if it is repeated.
			// Drop any package qualifier since we have hoisted the type into this package.
			rep := strings.HasPrefix(typ, "[]")
			if rep {
				typ = typ[2:]
			}
			isMap := strings.HasPrefix(typ, "map[")
			star := typ[0] == '*'
			if !isMap { // map types handled lower down
				typ = typ[strings.Index(typ, ".")+1:]
			}
			if star {
				typ = "*" + typ
			}
			if rep {
				// Go does not permit conversion between slice types where both
				// element types are named. That means we need to generate a bit
				// of code in this situation.
				// typ is the element type.
				// val is the expression to get the slice from the imported type.

				ctyp := typ // conversion type expression; "Foo" or "(*Foo)"
				if star {
					ctyp = "(" + typ + ")"
				}

				g.P("func (m *", ms.sym, ") ", get.name, "() []", typ, " {")
				g.In()
				g.P("o := ", val)
				g.P("if o == nil {")
				g.In()
				g.P("return nil")
				g.Out()
				g.P("}")
				g.P("s := make([]", typ, ", len(o))")
				g.P("for i, x := range o {")
				g.In()
				g.P("s[i] = ", ctyp, "(x)")
				g.Out()
				g.P("}")
				g.P("return s")
				g.Out()
				g.P("}")
				continue
			}
			if isMap {
				// Split map[keyTyp]valTyp.
				bra, ket := strings.Index(typ, "["), strings.Index(typ, "]")
				keyTyp, valTyp := typ[bra+1:ket], typ[ket+1:]
				// Drop any package qualifier.
				// Only the value type may be foreign.
				star := valTyp[0] == '*'
				valTyp = valTyp[strings.Index(valTyp, ".")+1:]
				if star {
					valTyp = "*" + valTyp
				}

				typ := "map[" + keyTyp + "]" + valTyp
				g.P("func (m *", ms.sym, ") ", get.name, "() ", typ, " {")
				g.P("o := ", val)
				g.P("if o == nil { return nil }")
				g.P("s := make(", typ, ", len(o))")
				g.P("for k, v := range o {")
				g.P("s[k] = (", valTyp, ")(v)")
				g.P("}")
				g.P("return s")
				g.P("}")
				continue
			}
			// Convert imported type into the forwarding type.
			val = "(" + typ + ")(" + val + ")"
		}

		g.P("func (m *", ms.sym, ") ", get.name, "() ", typ, " { return ", val, " }")
	}

}

// Descriptor represents a protocol buffer message.
type messageDescriptor struct {
	common
	*descriptor.DescriptorProto
	parent     *messageDescriptor     // The containing message, if any.
	nested     []*messageDescriptor   // Inner messages, if any.
	enums      []*enumDescriptor      // Inner enums, if any.
	extensions []*extensionDescriptor // Extensions, if any.
	typename   []string               // Cached typename vector.
	index      int                    // The index into the container, whether the file or another message.
	path       string                 // The SourceCodeInfo path as comma-separated integers.
	group      bool
}

func newMessage(desc *descriptor.DescriptorProto, parent *messageDescriptor, file *descriptor.FileDescriptorProto, index int) *messageDescriptor {
	d := &messageDescriptor{
		common:          common{file},
		DescriptorProto: desc,
		parent:          parent,
		index:           index,
	}
	if parent == nil {
		d.path = fmt.Sprintf("%d,%d", messagePath, index)
	} else {
		d.path = fmt.Sprintf("%s,%d,%d", parent.path, messageMessagePath, index)
	}

	// The only way to distinguish a group from a message is whether
	// the containing message has a TYPE_GROUP field that matches.
	if parent != nil {
		parts := d.TypeName()
		if file.Package != nil {
			parts = append([]string{*file.Package}, parts...)
		}
		exp := "." + strings.Join(parts, ".")
		for _, field := range parent.Field {
			if field.GetType() == descriptor.FieldDescriptorProto_TYPE_GROUP && field.GetTypeName() == exp {
				d.group = true
				break
			}
		}
	}

	for _, field := range desc.Extension {
		d.extensions = append(d.extensions, &extensionDescriptor{common{file}, field, d})
	}

	return d
}

// Return a slice of all the Messages defined within this file
func wrapMessages(file *descriptor.FileDescriptorProto) []*messageDescriptor {
	sl := make([]*messageDescriptor, 0, len(file.MessageType)+10)
	for i, desc := range file.MessageType {
		sl = wrapThisDescriptor(sl, desc, nil, file, i)
	}
	return sl
}

// Wrap this Descriptor, recursively
func wrapThisDescriptor(sl []*messageDescriptor, desc *descriptor.DescriptorProto, parent *messageDescriptor, file *descriptor.FileDescriptorProto, index int) []*messageDescriptor {
	sl = append(sl, newMessage(desc, parent, file, index))
	me := sl[len(sl)-1]
	for i, nested := range desc.NestedType {
		sl = wrapThisDescriptor(sl, nested, me, file, i)
	}
	return sl
}

// TypeName returns the elements of the dotted type name. The package name is
// not part of this name.
func (d *messageDescriptor) TypeName() []string {
	if d.typename != nil {
		return d.typename
	}
	n := 0
	for parent := d; parent != nil; parent = parent.parent {
		n++
	}
	s := make([]string, n, n)
	for parent := d; parent != nil; parent = parent.parent {
		n--
		s[n] = parent.GetName()
	}
	d.typename = s
	return s
}

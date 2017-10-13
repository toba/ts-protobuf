package main

import "github.com/golang/protobuf/protoc-gen-go/descriptor"

// Names of messages in the `google.protobuf` package for which
// we will generate XXX_WellKnownType methods.
var wellKnownTypes = map[string]bool{
	"Any":       true,
	"Duration":  true,
	"Empty":     true,
	"Struct":    true,
	"Timestamp": true,

	"Value":       true,
	"ListValue":   true,
	"DoubleValue": true,
	"FloatValue":  true,
	"Int64Value":  true,
	"UInt64Value": true,
	"Int32Value":  true,
	"UInt32Value": true,
	"BoolValue":   true,
	"StringValue": true,
	"BytesValue":  true,
}

// BuildTypeNameMap builds the map from fully qualified type names to objects.
// The key names for the map come from the input data, which puts a period at the beginning.
// It should be called after SetPackageNames and before GenerateAllFiles.
func (g *Generator) BuildTypeNameMap() {
	g.typeNameToObject = make(map[string]Object)
	for _, f := range g.allFiles {
		// The names in this loop are defined by the proto world, not us, so the
		// package name may be empty.  If so, the dotted package name of X will
		// be ".X"; otherwise it will be ".pkg.X".
		dottedPkg := "." + f.GetPackage()
		if dottedPkg != "." {
			dottedPkg += "."
		}
		for _, enum := range f.enum {
			name := dottedPkg + dottedSlice(enum.TypeName())
			g.typeNameToObject[name] = enum
		}
		for _, desc := range f.desc {
			name := dottedPkg + dottedSlice(desc.TypeName())
			g.typeNameToObject[name] = desc
		}
	}
}

// TypeName is the printed name appropriate for an item. If the object is in the current file,
// TypeName drops the package name and underscores the rest.
// Otherwise the object is from another package; and the result is the underscored
// package name followed by the item name.
// The result always has an initial capital.
func (g *Generator) TypeName(obj ProtoObject) string {
	return g.DefaultPackageName(obj) + CamelCaseSlice(obj.TypeName())
}

// TypeNameWithPackage is like TypeName, but always includes the package
// name even if the object is in our own package.
func (g *Generator) TypeNameWithPackage(obj ProtoObject) string {
	return obj.PackageName() + CamelCaseSlice(obj.TypeName())
}

// GoType returns a string representing the type name, and the wire type
func (g *Generator) GoType(message *messageDescriptor, field *descriptor.FieldDescriptorProto) (typ string, wire string) {
	// TODO: Options.
	switch *field.Type {
	case descriptor.FieldDescriptorProto_TYPE_DOUBLE:
		typ, wire = "float64", "fixed64"
	case descriptor.FieldDescriptorProto_TYPE_FLOAT:
		typ, wire = "float32", "fixed32"
	case descriptor.FieldDescriptorProto_TYPE_INT64:
		typ, wire = "int64", "varint"
	case descriptor.FieldDescriptorProto_TYPE_UINT64:
		typ, wire = "uint64", "varint"
	case descriptor.FieldDescriptorProto_TYPE_INT32:
		typ, wire = "int32", "varint"
	case descriptor.FieldDescriptorProto_TYPE_UINT32:
		typ, wire = "uint32", "varint"
	case descriptor.FieldDescriptorProto_TYPE_FIXED64:
		typ, wire = "uint64", "fixed64"
	case descriptor.FieldDescriptorProto_TYPE_FIXED32:
		typ, wire = "uint32", "fixed32"
	case descriptor.FieldDescriptorProto_TYPE_BOOL:
		typ, wire = "bool", "varint"
	case descriptor.FieldDescriptorProto_TYPE_STRING:
		typ, wire = "string", "bytes"
	case descriptor.FieldDescriptorProto_TYPE_GROUP:
		desc := g.ObjectNamed(field.GetTypeName())
		typ, wire = "*"+g.TypeName(desc), "group"
	case descriptor.FieldDescriptorProto_TYPE_MESSAGE:
		desc := g.ObjectNamed(field.GetTypeName())
		typ, wire = "*"+g.TypeName(desc), "bytes"
	case descriptor.FieldDescriptorProto_TYPE_BYTES:
		typ, wire = "[]byte", "bytes"
	case descriptor.FieldDescriptorProto_TYPE_ENUM:
		desc := g.ObjectNamed(field.GetTypeName())
		typ, wire = g.TypeName(desc), "varint"
	case descriptor.FieldDescriptorProto_TYPE_SFIXED32:
		typ, wire = "int32", "fixed32"
	case descriptor.FieldDescriptorProto_TYPE_SFIXED64:
		typ, wire = "int64", "fixed64"
	case descriptor.FieldDescriptorProto_TYPE_SINT32:
		typ, wire = "int32", "zigzag32"
	case descriptor.FieldDescriptorProto_TYPE_SINT64:
		typ, wire = "int64", "zigzag64"
	default:
		g.Fail("unknown type for", field.GetName())
	}
	if isRepeated(field) {
		typ = "[]" + typ
	} else if message != nil && message.proto3() {
		return
	} else if field.OneofIndex != nil && message != nil {
		return
	} else if needsStar(*field.Type) {
		typ = "*" + typ
	}
	return
}

func (g *Generator) RecordTypeUse(t string) {
	if obj, ok := g.typeNameToObject[t]; ok {
		// Call ObjectNamed to get the true object to record the use.
		obj = g.ObjectNamed(t)
		g.usedPackages[obj.PackageName()] = true
	}
}

// WrapTypes walks the incoming data, wrapping DescriptorProtos, EnumDescriptorProtos
// and FileDescriptorProtos into file-referenced objects within the Generator.
// It also creates the list of files to generate and so should be called before GenerateAllFiles.
func (g *Generator) WrapTypes() {
	g.allFiles = make([]*fileDescriptor, 0, len(g.Request.ProtoFile))
	g.allFilesByName = make(map[string]*fileDescriptor, len(g.allFiles))
	for _, f := range g.Request.ProtoFile {
		// We must wrap the descriptors before we wrap the enums
		descs := wrapMessages(f)
		g.buildNestedDescriptors(descs)
		enums := wrapEnumDescriptors(f, descs)
		g.buildNestedEnums(descs, enums)
		exts := wrapExtensions(f)
		fd := &fileDescriptor{
			FileDescriptorProto: f,
			desc:                descs,
			enum:                enums,
			ext:                 exts,
			exported:            make(map[Object][]symbol),
			proto3:              fileIsProto3(f),
		}
		extractComments(fd)
		g.allFiles = append(g.allFiles, fd)
		g.allFilesByName[f.GetName()] = fd
	}
	for _, fd := range g.allFiles {
		fd.imp = wrapImported(fd.FileDescriptorProto, g)
	}

	g.genFiles = make([]*fileDescriptor, 0, len(g.Request.FileToGenerate))
	for _, fileName := range g.Request.FileToGenerate {
		fd := g.allFilesByName[fileName]
		if fd == nil {
			g.Fail("could not find file named", fileName)
		}
		fd.index = len(g.genFiles)
		g.genFiles = append(g.genFiles, fd)
	}
}

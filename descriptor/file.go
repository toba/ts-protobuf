package descriptor

import (
	"fmt"
	"log"
	"path"
	"strings"

	proto "github.com/golang/protobuf/protoc-gen-go/descriptor"
)

// FileDescriptor describes a protocol buffer descriptor file (.proto). It
// includes slices of all the messages and enums defined within it. Those slices
// are constructed by WrapTypes.
type FileDescriptor struct {
	*proto.FileDescriptorProto
	messages   []*MessageDescriptor   // All the messages defined in this file.
	enums      []*EnumDescriptor      // All the enums defined in this file.
	extensions []*ExtensionDescriptor // All the top-level extensions defined in this file.
	imports    []*ImportedDescriptor  // All types defined in files publicly imported by this file.

	// Comments stored as a map of path (comma-separated integers) to the comment.
	comments map[string]*proto.SourceCodeInfo_Location

	// The full list of symbols that are exported, as a map from the exported
	// object to its symbols. This is used for supporting public imports.
	//exports map[ProtoObject][]symbol.Symbol

	index  int  // The index of this file in the list of files to generate code for
	proto3 bool // whether to generate proto3 code for this file
}

// PackageName is the package name we'll use in the generated code to refer to
// this file.
func (d *FileDescriptor) PackageName() string {
	return uniquePackageOf(d.FileDescriptorProto)
}

// VarName is the variable name we'll use in the generated code to refer
// to the compressed bytes of this descriptor. It is not exported, so
// it is only valid inside the generated package.
func (d *FileDescriptor) VarName() string {
	return fmt.Sprintf("fileDescriptor%d", d.index)
}

// goPackageOption interprets the file's go_package option.
// If there is no go_package, it returns ("", "", false).
// If there's a simple name, it returns ("", pkg, true).
// If the option implies an import path, it returns (impPath, pkg, true).
func (d *FileDescriptor) goPackageOption() (impPath, pkg string, ok bool) {
	pkg = d.GetOptions().GetGoPackage()
	if pkg == "" {
		return
	}
	ok = true
	// The presence of a slash implies there's an import path.
	slash := strings.LastIndex(pkg, "/")
	if slash < 0 {
		return
	}
	impPath, pkg = pkg, pkg[slash+1:]
	// A semicolon-delimited suffix overrides the package name.
	sc := strings.IndexByte(impPath, ';')
	if sc < 0 {
		return
	}
	impPath, pkg = impPath[:sc], impPath[sc+1:]
	return
}

// goPackageName returns the Go package name to use in the
// generated Go file.  The result explicit reports whether the name
// came from an option go_package statement.  If explicit is false,
// the name was derived from the protocol buffer's package statement
// or the input file name.
func (d *FileDescriptor) goPackageName() (name string, explicit bool) {
	// Does the file have a "go_package" option?
	if _, pkg, ok := d.goPackageOption(); ok {
		return pkg, true
	}

	// Does the file have a package clause?
	if pkg := d.GetPackage(); pkg != "" {
		return pkg, false
	}
	// Use the file base name.
	return baseName(d.GetName()), false
}

// outputFileName returns the output name for the generated TypeScript file.
func (d *FileDescriptor) outputFileName() string {
	name := *d.Name
	if ext := path.Ext(name); ext == ".proto" || ext == ".protodevel" {
		name = name[:len(name)-len(ext)]
	}
	return name + ".pb.ts"
}

// TODO: fix circular dep
// func (d *FileDescriptor) addExport(obj ProtoObject, sym symbol) {
// 	d.exports[obj] = append(d.exports[obj], sym)
// }

func fileIsProto3(file *proto.FileDescriptorProto) bool {
	return file.GetSyntax() == "proto3"
}

// Each package name we generate must be unique. The package we're generating
// gets its own name but every other package must have a unique name that does
// not conflict in the code we generate.  These names are chosen globally (although
// they don't have to be, it simplifies things to do them globally).
func uniquePackageOf(fd *proto.FileDescriptorProto) string {
	s, ok := uniquePackageName[fd]
	if !ok {
		log.Fatal("internal error: no package name defined for " + fd.GetName())
	}
	return s
}

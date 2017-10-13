package descriptor

import (
	"fmt"
	"path"
	"strings"

	proto "github.com/golang/protobuf/protoc-gen-go/descriptor"
	"github.com/toba/ts-protobuf/symbol"
)

// FileDescriptor describes a protocol buffer descriptor file (.proto). It
// includes slices of all the messages and enums defined within it. Those slices
// are constructed by WrapTypes.
type FileDescriptor struct {
	*proto.FileDescriptorProto
	desc []*Descriptor          // All the messages defined in this file.
	enum []*EnumDescriptor      // All the enums defined in this file.
	ext  []*ExtensionDescriptor // All the top-level extensions defined in this file.
	imp  []*ImportedDescriptor  // All types defined in files publicly imported by this file.

	// Comments, stored as a map of path (comma-separated integers) to the comment.
	comments map[string]*proto.SourceCodeInfo_Location

	// The full list of symbols that are exported, as a map from the exported
	// object to its symbols. This is used for supporting public imports.
	exported map[Object][]symbol.Symbol

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

// goFileName returns the output name for the generated Go file.
func (d *FileDescriptor) goFileName() string {
	name := *d.Name
	if ext := path.Ext(name); ext == ".proto" || ext == ".protodevel" {
		name = name[:len(name)-len(ext)]
	}
	name += ".pb.go"

	// Does the file have a "go_package" option?
	// If it does, it may override the filename.
	if impPath, _, ok := d.goPackageOption(); ok && impPath != "" {
		// Replace the existing dirname with the declared import path.
		_, name = path.Split(name)
		name = path.Join(impPath, name)
		return name
	}

	return name
}

func (d *FileDescriptor) addExport(obj Object, sym symbol) {
	d.exported[obj] = append(d.exported[obj], sym)
}

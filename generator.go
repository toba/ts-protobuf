// Go support for Protocol Buffers - Google's data interchange format
//
// Copyright 2010 The Go Authors.  All rights reserved.
// https://github.com/golang/protobuf
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are
// met:
//
//     * Redistributions of source code must retain the above copyright
// notice, this list of conditions and the following disclaimer.
//     * Redistributions in binary form must reproduce the above
// copyright notice, this list of conditions and the following disclaimer
// in the documentation and/or other materials provided with the
// distribution.
//     * Neither the name of Google Inc. nor the names of its
// contributors may be used to endorse or promote products derived from
// this software without specific prior written permission.
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
// "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
// LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
// A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
// OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
// SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
// LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
// DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
// THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
// (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
// OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

/*
	The code generator for the plugin for the Google protocol buffer compiler.
	It generates Go code from the protocol buffer description files read by the
	main routine.
*/
package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"go/parser"
	"go/printer"
	"go/token"
	"log"
	"os"
	"path"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/golang/protobuf/proto"

	"github.com/golang/protobuf/protoc-gen-go/descriptor"
	plugin "github.com/golang/protobuf/protoc-gen-go/plugin"
)

// Each type we import as a protocol buffer (other than FileDescriptorProto) needs
// a pointer to the FileDescriptorProto that represents it.  These types achieve that
// wrapping by placing each Proto inside a struct with the pointer to its File. The
// structs have the same names as their contents, with "Proto" removed.
// FileDescriptor is used to store the things that it points to.

// The file and package name method are common to messages and enums.
type common struct {
	file *descriptor.FileDescriptorProto // File this object comes from.
}

// PackageName is name in the package clause in the generated file.
func (c *common) PackageName() string { return uniquePackageOf(c.file) }

func (c *common) File() *descriptor.FileDescriptorProto { return c.file }

func fileIsProto3(file *descriptor.FileDescriptorProto) bool {
	return file.GetSyntax() == "proto3"
}

func (c *common) proto3() bool { return fileIsProto3(c.file) }

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

// Object is an interface abstracting the abilities shared by enums, messages, extensions and imported objects.
type Object interface {
	PackageName() string // The name we use in our output (a_b_c), possibly renamed for uniqueness.
	TypeName() []string
	File() *descriptor.FileDescriptorProto
}

// Each package name we generate must be unique. The package we're generating
// gets its own name but every other package must have a unique name that does
// not conflict in the code we generate.  These names are chosen globally (although
// they don't have to be, it simplifies things to do them globally).
func uniquePackageOf(fd *descriptor.FileDescriptorProto) string {
	s, ok := uniquePackageName[fd]
	if !ok {
		log.Fatal("internal error: no package name defined for " + fd.GetName())
	}
	return s
}

// Generator is the type whose methods generate the output, stored in the
// associated response structure.
type Generator struct {
	*bytes.Buffer

	Request  *plugin.CodeGeneratorRequest  // The input.
	Response *plugin.CodeGeneratorResponse // The output.

	Param             map[string]string // Command-line parameters.
	PackageImportPath string            // Go import path of the package we're generating code for
	ImportPrefix      string            // String to prefix to imported package file names.
	ImportMap         map[string]string // Mapping from .proto file name to import path

	Pkg map[string]string // The names under which we import support packages

	packageName      string                     // What we're calling ourselves.
	allFiles         []*FileDescriptor          // All files in the tree
	allFilesByName   map[string]*FileDescriptor // All files by filename.
	genFiles         []*FileDescriptor          // Those files we will generate output for.
	file             *FileDescriptor            // The file we are compiling now.
	usedPackages     map[string]bool            // Names of packages used in current file.
	typeNameToObject map[string]Object          // Key is a fully-qualified name in input syntax.
	init             []string                   // Lines to emit in the init function.
	indent           string
	writeOutput      bool
}

// NewGenerator creates a new generator and allocates the request and response
// protobufs.
func NewGenerator() *Generator {
	g := new(Generator)
	g.Buffer = new(bytes.Buffer)
	g.Request = new(plugin.CodeGeneratorRequest)
	g.Response = new(plugin.CodeGeneratorResponse)
	return g
}

// Error reports a problem, including an error, and exits the program.
func (g *Generator) Error(err error, msgs ...string) {
	s := strings.Join(msgs, " ") + ":" + err.Error()
	log.Print("protoc-gen-go: error:", s)
	os.Exit(1)
}

// Fail reports a problem and exits the program.
func (g *Generator) Fail(msgs ...string) {
	s := strings.Join(msgs, " ")
	log.Print("protoc-gen-go: error:", s)
	os.Exit(1)
}

// CommandLineParameters breaks the comma-separated list of key=value pairs
// in the parameter (a member of the request protobuf) into a key/value map.
// It then sets file name mappings defined by those entries.
func (g *Generator) CommandLineParameters(parameter string) {
	g.Param = make(map[string]string)
	for _, p := range strings.Split(parameter, ",") {
		if i := strings.Index(p, "="); i < 0 {
			g.Param[p] = ""
		} else {
			g.Param[p[0:i]] = p[i+1:]
		}
	}

	g.ImportMap = make(map[string]string)
	pluginList := "none" // Default list of plugin names to enable (empty means all).
	for k, v := range g.Param {
		switch k {
		case "import_prefix":
			g.ImportPrefix = v
		case "import_path":
			g.PackageImportPath = v
		case "plugins":
			pluginList = v
		default:
			if len(k) > 0 && k[0] == 'M' {
				g.ImportMap[k[1:]] = v
			}
		}
	}
	if pluginList != "" {
		// Amend the set of plugins.
		enabled := make(map[string]bool)
		for _, name := range strings.Split(pluginList, "+") {
			enabled[name] = true
		}
		var nplugins []Plugin
		for _, p := range plugins {
			if enabled[p.Name()] {
				nplugins = append(nplugins, p)
			}
		}
		plugins = nplugins
	}
}

// DefaultPackageName returns the package name printed for the object.
// If its file is in a different package, it returns the package name we're using for this file, plus ".".
// Otherwise it returns the empty string.
func (g *Generator) DefaultPackageName(obj Object) string {
	pkg := obj.PackageName()
	if pkg == g.packageName {
		return ""
	}
	return pkg + "."
}

// For each input file, the unique package name to use, underscored.
var uniquePackageName = make(map[*descriptor.FileDescriptorProto]string)

// Package names already registered.  Key is the name from the .proto file;
// value is the name that appears in the generated code.
var pkgNamesInUse = make(map[string]bool)

// Create and remember a guaranteed unique package name for this file descriptor.
// Pkg is the candidate name.  If f is nil, it's a builtin package like "proto" and
// has no file descriptor.
func RegisterUniquePackageName(pkg string, f *FileDescriptor) string {
	// Convert dots to underscores before finding a unique alias.
	pkg = strings.Map(badToUnderscore, pkg)

	for i, orig := 1, pkg; pkgNamesInUse[pkg]; i++ {
		// It's a duplicate; must rename.
		pkg = orig + strconv.Itoa(i)
	}
	// Install it.
	pkgNamesInUse[pkg] = true
	if f != nil {
		uniquePackageName[f.FileDescriptorProto] = pkg
	}
	return pkg
}

var isGoKeyword = map[string]bool{
	"break":       true,
	"case":        true,
	"chan":        true,
	"const":       true,
	"continue":    true,
	"default":     true,
	"else":        true,
	"defer":       true,
	"fallthrough": true,
	"for":         true,
	"func":        true,
	"go":          true,
	"goto":        true,
	"if":          true,
	"import":      true,
	"interface":   true,
	"map":         true,
	"package":     true,
	"range":       true,
	"return":      true,
	"select":      true,
	"struct":      true,
	"switch":      true,
	"type":        true,
	"var":         true,
}

// defaultGoPackage returns the package name to use,
// derived from the import path of the package we're building code for.
func (g *Generator) defaultGoPackage() string {
	p := g.PackageImportPath
	if i := strings.LastIndex(p, "/"); i >= 0 {
		p = p[i+1:]
	}
	if p == "" {
		return ""
	}

	p = strings.Map(badToUnderscore, p)
	// Identifier must not be keyword: insert _.
	if isGoKeyword[p] {
		p = "_" + p
	}
	// Identifier must not begin with digit: insert _.
	if r, _ := utf8.DecodeRuneInString(p); unicode.IsDigit(r) {
		p = "_" + p
	}
	return p
}

// SetPackageNames sets the package name for this run.
// The package name must agree across all files being generated.
// It also defines unique package names for all imported files.
func (g *Generator) SetPackageNames() {
	// Register the name for this package.  It will be the first name
	// registered so is guaranteed to be unmodified.
	pkg, explicit := g.genFiles[0].goPackageName()

	// Check all files for an explicit go_package option.
	for _, f := range g.genFiles {
		thisPkg, thisExplicit := f.goPackageName()
		if thisExplicit {
			if !explicit {
				// Let this file's go_package option serve for all input files.
				pkg, explicit = thisPkg, true
			} else if thisPkg != pkg {
				g.Fail("inconsistent package names:", thisPkg, pkg)
			}
		}
	}

	// If we don't have an explicit go_package option but we have an
	// import path, use that.
	if !explicit {
		p := g.defaultGoPackage()
		if p != "" {
			pkg, explicit = p, true
		}
	}

	// If there was no go_package and no import path to use,
	// double-check that all the inputs have the same implicit
	// Go package name.
	if !explicit {
		for _, f := range g.genFiles {
			thisPkg, _ := f.goPackageName()
			if thisPkg != pkg {
				g.Fail("inconsistent package names:", thisPkg, pkg)
			}
		}
	}

	g.packageName = RegisterUniquePackageName(pkg, g.genFiles[0])

	// Register the support package names. They might collide with the
	// name of a package we import.
	g.Pkg = map[string]string{
		"fmt":   RegisterUniquePackageName("fmt", nil),
		"math":  RegisterUniquePackageName("math", nil),
		"proto": RegisterUniquePackageName("proto", nil),
	}

AllFiles:
	for _, f := range g.allFiles {
		for _, genf := range g.genFiles {
			if f == genf {
				// In this package already.
				uniquePackageName[f.FileDescriptorProto] = g.packageName
				continue AllFiles
			}
		}
		// The file is a dependency, so we want to ignore its go_package option
		// because that is only relevant for its specific generated output.
		pkg := f.GetPackage()
		if pkg == "" {
			pkg = baseName(*f.Name)
		}
		RegisterUniquePackageName(pkg, f)
	}
}

// WrapTypes walks the incoming data, wrapping DescriptorProtos, EnumDescriptorProtos
// and FileDescriptorProtos into file-referenced objects within the Generator.
// It also creates the list of files to generate and so should be called before GenerateAllFiles.
func (g *Generator) WrapTypes() {
	g.allFiles = make([]*FileDescriptor, 0, len(g.Request.ProtoFile))
	g.allFilesByName = make(map[string]*FileDescriptor, len(g.allFiles))
	for _, f := range g.Request.ProtoFile {
		// We must wrap the descriptors before we wrap the enums
		descs := wrapDescriptors(f)
		g.buildNestedDescriptors(descs)
		enums := wrapEnumDescriptors(f, descs)
		g.buildNestedEnums(descs, enums)
		exts := wrapExtensions(f)
		fd := &FileDescriptor{
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

	g.genFiles = make([]*FileDescriptor, 0, len(g.Request.FileToGenerate))
	for _, fileName := range g.Request.FileToGenerate {
		fd := g.allFilesByName[fileName]
		if fd == nil {
			g.Fail("could not find file named", fileName)
		}
		fd.index = len(g.genFiles)
		g.genFiles = append(g.genFiles, fd)
	}
}

// Scan the descriptors in this file.  For each one, build the slice of nested descriptors
func (g *Generator) buildNestedDescriptors(descs []*Descriptor) {
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

func (g *Generator) buildNestedEnums(descs []*Descriptor, enums []*EnumDescriptor) {
	for _, desc := range descs {
		if len(desc.EnumType) != 0 {
			for _, enum := range enums {
				if enum.parent == desc {
					desc.enums = append(desc.enums, enum)
				}
			}
			if len(desc.enums) != len(desc.EnumType) {
				g.Fail("internal error: enum nesting failure for", desc.GetName())
			}
		}
	}
}

// Construct the Descriptor
func newDescriptor(desc *descriptor.DescriptorProto, parent *Descriptor, file *descriptor.FileDescriptorProto, index int) *Descriptor {
	d := &Descriptor{
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
		d.ext = append(d.ext, &ExtensionDescriptor{common{file}, field, d})
	}

	return d
}

// Return a slice of all the Descriptors defined within this file
func wrapDescriptors(file *descriptor.FileDescriptorProto) []*Descriptor {
	sl := make([]*Descriptor, 0, len(file.MessageType)+10)
	for i, desc := range file.MessageType {
		sl = wrapThisDescriptor(sl, desc, nil, file, i)
	}
	return sl
}

// Wrap this Descriptor, recursively
func wrapThisDescriptor(sl []*Descriptor, desc *descriptor.DescriptorProto, parent *Descriptor, file *descriptor.FileDescriptorProto, index int) []*Descriptor {
	sl = append(sl, newDescriptor(desc, parent, file, index))
	me := sl[len(sl)-1]
	for i, nested := range desc.NestedType {
		sl = wrapThisDescriptor(sl, nested, me, file, i)
	}
	return sl
}

// Construct the EnumDescriptor
func newEnumDescriptor(desc *descriptor.EnumDescriptorProto, parent *Descriptor, file *descriptor.FileDescriptorProto, index int) *EnumDescriptor {
	ed := &EnumDescriptor{
		common:              common{file},
		EnumDescriptorProto: desc,
		parent:              parent,
		index:               index,
	}
	if parent == nil {
		ed.path = fmt.Sprintf("%d,%d", enumPath, index)
	} else {
		ed.path = fmt.Sprintf("%s,%d,%d", parent.path, messageEnumPath, index)
	}
	return ed
}

// Return a slice of all the EnumDescriptors defined within this file
func wrapEnumDescriptors(file *descriptor.FileDescriptorProto, descs []*Descriptor) []*EnumDescriptor {
	sl := make([]*EnumDescriptor, 0, len(file.EnumType)+10)
	// Top-level enums.
	for i, enum := range file.EnumType {
		sl = append(sl, newEnumDescriptor(enum, nil, file, i))
	}
	// Enums within messages. Enums within embedded messages appear in the outer-most message.
	for _, nested := range descs {
		for i, enum := range nested.EnumType {
			sl = append(sl, newEnumDescriptor(enum, nested, file, i))
		}
	}
	return sl
}

// Return a slice of all the top-level ExtensionDescriptors defined within this file.
func wrapExtensions(file *descriptor.FileDescriptorProto) []*ExtensionDescriptor {
	var sl []*ExtensionDescriptor
	for _, field := range file.Extension {
		sl = append(sl, &ExtensionDescriptor{common{file}, field, nil})
	}
	return sl
}

// Return a slice of all the types that are publicly imported into this file.
func wrapImported(file *descriptor.FileDescriptorProto, g *Generator) (sl []*ImportedDescriptor) {
	for _, index := range file.PublicDependency {
		df := g.fileByName(file.Dependency[index])
		for _, d := range df.desc {
			if d.GetOptions().GetMapEntry() {
				continue
			}
			sl = append(sl, &ImportedDescriptor{common{file}, d})
		}
		for _, e := range df.enum {
			sl = append(sl, &ImportedDescriptor{common{file}, e})
		}
		for _, ext := range df.ext {
			sl = append(sl, &ImportedDescriptor{common{file}, ext})
		}
	}
	return
}

func extractComments(file *FileDescriptor) {
	file.comments = make(map[string]*descriptor.SourceCodeInfo_Location)
	for _, loc := range file.GetSourceCodeInfo().GetLocation() {
		if loc.LeadingComments == nil {
			continue
		}
		var p []string
		for _, n := range loc.Path {
			p = append(p, strconv.Itoa(int(n)))
		}
		file.comments[strings.Join(p, ",")] = loc
	}
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

// ObjectNamed, given a fully-qualified input type name as it appears in the input data,
// returns the descriptor for the message or enum with that name.
func (g *Generator) ObjectNamed(typeName string) Object {
	o, ok := g.typeNameToObject[typeName]
	if !ok {
		g.Fail("can't find object with type", typeName)
	}

	// If the file of this object isn't a direct dependency of the current file,
	// or in the current file, then this object has been publicly imported into
	// a dependency of the current file.
	// We should return the ImportedDescriptor object for it instead.
	direct := *o.File().Name == *g.file.Name
	if !direct {
		for _, dep := range g.file.Dependency {
			if *g.fileByName(dep).Name == *o.File().Name {
				direct = true
				break
			}
		}
	}
	if !direct {
		found := false
	Loop:
		for _, dep := range g.file.Dependency {
			df := g.fileByName(*g.fileByName(dep).Name)
			for _, td := range df.imp {
				if td.o == o {
					// Found it!
					o = td
					found = true
					break Loop
				}
			}
		}
		if !found {
			log.Printf("protoc-gen-go: WARNING: failed finding publicly imported dependency for %v, used in %v", typeName, *g.file.Name)
		}
	}

	return o
}

// P prints the arguments to the generated output.  It handles strings and int32s, plus
// handling indirections because they may be *string, etc.
func (g *Generator) P(str ...interface{}) {
	if !g.writeOutput {
		return
	}
	g.WriteString(g.indent)
	for _, v := range str {
		switch s := v.(type) {
		case string:
			g.WriteString(s)
		case *string:
			g.WriteString(*s)
		case bool:
			fmt.Fprintf(g, "%t", s)
		case *bool:
			fmt.Fprintf(g, "%t", *s)
		case int:
			fmt.Fprintf(g, "%d", s)
		case *int32:
			fmt.Fprintf(g, "%d", *s)
		case *int64:
			fmt.Fprintf(g, "%d", *s)
		case float64:
			fmt.Fprintf(g, "%g", s)
		case *float64:
			fmt.Fprintf(g, "%g", *s)
		default:
			g.Fail(fmt.Sprintf("unknown type in printer: %T", v))
		}
	}
	g.WriteByte('\n')
}

// addInitf stores the given statement to be printed inside the file's init function.
// The statement is given as a format specifier and arguments.
func (g *Generator) addInitf(stmt string, a ...interface{}) {
	g.init = append(g.init, fmt.Sprintf(stmt, a...))
}

// In Indents the output one tab stop.
func (g *Generator) In() { g.indent += "\t" }

// Out unindents the output one tab stop.
func (g *Generator) Out() {
	if len(g.indent) > 0 {
		g.indent = g.indent[1:]
	}
}

// GenerateAllFiles generates the output for all the files we're outputting.
func (g *Generator) GenerateAllFiles() {
	// Initialize the plugins
	for _, p := range plugins {
		p.Init(g)
	}
	// Generate the output. The generator runs for every file, even the files
	// that we don't generate output for, so that we can collate the full list
	// of exported symbols to support public imports.
	genFileMap := make(map[*FileDescriptor]bool, len(g.genFiles))
	for _, file := range g.genFiles {
		genFileMap[file] = true
	}
	for _, file := range g.allFiles {
		g.Reset()
		g.writeOutput = genFileMap[file]
		g.generate(file)
		if !g.writeOutput {
			continue
		}
		g.Response.File = append(g.Response.File, &plugin.CodeGeneratorResponse_File{
			Name:    proto.String(file.goFileName()),
			Content: proto.String(g.String()),
		})
	}
}

// Run all the plugins associated with the file.
func (g *Generator) runPlugins(file *FileDescriptor) {
	for _, p := range plugins {
		p.Generate(file)
	}
}

// FileOf return the FileDescriptor for this FileDescriptorProto.
func (g *Generator) FileOf(fd *descriptor.FileDescriptorProto) *FileDescriptor {
	for _, file := range g.allFiles {
		if file.FileDescriptorProto == fd {
			return file
		}
	}
	g.Fail("could not find file in table:", fd.GetName())
	return nil
}

// Fill the response protocol buffer with the generated output for all the files we're
// supposed to generate.
func (g *Generator) generate(file *FileDescriptor) {
	g.file = g.FileOf(file.FileDescriptorProto)
	g.usedPackages = make(map[string]bool)

	if g.file.index == 0 {
		// For one file in the package, assert version compatibility.
		g.P("// This is a compile-time assertion to ensure that this generated file")
		g.P("// is compatible with the proto package it is being compiled against.")
		g.P("// A compilation error at this line likely means your copy of the")
		g.P("// proto package needs to be updated.")
		g.P("const _ = ", g.Pkg["proto"], ".ProtoPackageIsVersion", generatedCodeVersion, " // please upgrade the proto package")
		g.P()
	}
	for _, td := range g.file.imp {
		g.generateImported(td)
	}
	for _, enum := range g.file.enum {
		g.generateEnum(enum)
	}
	for _, desc := range g.file.desc {
		// Don't generate virtual messages for maps.
		if desc.GetOptions().GetMapEntry() {
			continue
		}
		g.generateMessage(desc)
	}
	for _, ext := range g.file.ext {
		g.generateExtension(ext)
	}
	g.generateInitFunction()

	// Run the plugins before the imports so we know which imports are necessary.
	g.runPlugins(file)

	g.generateFileDescriptor(file)

	// Generate header and imports last, though they appear first in the output.
	rem := g.Buffer
	g.Buffer = new(bytes.Buffer)
	g.generateHeader()
	g.generateImports()
	if !g.writeOutput {
		return
	}
	g.Write(rem.Bytes())

	// Reformat generated code.
	fset := token.NewFileSet()
	raw := g.Bytes()
	ast, err := parser.ParseFile(fset, "", g, parser.ParseComments)
	if err != nil {
		// Print out the bad code with line numbers.
		// This should never happen in practice, but it can while changing generated code,
		// so consider this a debugging aid.
		var src bytes.Buffer
		s := bufio.NewScanner(bytes.NewReader(raw))
		for line := 1; s.Scan(); line++ {
			fmt.Fprintf(&src, "%5d\t%s\n", line, s.Bytes())
		}
		g.Fail("bad Go source code was generated:", err.Error(), "\n"+src.String())
	}
	g.Reset()
	err = (&printer.Config{Mode: printer.TabIndent | printer.UseSpaces, Tabwidth: 8}).Fprint(g, fset, ast)
	if err != nil {
		g.Fail("generated Go source code could not be reformatted:", err.Error())
	}
}

// Generate the header, including package definition
func (g *Generator) generateHeader() {
	g.P("// Code generated by protoc-gen-go. DO NOT EDIT.")
	g.P("// source: ", g.file.Name)
	g.P()

	name := g.file.PackageName()

	if g.file.index == 0 {
		// Generate package docs for the first file in the package.
		g.P("/*")
		g.P("Package ", name, " is a generated protocol buffer package.")
		g.P()
		if loc, ok := g.file.comments[strconv.Itoa(packagePath)]; ok {
			// not using g.PrintComments because this is a /* */ comment block.
			text := strings.TrimSuffix(loc.GetLeadingComments(), "\n")
			for _, line := range strings.Split(text, "\n") {
				line = strings.TrimPrefix(line, " ")
				// ensure we don't escape from the block comment
				line = strings.Replace(line, "*/", "* /", -1)
				g.P(line)
			}
			g.P()
		}
		var topMsgs []string
		g.P("It is generated from these files:")
		for _, f := range g.genFiles {
			g.P("\t", f.Name)
			for _, msg := range f.desc {
				if msg.parent != nil {
					continue
				}
				topMsgs = append(topMsgs, CamelCaseSlice(msg.TypeName()))
			}
		}
		g.P()
		g.P("It has these top-level messages:")
		for _, msg := range topMsgs {
			g.P("\t", msg)
		}
		g.P("*/")
	}

	g.P("package ", name)
	g.P()
}

// PrintComments prints any comments from the source .proto file.
// The path is a comma-separated list of integers.
// It returns an indication of whether any comments were printed.
// See descriptor.proto for its format.
func (g *Generator) PrintComments(path string) bool {
	if !g.writeOutput {
		return false
	}
	if loc, ok := g.file.comments[path]; ok {
		text := strings.TrimSuffix(loc.GetLeadingComments(), "\n")
		for _, line := range strings.Split(text, "\n") {
			g.P("// ", strings.TrimPrefix(line, " "))
		}
		return true
	}
	return false
}

func (g *Generator) fileByName(filename string) *FileDescriptor {
	return g.allFilesByName[filename]
}

// weak returns whether the ith import of the current file is a weak import.
func (g *Generator) weak(i int32) bool {
	for _, j := range g.file.WeakDependency {
		if j == i {
			return true
		}
	}
	return false
}

// Generate the imports
func (g *Generator) generateImports() {
	// We almost always need a proto import.  Rather than computing when we
	// do, which is tricky when there's a plugin, just import it and
	// reference it later. The same argument applies to the fmt and math packages.
	g.P("import " + g.Pkg["proto"] + " " + strconv.Quote(g.ImportPrefix+"github.com/golang/protobuf/proto"))
	g.P("import " + g.Pkg["fmt"] + ` "fmt"`)
	g.P("import " + g.Pkg["math"] + ` "math"`)
	for i, s := range g.file.Dependency {
		fd := g.fileByName(s)
		// Do not import our own package.
		if fd.PackageName() == g.packageName {
			continue
		}
		filename := fd.goFileName()
		// By default, import path is the dirname of the Go filename.
		importPath := path.Dir(filename)
		if substitution, ok := g.ImportMap[s]; ok {
			importPath = substitution
		}
		importPath = g.ImportPrefix + importPath
		// Skip weak imports.
		if g.weak(int32(i)) {
			g.P("// skipping weak import ", fd.PackageName(), " ", strconv.Quote(importPath))
			continue
		}
		// We need to import all the dependencies, even if we don't reference them,
		// because other code and tools depend on having the full transitive closure
		// of protocol buffer types in the binary.
		pname := fd.PackageName()
		if _, ok := g.usedPackages[pname]; !ok {
			pname = "_"
		}
		g.P("import ", pname, " ", strconv.Quote(importPath))
	}
	g.P()
	// TODO: may need to worry about uniqueness across plugins
	for _, p := range plugins {
		p.GenerateImports(g.file)
		g.P()
	}
	g.P("// Reference imports to suppress errors if they are not otherwise used.")
	g.P("var _ = ", g.Pkg["proto"], ".Marshal")
	g.P("var _ = ", g.Pkg["fmt"], ".Errorf")
	g.P("var _ = ", g.Pkg["math"], ".Inf")
	g.P()
}

func (g *Generator) generateImported(id *ImportedDescriptor) {
	// Don't generate public import symbols for files that we are generating
	// code for, since those symbols will already be in this package.
	// We can't simply avoid creating the ImportedDescriptor objects,
	// because g.genFiles isn't populated at that stage.
	tn := id.TypeName()
	sn := tn[len(tn)-1]
	df := g.FileOf(id.o.File())
	filename := *df.Name
	for _, fd := range g.genFiles {
		if *fd.Name == filename {
			g.P("// Ignoring public import of ", sn, " from ", filename)
			g.P()
			return
		}
	}
	g.P("// ", sn, " from public import ", filename)
	g.usedPackages[df.PackageName()] = true

	for _, sym := range df.exported[id.o] {
		sym.GenerateAlias(g, df.PackageName())
	}

	g.P()
}

// Generate the enum definitions for this EnumDescriptor.
func (g *Generator) generateEnum(enum *EnumDescriptor) {
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
	for m := enum.parent; m != nil; m = m.parent {
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
func (g *Generator) goTag(message *Descriptor, field *descriptor.FieldDescriptorProto, wiretype string) string {
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
			enum, ok := obj.(*EnumDescriptor)
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

func needsStar(typ descriptor.FieldDescriptorProto_Type) bool {
	switch typ {
	case descriptor.FieldDescriptorProto_TYPE_GROUP:
		return false
	case descriptor.FieldDescriptorProto_TYPE_MESSAGE:
		return false
	case descriptor.FieldDescriptorProto_TYPE_BYTES:
		return false
	}
	return true
}

// TypeName is the printed name appropriate for an item. If the object is in the current file,
// TypeName drops the package name and underscores the rest.
// Otherwise the object is from another package; and the result is the underscored
// package name followed by the item name.
// The result always has an initial capital.
func (g *Generator) TypeName(obj Object) string {
	return g.DefaultPackageName(obj) + CamelCaseSlice(obj.TypeName())
}

// TypeNameWithPackage is like TypeName, but always includes the package
// name even if the object is in our own package.
func (g *Generator) TypeNameWithPackage(obj Object) string {
	return obj.PackageName() + CamelCaseSlice(obj.TypeName())
}

// GoType returns a string representing the type name, and the wire type
func (g *Generator) GoType(message *Descriptor, field *descriptor.FieldDescriptorProto) (typ string, wire string) {
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

// Method names that may be generated.  Fields with these names get an
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

// Generate the type and default constant definitions for this Descriptor.
func (g *Generator) generateMessage(message *Descriptor) {
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
			if d, ok := desc.(*Descriptor); ok && d.GetOptions().GetMapEntry() {
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
	// if we change proto.MessageName to work with multiple equivalents.
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
			g.P("// ensure ", ccTypeName, " satisfies proto.Marshaler and proto.Unmarshaler")
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
			var enum *EnumDescriptor
			if id, ok := obj.(*ImportedDescriptor); ok {
				// The enum type has been publicly imported.
				enum, _ = id.o.(*EnumDescriptor)
			} else {
				enum, _ = obj.(*EnumDescriptor)
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
		// the importer may not already be importing the defining .proto.
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
				var enum *EnumDescriptor
				if id, ok := obj.(*ImportedDescriptor); ok {
					// The enum type has been publicly imported.
					enum, _ = id.o.(*EnumDescriptor)
				} else {
					enum, _ = obj.(*EnumDescriptor)
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

	for _, ext := range message.ext {
		g.generateExtension(ext)
	}

	fullName := strings.Join(message.TypeName(), ".")
	if g.file.Package != nil {
		fullName = *g.file.Package + "." + fullName
	}

	g.addInitf("%s.RegisterType((*%s)(nil), %q)", g.Pkg["proto"], ccTypeName, fullName)
}

var escapeChars = [256]byte{
	'a': '\a', 'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t', 'v': '\v', '\\': '\\', '"': '"', '\'': '\'', '?': '?',
}

// unescape reverses the "C" escaping that protoc does for default values of bytes fields.
// It is best effort in that it effectively ignores malformed input. Seemingly invalid escape
// sequences are conveyed, unmodified, into the decoded result.
func unescape(s string) string {
	// NB: Sadly, we can't use strconv.Unquote because protoc will escape both
	// single and double quotes, but strconv.Unquote only allows one or the
	// other (based on actual surrounding quotes of its input argument).

	var out []byte
	for len(s) > 0 {
		// regular character, or too short to be valid escape
		if s[0] != '\\' || len(s) < 2 {
			out = append(out, s[0])
			s = s[1:]
		} else if c := escapeChars[s[1]]; c != 0 {
			// escape sequence
			out = append(out, c)
			s = s[2:]
		} else if s[1] == 'x' || s[1] == 'X' {
			// hex escape, e.g. "\x80
			if len(s) < 4 {
				// too short to be valid
				out = append(out, s[:2]...)
				s = s[2:]
				continue
			}
			v, err := strconv.ParseUint(s[2:4], 16, 8)
			if err != nil {
				out = append(out, s[:4]...)
			} else {
				out = append(out, byte(v))
			}
			s = s[4:]
		} else if '0' <= s[1] && s[1] <= '7' {
			// octal escape, can vary from 1 to 3 octal digits; e.g., "\0" "\40" or "\164"
			// so consume up to 2 more bytes or up to end-of-string
			n := len(s[1:]) - len(strings.TrimLeft(s[1:], "01234567"))
			if n > 3 {
				n = 3
			}
			v, err := strconv.ParseUint(s[1:1+n], 8, 8)
			if err != nil {
				out = append(out, s[:1+n]...)
			} else {
				out = append(out, byte(v))
			}
			s = s[1+n:]
		} else {
			// bad escape, just propagate the slash as-is
			out = append(out, s[0])
			s = s[1:]
		}
	}

	return string(out)
}

func (g *Generator) generateExtension(ext *ExtensionDescriptor) {
	ccTypeName := ext.DescName()

	extObj := g.ObjectNamed(*ext.Extendee)
	var extDesc *Descriptor
	if id, ok := extObj.(*ImportedDescriptor); ok {
		// This is extending a publicly imported message.
		// We need the underlying type for goTag.
		extDesc = id.o.(*Descriptor)
	} else {
		extDesc = extObj.(*Descriptor)
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

func (g *Generator) generateInitFunction() {
	for _, enum := range g.file.enum {
		g.generateEnumRegistration(enum)
	}
	for _, d := range g.file.desc {
		for _, ext := range d.ext {
			g.generateExtensionRegistration(ext)
		}
	}
	for _, ext := range g.file.ext {
		g.generateExtensionRegistration(ext)
	}
	if len(g.init) == 0 {
		return
	}
	g.P("func init() {")
	g.In()
	for _, l := range g.init {
		g.P(l)
	}
	g.Out()
	g.P("}")
	g.init = nil
}

func (g *Generator) generateFileDescriptor(file *FileDescriptor) {
	// Make a copy and trim source_code_info data.
	// TODO: Trim this more when we know exactly what we need.
	pb := proto.Clone(file.FileDescriptorProto).(*descriptor.FileDescriptorProto)
	pb.SourceCodeInfo = nil

	b, err := proto.Marshal(pb)
	if err != nil {
		g.Fail(err.Error())
	}

	var buf bytes.Buffer
	w, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	w.Write(b)
	w.Close()
	b = buf.Bytes()

	v := file.VarName()
	g.P()
	g.P("func init() { ", g.Pkg["proto"], ".RegisterFile(", strconv.Quote(*file.Name), ", ", v, ") }")
	g.P("var ", v, " = []byte{")
	g.In()
	g.P("// ", len(b), " bytes of a gzipped FileDescriptorProto")
	for len(b) > 0 {
		n := 16
		if n > len(b) {
			n = len(b)
		}

		s := ""
		for _, c := range b[:n] {
			s += fmt.Sprintf("0x%02x,", c)
		}
		g.P(s)

		b = b[n:]
	}
	g.Out()
	g.P("}")
}

func (g *Generator) generateEnumRegistration(enum *EnumDescriptor) {
	// // We always print the full (proto-world) package name here.
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

func (g *Generator) generateExtensionRegistration(ext *ExtensionDescriptor) {
	g.addInitf("%s.RegisterExtension(%s)", g.Pkg["proto"], ext.DescName())
}

// And now lots of helper functions.

// Is c an ASCII lower-case letter?
func isASCIILower(c byte) bool {
	return 'a' <= c && c <= 'z'
}

// Is c an ASCII digit?
func isASCIIDigit(c byte) bool {
	return '0' <= c && c <= '9'
}

// CamelCase returns the CamelCased name.
// If there is an interior underscore followed by a lower case letter,
// drop the underscore and convert the letter to upper case.
// There is a remote possibility of this rewrite causing a name collision,
// but it's so remote we're prepared to pretend it's nonexistent - since the
// C++ generator lowercases names, it's extremely unlikely to have two fields
// with different capitalizations.
// In short, _my_field_name_2 becomes XMyFieldName_2.
func CamelCase(s string) string {
	if s == "" {
		return ""
	}
	t := make([]byte, 0, 32)
	i := 0
	if s[0] == '_' {
		// Need a capital letter; drop the '_'.
		t = append(t, 'X')
		i++
	}
	// Invariant: if the next letter is lower case, it must be converted
	// to upper case.
	// That is, we process a word at a time, where words are marked by _ or
	// upper case letter. Digits are treated as words.
	for ; i < len(s); i++ {
		c := s[i]
		if c == '_' && i+1 < len(s) && isASCIILower(s[i+1]) {
			continue // Skip the underscore in s.
		}
		if isASCIIDigit(c) {
			t = append(t, c)
			continue
		}
		// Assume we have a letter now - if not, it's a bogus identifier.
		// The next word is a sequence of characters that must start upper case.
		if isASCIILower(c) {
			c ^= ' ' // Make it a capital letter.
		}
		t = append(t, c) // Guaranteed not lower case.
		// Accept lower case sequence that follows.
		for i+1 < len(s) && isASCIILower(s[i+1]) {
			i++
			t = append(t, s[i])
		}
	}
	return string(t)
}

// CamelCaseSlice is like CamelCase, but the argument is a slice of strings to
// be joined with "_".
func CamelCaseSlice(elem []string) string { return CamelCase(strings.Join(elem, "_")) }

// dottedSlice turns a sliced name into a dotted name.
func dottedSlice(elem []string) string { return strings.Join(elem, ".") }

// Is this field optional?
func isOptional(field *descriptor.FieldDescriptorProto) bool {
	return field.Label != nil && *field.Label == descriptor.FieldDescriptorProto_LABEL_OPTIONAL
}

// Is this field required?
func isRequired(field *descriptor.FieldDescriptorProto) bool {
	return field.Label != nil && *field.Label == descriptor.FieldDescriptorProto_LABEL_REQUIRED
}

// Is this field repeated?
func isRepeated(field *descriptor.FieldDescriptorProto) bool {
	return field.Label != nil && *field.Label == descriptor.FieldDescriptorProto_LABEL_REPEATED
}

// Is this field a scalar numeric type?
func isScalar(field *descriptor.FieldDescriptorProto) bool {
	if field.Type == nil {
		return false
	}
	switch *field.Type {
	case descriptor.FieldDescriptorProto_TYPE_DOUBLE,
		descriptor.FieldDescriptorProto_TYPE_FLOAT,
		descriptor.FieldDescriptorProto_TYPE_INT64,
		descriptor.FieldDescriptorProto_TYPE_UINT64,
		descriptor.FieldDescriptorProto_TYPE_INT32,
		descriptor.FieldDescriptorProto_TYPE_FIXED64,
		descriptor.FieldDescriptorProto_TYPE_FIXED32,
		descriptor.FieldDescriptorProto_TYPE_BOOL,
		descriptor.FieldDescriptorProto_TYPE_UINT32,
		descriptor.FieldDescriptorProto_TYPE_ENUM,
		descriptor.FieldDescriptorProto_TYPE_SFIXED32,
		descriptor.FieldDescriptorProto_TYPE_SFIXED64,
		descriptor.FieldDescriptorProto_TYPE_SINT32,
		descriptor.FieldDescriptorProto_TYPE_SINT64:
		return true
	default:
		return false
	}
}

// badToUnderscore is the mapping function used to generate Go names from package names,
// which can be dotted in the input .proto file.  It replaces non-identifier characters such as
// dot or dash with underscore.
func badToUnderscore(r rune) rune {
	if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
		return r
	}
	return '_'
}

// baseName returns the last path element of the name, with the last dotted suffix removed.
func baseName(name string) string {
	// First, find the last element
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	// Now drop the suffix
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[0:i]
	}
	return name
}

// The SourceCodeInfo message describes the location of elements of a parsed
// .proto file by way of a "path", which is a sequence of integers that
// describe the route from a FileDescriptorProto to the relevant submessage.
// The path alternates between a field number of a repeated field, and an index
// into that repeated field. The constants below define the field numbers that
// are used.
//
// See descriptor.proto for more information about this.
const (
	// tag numbers in FileDescriptorProto
	packagePath = 2 // package
	messagePath = 4 // message_type
	enumPath    = 5 // enum_type
	// tag numbers in DescriptorProto
	messageFieldPath   = 2 // field
	messageMessagePath = 3 // nested_type
	messageEnumPath    = 4 // enum_type
	messageOneofPath   = 8 // oneof_decl
	// tag numbers in EnumDescriptorProto
	enumValuePath = 2 // value
)

package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"log"
	"path"
	"strconv"
	"strings"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/protobuf/protoc-gen-go/descriptor"
	plugin "github.com/golang/protobuf/protoc-gen-go/plugin"
)

// FileDescriptor describes a protocol buffer descriptor file (.proto). It
// includes slices of all the messages and enums defined within it. Those slices
// are constructed by WrapTypes.
type fileDescriptor struct {
	*descriptor.FileDescriptorProto
	messages   []*messageDescriptor   // All the messages defined in this file.
	enums      []*enumDescriptor      // All the enums defined in this file.
	extensions []*extensionDescriptor // All the top-level extensions defined in this file.
	imports    []*importDescriptor    // All types defined in files publicly imported by this file.

	// Comments stored as a map of path (comma-separated integers) to the comment.
	comments map[string]*descriptor.SourceCodeInfo_Location

	// The full list of symbols that are exported, as a map from the exported
	// object to its symbols. This is used for supporting public imports.
	exports map[ProtoObject][]symbol

	index  int  // The index of this file in the list of files to generate code for
	proto3 bool // whether to generate proto3 code for this file
}

// PackageName is the package name we'll use in the generated code to refer to
// this file.
func (d *fileDescriptor) PackageName() string {
	return uniquePackageOf(d.FileDescriptorProto)
}

// VarName is the variable name we'll use in the generated code to refer
// to the compressed bytes of this descriptor. It is not exported, so
// it is only valid inside the generated package.
func (d *fileDescriptor) VarName() string {
	return fmt.Sprintf("fileDescriptor%d", d.index)
}

// goPackageOption interprets the file's go_package option.
// If there is no go_package, it returns ("", "", false).
// If there's a simple name, it returns ("", pkg, true).
// If the option implies an import path, it returns (impPath, pkg, true).
func (d *fileDescriptor) goPackageOption() (impPath, pkg string, ok bool) {
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

// goFileName returns the output name for the generated Go file.
func (d *fileDescriptor) goFileName() string {
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

// goPackageName returns the Go package name to use in the
// generated Go file.  The result explicit reports whether the name
// came from an option go_package statement.  If explicit is false,
// the name was derived from the protocol buffer's package statement
// or the input file name.
func (d *fileDescriptor) goPackageName() (name string, explicit bool) {
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
func (d *fileDescriptor) outputFileName() string {
	name := *d.Name
	if ext := path.Ext(name); ext == ".proto" || ext == ".protodevel" {
		name = name[:len(name)-len(ext)]
	}
	return name + ".pb.ts"
}

func (d *fileDescriptor) addExport(obj ProtoObject, sym symbol) {
	d.exports[obj] = append(d.exports[obj], sym)
}

func fileIsProto3(file *descriptor.FileDescriptorProto) bool {
	return file.GetSyntax() == "proto3"
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

// GenerateAllFiles generates the output for all the files we're outputting.
func (g *Generator) GenerateAllFiles() {
	// Generate the output. The generator runs for every file, even the files
	// that we don't generate output for, so that we can collate the full list
	// of exported symbols to support public imports.
	genFileMap := make(map[*fileDescriptor]bool, len(g.genFiles))
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
			Name:    proto.String(file.outputFileName()),
			Content: proto.String(g.String()),
		})
	}
}

// FileOf return the FileDescriptor for this FileDescriptorProto.
func (g *Generator) FileOf(fd *descriptor.FileDescriptorProto) *fileDescriptor {
	for _, file := range g.allFiles {
		if file.FileDescriptorProto == fd {
			return file
		}
	}
	g.Fail("could not find file in table:", fd.GetName())
	return nil
}

func (g *Generator) fileByName(filename string) *fileDescriptor {
	return g.allFilesByName[filename]
}

func (g *Generator) generateFileDescriptor(file *fileDescriptor) {
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

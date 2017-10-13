// Package descriptor defines structs that describe protobufs in a manner
// similar to an AST. These details can then be used to generate code in a
// particular language.
//
// These descriptors are composed of the standard protobuf descriptors.
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"go/parser"
	"go/printer"
	"go/token"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/golang/protobuf/protoc-gen-go/descriptor"
	plugin "github.com/golang/protobuf/protoc-gen-go/plugin"
)

// Generator is the type whose methods generate the output, stored in the
// associated response structure.
type Generator struct {
	*bytes.Buffer

	Request  *plugin.CodeGeneratorRequest  // The input.
	Response *plugin.CodeGeneratorResponse // The output.

	Parameter         map[string]string // Command-line parameters.
	PackageImportPath string            // Go import path of the package we're generating code for
	ImportPrefix      string            // String to prefix to imported package file names.
	ImportMap         map[string]string // Mapping from .proto file name to import path.

	Pkg map[string]string // The names under which we import support packages

	packageName      string                     // What we're calling ourselves.
	allFiles         []*fileDescriptor          // All files in the tree
	allFilesByName   map[string]*fileDescriptor // All files by filename.
	genFiles         []*fileDescriptor          // Those files we will generate output for.
	file             *fileDescriptor            // The file we are compiling now.
	usedPackages     map[string]bool            // Names of packages used in current file.
	typeNameToObject map[string]ProtoObject     // Key is a fully-qualified name in input syntax.
	init             []string                   // Lines to emit in the init function.
	indent           string
	writeOutput      bool
}

// new creates a new generator and allocates the request and response
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
	log.Print("protoc-gen-ts: error:", s)
	os.Exit(1)
}

// Fail reports a problem and exits the program.
func (g *Generator) Fail(msgs ...string) {
	s := strings.Join(msgs, " ")
	log.Print("protoc-gen-ts: error:", s)
	os.Exit(1)
}

// CommandLineParameters breaks the comma-separated list of key=value pairs
// in the parameter (a member of the request protobuf) into a key/value map.
// It then sets file name mappings defined by those entries.
func (g *Generator) CommandLineParameters(parameter string) {
	g.Parameter = make(map[string]string)
	for _, p := range strings.Split(parameter, ",") {
		if i := strings.Index(p, "="); i < 0 {
			g.Parameter[p] = ""
		} else {
			g.Parameter[p[0:i]] = p[i+1:]
		}
	}

	g.ImportMap = make(map[string]string)

	for k, v := range g.Parameter {
		switch k {
		case "import_prefix":
			g.ImportPrefix = v
		case "import_path":
			g.PackageImportPath = v
		default:
			if len(k) > 0 && k[0] == 'M' {
				g.ImportMap[k[1:]] = v
			}
		}
	}
}

// ObjectNamed, given a fully-qualified input type name as it appears in the input data,
// returns the descriptor for the message or enum with that name.
func (g *Generator) ObjectNamed(typeName string) ProtoObject {
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
			for _, td := range df.imports {
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

// addInitf stores the given statement to be printed inside the file's init function.
// The statement is given as a format specifier and arguments.
func (g *Generator) addInitf(stmt string, a ...interface{}) {
	g.init = append(g.init, fmt.Sprintf(stmt, a...))
}

// Fill the response protocol buffer with the generated output for all the files we're
// supposed to generate.
func (g *Generator) generate(file *fileDescriptor) {
	g.file = g.FileOf(file.FileDescriptorProto)
	g.usedPackages = make(map[string]bool)

	if g.file.index == 0 {
		// For one file in the package, assert version compatibility.
		g.P("// This is a compile-time assertion to ensure that this generated file")
		g.P("// is compatible with the proto package it is being compiled against.")
		g.P("// A compilation error at this line likely means your copy of the")
		g.P("// proto package needs to be updated.")
		g.P()
	}
	for _, td := range g.file.imports {
		g.generateImported(td)
	}
	for _, enum := range g.file.enums {
		g.generateEnum(enum)
	}
	for _, desc := range g.file.messages {
		// Don't generate virtual messages for maps.
		if desc.GetOptions().GetMapEntry() {
			continue
		}
		g.generateMessage(desc)
	}
	for _, ext := range g.file.extensions {
		g.generateExtension(ext)
	}
	g.generateInitFunction()

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
			for _, msg := range f.messages {
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

// weak returns whether the ith import of the current file is a weak import.
func (g *Generator) weak(i int32) bool {
	for _, j := range g.file.WeakDependency {
		if j == i {
			return true
		}
	}
	return false
}

// needStar returns whether the type should be a pointer.
func needsStar(typ descriptor.FieldDescriptorProto_Type) bool {
	switch typ {
	case descriptor.FieldDescriptorProto_TYPE_GROUP:
	case descriptor.FieldDescriptorProto_TYPE_MESSAGE:
	case descriptor.FieldDescriptorProto_TYPE_BYTES:
		return false
	}
	return true
}

func (g *Generator) generateInitFunction() {
	for _, enum := range g.file.enums {
		g.generateEnumRegistration(enum)
	}
	for _, msg := range g.file.messages {
		for _, ext := range msg.extensions {
			g.generateExtensionRegistration(ext)
		}
	}
	for _, ext := range g.file.extensions {
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

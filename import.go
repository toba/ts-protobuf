package main

import (
	"path"
	"strconv"

	"github.com/golang/protobuf/protoc-gen-go/descriptor"
)

// importDescriptor describes a type that has been publicly imported from
// another file.
type importDescriptor struct {
	common
	o ProtoObject
}

func (id *importDescriptor) TypeName() []string {
	return id.o.TypeName()
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
	g.P("// Reference imports to suppress errors if they are not otherwise used.")
	g.P("var _ = ", g.Pkg["proto"], ".Marshal")
	g.P("var _ = ", g.Pkg["fmt"], ".Errorf")
	g.P("var _ = ", g.Pkg["math"], ".Inf")
	g.P()
}

func (g *Generator) generateImported(id *importDescriptor) {
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

	for _, sym := range df.exports[id.o] {
		sym.GenerateAlias(g, df.PackageName())
	}

	g.P()
}

// Return a slice of all the types that are publicly imported into this file.
func wrapImported(file *descriptor.FileDescriptorProto, g *Generator) (sl []*importDescriptor) {
	for _, index := range file.PublicDependency {
		df := g.fileByName(file.Dependency[index])
		for _, d := range df.messages {
			if d.GetOptions().GetMapEntry() {
				continue
			}
			sl = append(sl, &importDescriptor{common{file}, d})
		}
		for _, e := range df.enums {
			sl = append(sl, &importDescriptor{common{file}, e})
		}
		for _, ext := range df.extensions {
			sl = append(sl, &importDescriptor{common{file}, ext})
		}
	}
	return
}

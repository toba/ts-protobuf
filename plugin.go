package main

import (
	"github.com/golang/protobuf/protoc-gen-go/generator"
)

// typeScript is an implementation of the Go protocol buffer compiler's
// plugin architecture. It generates bindings for TypeScript.
type typeScript struct {
	gen *generator.Generator
}

func init() {
	generator.RegisterPlugin(new(typeScript))
}

// Name returns the name of this plugin.
func (ts *typeScript) Name() string {
	return "TypeScript"
}

// Init initializes the TypeScript plugin.
func (ts *typeScript) Init(gen *generator.Generator) {
	ts.gen = gen
}

// P forwards to g.gen.P.
func (ts *typeScript) P(args ...interface{}) { ts.gen.P(args...) }

// Generate generates code for the services in the given file.
func (ts *typeScript) Generate(file *generator.FileDescriptor) {
	if len(file.FileDescriptorProto.Service) == 0 {
		return
	}

	for i, service := range file.FileDescriptorProto.Service {
		ts.generateService(file, service, i)
	}
}

// GenerateImports generates the import declaration for this file.
func (ts *typeScript) GenerateImports(file *generator.FileDescriptor) {
	if len(file.FileDescriptorProto.Service) == 0 {
		return
	}
	ts.P("import (")
	ts.P(")")
	ts.P()
}

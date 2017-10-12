package main

import (
	pb "github.com/golang/protobuf/protoc-gen-go/descriptor"
	"github.com/golang/protobuf/protoc-gen-go/generator"
)

// generateService generates code for the services in the given file.
func (ts *typeScript) generateService(file *generator.FileDescriptor, service *pb.ServiceDescriptorProto, index int) {
	origServName := service.GetName()
	fullServName := origServName
	if pkg := file.GetPackage(); pkg != "" {
		fullServName = pkg + "." + fullServName
	}
	servName := generator.CamelCase(origServName)

	ts.P()
	ts.P("// Client API for ", servName, " service")
	ts.P()

	for i, method := range service.Method {
		ts.P(ts.generateClientSignature(servName, method))
	}
	ts.P("}")
	ts.P()
}

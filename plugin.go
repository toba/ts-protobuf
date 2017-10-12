// Go support for Protocol Buffers - Google's data interchange format
//
// Copyright 2015 The Go Authors.  All rights reserved.
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

// Package grpc outputs gRPC service descriptions in Go code.
// It runs as a plugin for the Go protocol buffer compiler plugin.
// It is linked in to protoc-gen-go.
package main

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	pb "github.com/golang/protobuf/protoc-gen-go/descriptor"
	"github.com/golang/protobuf/protoc-gen-go/generator"
)

// generatedCodeVersion indicates a version of the generated code.
// It is incremented whenever an incompatibility between the generated code and
// the grpc package is introduced; the generated code references
// a constant, grpc.SupportPackageIsVersionN (where N is generatedCodeVersion).
const generatedCodeVersion = 4

// Paths for packages used by code generated in this file,
// relative to the import_prefix of the generator.Generator.
const (
	contextPkgPath = "golang.org/x/net/context"
	grpcPkgPath    = "google.golang.org/grpc"
)

func init() {
	generator.RegisterPlugin(new(typeScript))
}

// typeScript is an implementation of the Go protocol buffer compiler's
// plugin architecture. It generates bindings for gRPC support.
type typeScript struct {
	gen *generator.Generator
}

// Name returns the name of this plugin.
func (ts *typeScript) Name() string {
	return "TypeScript"
}

// The names for packages imported in the generated code.
// They may vary from the final path component of the import path
// if the name is used by other packages.
var (
	contextPkg string
	grpcPkg    string
)

// Init initializes the plugin.
func (ts *typeScript) Init(gen *generator.Generator) {
	ts.gen = gen
	contextPkg = generator.RegisterUniquePackageName("context", nil)
	grpcPkg = generator.RegisterUniquePackageName("grpc", nil)
}

// Given a type name defined in a .proto, return its object.
// Also record that we're using it, to guarantee the associated import.
func (ts *typeScript) objectNamed(name string) generator.Object {
	ts.gen.RecordTypeUse(name)
	return ts.gen.ObjectNamed(name)
}

// Given a type name defined in a .proto, return its name as we will print it.
func (ts *typeScript) typeName(str string) string {
	return ts.gen.TypeName(ts.objectNamed(str))
}

// P forwards to g.gen.P.
func (ts *typeScript) P(args ...interface{}) { ts.gen.P(args...) }

// Generate generates code for the services in the given file.
func (ts *typeScript) Generate(file *generator.FileDescriptor) {
	if len(file.FileDescriptorProto.Service) == 0 {
		return
	}

	ts.P("// Reference imports to suppress errors if they are not otherwise used.")
	ts.P("var _ ", contextPkg, ".Context")
	ts.P("var _ ", grpcPkg, ".ClientConn")
	ts.P()

	// Assert version compatibility.
	ts.P("// This is a compile-time assertion to ensure that this generated file")
	ts.P("// is compatible with the grpc package it is being compiled against.")
	ts.P("const _ = ", grpcPkg, ".SupportPackageIsVersion", generatedCodeVersion)
	ts.P()

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
	ts.P(contextPkg, " ", strconv.Quote(path.Join(ts.gen.ImportPrefix, contextPkgPath)))
	ts.P(grpcPkg, " ", strconv.Quote(path.Join(ts.gen.ImportPrefix, grpcPkgPath)))
	ts.P(")")
	ts.P()
}

// reservedClientName records whether a client name is reserved on the client side.
var reservedClientName = map[string]bool{
// TODO: do we need any in gRPC?
}

func unexport(s string) string { return strings.ToLower(s[:1]) + s[1:] }

// generateService generates all the code for the named service.
func (ts *typeScript) generateService(file *generator.FileDescriptor, service *pb.ServiceDescriptorProto, index int) {
	path := fmt.Sprintf("6,%d", index) // 6 means service.

	origServName := service.GetName()
	fullServName := origServName
	if pkg := file.GetPackage(); pkg != "" {
		fullServName = pkg + "." + fullServName
	}
	servName := generator.CamelCase(origServName)

	ts.P()
	ts.P("// Client API for ", servName, " service")
	ts.P()

	// Client interface.
	ts.P("type ", servName, "Client interface {")
	for i, method := range service.Method {
		ts.gen.PrintComments(fmt.Sprintf("%s,2,%d", path, i)) // 2 means method in a service.
		ts.P(ts.generateClientSignature(servName, method))
	}
	ts.P("}")
	ts.P()

	// Client structure.
	ts.P("type ", unexport(servName), "Client struct {")
	ts.P("cc *", grpcPkg, ".ClientConn")
	ts.P("}")
	ts.P()

	// NewClient factory.
	ts.P("func New", servName, "Client (cc *", grpcPkg, ".ClientConn) ", servName, "Client {")
	ts.P("return &", unexport(servName), "Client{cc}")
	ts.P("}")
	ts.P()

	var methodIndex, streamIndex int
	serviceDescVar := "_" + servName + "_serviceDesc"
	// Client method implementations.
	for _, method := range service.Method {
		var descExpr string
		if !method.GetServerStreaming() && !method.GetClientStreaming() {
			// Unary RPC method
			descExpr = fmt.Sprintf("&%s.Methods[%d]", serviceDescVar, methodIndex)
			methodIndex++
		} else {
			// Streaming RPC method
			descExpr = fmt.Sprintf("&%s.Streams[%d]", serviceDescVar, streamIndex)
			streamIndex++
		}
		ts.generateClientMethod(servName, fullServName, serviceDescVar, method, descExpr)
	}

	ts.P("// Server API for ", servName, " service")
	ts.P()

	// Server interface.
	serverType := servName + "Server"
	ts.P("type ", serverType, " interface {")
	for i, method := range service.Method {
		ts.gen.PrintComments(fmt.Sprintf("%s,2,%d", path, i)) // 2 means method in a service.
		ts.P(ts.generateServerSignature(servName, method))
	}
	ts.P("}")
	ts.P()

	// Server registration.
	ts.P("func Register", servName, "Server(s *", grpcPkg, ".Server, srv ", serverType, ") {")
	ts.P("s.RegisterService(&", serviceDescVar, `, srv)`)
	ts.P("}")
	ts.P()

	// Server handler implementations.
	var handlerNames []string
	for _, method := range service.Method {
		hname := ts.generateServerMethod(servName, fullServName, method)
		handlerNames = append(handlerNames, hname)
	}

	// Service descriptor.
	ts.P("var ", serviceDescVar, " = ", grpcPkg, ".ServiceDesc {")
	ts.P("ServiceName: ", strconv.Quote(fullServName), ",")
	ts.P("HandlerType: (*", serverType, ")(nil),")
	ts.P("Methods: []", grpcPkg, ".MethodDesc{")
	for i, method := range service.Method {
		if method.GetServerStreaming() || method.GetClientStreaming() {
			continue
		}
		ts.P("{")
		ts.P("MethodName: ", strconv.Quote(method.GetName()), ",")
		ts.P("Handler: ", handlerNames[i], ",")
		ts.P("},")
	}
	ts.P("},")
	ts.P("Streams: []", grpcPkg, ".StreamDesc{")
	for i, method := range service.Method {
		if !method.GetServerStreaming() && !method.GetClientStreaming() {
			continue
		}
		ts.P("{")
		ts.P("StreamName: ", strconv.Quote(method.GetName()), ",")
		ts.P("Handler: ", handlerNames[i], ",")
		if method.GetServerStreaming() {
			ts.P("ServerStreams: true,")
		}
		if method.GetClientStreaming() {
			ts.P("ClientStreams: true,")
		}
		ts.P("},")
	}
	ts.P("},")
	ts.P("Metadata: \"", file.GetName(), "\",")
	ts.P("}")
	ts.P()
}

// generateClientSignature returns the client-side signature for a method.
func (ts *typeScript) generateClientSignature(servName string, method *pb.MethodDescriptorProto) string {
	origMethName := method.GetName()
	methName := generator.CamelCase(origMethName)
	if reservedClientName[methName] {
		methName += "_"
	}
	reqArg := ", in *" + ts.typeName(method.GetInputType())
	if method.GetClientStreaming() {
		reqArg = ""
	}
	respName := "*" + ts.typeName(method.GetOutputType())
	if method.GetServerStreaming() || method.GetClientStreaming() {
		respName = servName + "_" + generator.CamelCase(origMethName) + "Client"
	}
	return fmt.Sprintf("%s(ctx %s.Context%s, opts ...%s.CallOption) (%s, error)", methName, contextPkg, reqArg, grpcPkg, respName)
}

func (ts *typeScript) generateClientMethod(servName, fullServName, serviceDescVar string, method *pb.MethodDescriptorProto, descExpr string) {
	sname := fmt.Sprintf("/%s/%s", fullServName, method.GetName())
	methName := generator.CamelCase(method.GetName())
	inType := ts.typeName(method.GetInputType())
	outType := ts.typeName(method.GetOutputType())

	ts.P("func (c *", unexport(servName), "Client) ", ts.generateClientSignature(servName, method), "{")
	if !method.GetServerStreaming() && !method.GetClientStreaming() {
		ts.P("out := new(", outType, ")")
		// TODO: Pass descExpr to Invoke.
		ts.P("err := ", grpcPkg, `.Invoke(ctx, "`, sname, `", in, out, c.cc, opts...)`)
		ts.P("if err != nil { return nil, err }")
		ts.P("return out, nil")
		ts.P("}")
		ts.P()
		return
	}
	streamType := unexport(servName) + methName + "Client"
	ts.P("stream, err := ", grpcPkg, ".NewClientStream(ctx, ", descExpr, `, c.cc, "`, sname, `", opts...)`)
	ts.P("if err != nil { return nil, err }")
	ts.P("x := &", streamType, "{stream}")
	if !method.GetClientStreaming() {
		ts.P("if err := x.ClientStream.SendMsg(in); err != nil { return nil, err }")
		ts.P("if err := x.ClientStream.CloseSend(); err != nil { return nil, err }")
	}
	ts.P("return x, nil")
	ts.P("}")
	ts.P()

	genSend := method.GetClientStreaming()
	genRecv := method.GetServerStreaming()
	genCloseAndRecv := !method.GetServerStreaming()

	// Stream auxiliary types and methods.
	ts.P("type ", servName, "_", methName, "Client interface {")
	if genSend {
		ts.P("Send(*", inType, ") error")
	}
	if genRecv {
		ts.P("Recv() (*", outType, ", error)")
	}
	if genCloseAndRecv {
		ts.P("CloseAndRecv() (*", outType, ", error)")
	}
	ts.P(grpcPkg, ".ClientStream")
	ts.P("}")
	ts.P()

	ts.P("type ", streamType, " struct {")
	ts.P(grpcPkg, ".ClientStream")
	ts.P("}")
	ts.P()

	if genSend {
		ts.P("func (x *", streamType, ") Send(m *", inType, ") error {")
		ts.P("return x.ClientStream.SendMsg(m)")
		ts.P("}")
		ts.P()
	}
	if genRecv {
		ts.P("func (x *", streamType, ") Recv() (*", outType, ", error) {")
		ts.P("m := new(", outType, ")")
		ts.P("if err := x.ClientStream.RecvMsg(m); err != nil { return nil, err }")
		ts.P("return m, nil")
		ts.P("}")
		ts.P()
	}
	if genCloseAndRecv {
		ts.P("func (x *", streamType, ") CloseAndRecv() (*", outType, ", error) {")
		ts.P("if err := x.ClientStream.CloseSend(); err != nil { return nil, err }")
		ts.P("m := new(", outType, ")")
		ts.P("if err := x.ClientStream.RecvMsg(m); err != nil { return nil, err }")
		ts.P("return m, nil")
		ts.P("}")
		ts.P()
	}
}

// generateServerSignature returns the server-side signature for a method.
func (ts *typeScript) generateServerSignature(servName string, method *pb.MethodDescriptorProto) string {
	origMethName := method.GetName()
	methName := generator.CamelCase(origMethName)
	if reservedClientName[methName] {
		methName += "_"
	}

	var reqArgs []string
	ret := "error"
	if !method.GetServerStreaming() && !method.GetClientStreaming() {
		reqArgs = append(reqArgs, contextPkg+".Context")
		ret = "(*" + ts.typeName(method.GetOutputType()) + ", error)"
	}
	if !method.GetClientStreaming() {
		reqArgs = append(reqArgs, "*"+ts.typeName(method.GetInputType()))
	}
	if method.GetServerStreaming() || method.GetClientStreaming() {
		reqArgs = append(reqArgs, servName+"_"+generator.CamelCase(origMethName)+"Server")
	}

	return methName + "(" + strings.Join(reqArgs, ", ") + ") " + ret
}

func (ts *typeScript) generateServerMethod(servName, fullServName string, method *pb.MethodDescriptorProto) string {
	methName := generator.CamelCase(method.GetName())
	hname := fmt.Sprintf("_%s_%s_Handler", servName, methName)
	inType := ts.typeName(method.GetInputType())
	outType := ts.typeName(method.GetOutputType())

	if !method.GetServerStreaming() && !method.GetClientStreaming() {
		ts.P("func ", hname, "(srv interface{}, ctx ", contextPkg, ".Context, dec func(interface{}) error, interceptor ", grpcPkg, ".UnaryServerInterceptor) (interface{}, error) {")
		ts.P("in := new(", inType, ")")
		ts.P("if err := dec(in); err != nil { return nil, err }")
		ts.P("if interceptor == nil { return srv.(", servName, "Server).", methName, "(ctx, in) }")
		ts.P("info := &", grpcPkg, ".UnaryServerInfo{")
		ts.P("Server: srv,")
		ts.P("FullMethod: ", strconv.Quote(fmt.Sprintf("/%s/%s", fullServName, methName)), ",")
		ts.P("}")
		ts.P("handler := func(ctx ", contextPkg, ".Context, req interface{}) (interface{}, error) {")
		ts.P("return srv.(", servName, "Server).", methName, "(ctx, req.(*", inType, "))")
		ts.P("}")
		ts.P("return interceptor(ctx, in, info, handler)")
		ts.P("}")
		ts.P()
		return hname
	}
	streamType := unexport(servName) + methName + "Server"
	ts.P("func ", hname, "(srv interface{}, stream ", grpcPkg, ".ServerStream) error {")
	if !method.GetClientStreaming() {
		ts.P("m := new(", inType, ")")
		ts.P("if err := stream.RecvMsg(m); err != nil { return err }")
		ts.P("return srv.(", servName, "Server).", methName, "(m, &", streamType, "{stream})")
	} else {
		ts.P("return srv.(", servName, "Server).", methName, "(&", streamType, "{stream})")
	}
	ts.P("}")
	ts.P()

	genSend := method.GetServerStreaming()
	genSendAndClose := !method.GetServerStreaming()
	genRecv := method.GetClientStreaming()

	// Stream auxiliary types and methods.
	ts.P("type ", servName, "_", methName, "Server interface {")
	if genSend {
		ts.P("Send(*", outType, ") error")
	}
	if genSendAndClose {
		ts.P("SendAndClose(*", outType, ") error")
	}
	if genRecv {
		ts.P("Recv() (*", inType, ", error)")
	}
	ts.P(grpcPkg, ".ServerStream")
	ts.P("}")
	ts.P()

	ts.P("type ", streamType, " struct {")
	ts.P(grpcPkg, ".ServerStream")
	ts.P("}")
	ts.P()

	if genSend {
		ts.P("func (x *", streamType, ") Send(m *", outType, ") error {")
		ts.P("return x.ServerStream.SendMsg(m)")
		ts.P("}")
		ts.P()
	}
	if genSendAndClose {
		ts.P("func (x *", streamType, ") SendAndClose(m *", outType, ") error {")
		ts.P("return x.ServerStream.SendMsg(m)")
		ts.P("}")
		ts.P()
	}
	if genRecv {
		ts.P("func (x *", streamType, ") Recv() (*", inType, ", error) {")
		ts.P("m := new(", inType, ")")
		ts.P("if err := x.ServerStream.RecvMsg(m); err != nil { return nil, err }")
		ts.P("return m, nil")
		ts.P("}")
		ts.P()
	}

	return hname
}

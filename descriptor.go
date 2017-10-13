package main

import "github.com/golang/protobuf/protoc-gen-go/descriptor"

type (
	// common is embedded in all descriptors.
	// Each type we import as a protocol buffer (other than FileDescriptorProto) needs
	// a pointer to the FileDescriptorProto that represents it. These types achieve that
	// wrapping by placing each Proto inside a struct with the pointer to its File. The
	// structs have the same names as their contents, with "Proto" removed.
	// FileDescriptor is used to store the things that it points to.
	common struct {
		file *descriptor.FileDescriptorProto // File this object comes from.
	}

	// ProtoObject is an interface abstracting the abilities shared by enums,
	// messages, extensions and imported objects.
	ProtoObject interface {
		PackageName() string // The name we use in our output (a_b_c), possibly renamed for uniqueness.
		TypeName() []string
		File() *descriptor.FileDescriptorProto
	}
)

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

// PackageName is name in the package clause in the generated file.
func (c *common) PackageName() string {
	return uniquePackageOf(c.file)
}

func (c *common) File() *descriptor.FileDescriptorProto {
	return c.file
}

func (c *common) proto3() bool {
	return fileIsProto3(c.file)
}

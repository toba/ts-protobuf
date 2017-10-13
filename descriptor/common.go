package descriptor

import proto "github.com/golang/protobuf/protoc-gen-go/descriptor"

type (
	// The file and package name method are common to messages and enums.
	common struct {
		file *proto.FileDescriptorProto // File this object comes from.
	}

	// Object is an interface abstracting the abilities shared by enums,
	// messages, extensions and imported objects.
	Object interface {
		PackageName() string // The name we use in our output (a_b_c), possibly renamed for uniqueness.
		TypeName() []string
		File() *proto.FileDescriptorProto
	}
)

// PackageName is name in the package clause in the generated file.
func (c *common) PackageName() string {
	return uniquePackageOf(c.file)
}

func (c *common) File() *proto.FileDescriptorProto {
	return c.file
}

func (c *common) proto3() bool {
	return fileIsProto3(c.file)
}

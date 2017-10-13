package descriptor

import (
	"fmt"
	"log"

	proto "github.com/golang/protobuf/protoc-gen-go/descriptor"
)

// EnumDescriptor describes an enum. If it's at top level, its parent will be
// nil. Otherwise it will be the descriptor of the message in which it is
// defined.
type EnumDescriptor struct {
	common
	*proto.EnumDescriptorProto
	message   *MessageDescriptor // The containing message, if any.
	typeNames []string           // Cached typename vector.
	index     int                // The index into the container, whether the file or a message.
	path      string             // The SourceCodeInfo path as comma-separated integers.
}

// newEnum constructs an EnumDescriptor.
func newEnum(desc *proto.EnumDescriptorProto, msg *MessageDescriptor, file *proto.FileDescriptorProto, index int) *EnumDescriptor {
	ed := &EnumDescriptor{
		common:              common{file},
		EnumDescriptorProto: desc,
		message:             msg,
		index:               index,
	}
	if msg == nil {
		ed.path = fmt.Sprintf("%d,%d", enumPath, index)
	} else {
		ed.path = fmt.Sprintf("%s,%d,%d", msg.path, messageEnumPath, index)
	}
	return ed
}

// wrapEnums builds a slice of EnumDescriptors defined within a file.
func wrapEnums(file *proto.FileDescriptorProto, descs []*MessageDescriptor) []*EnumDescriptor {
	sl := make([]*EnumDescriptor, 0, len(file.EnumType)+10)
	// Top-level enums.
	for i, enum := range file.EnumType {
		sl = append(sl, newEnum(enum, nil, file, i))
	}
	// Enums within messages. Enums within embedded messages appear in the outer-most message.
	for _, nested := range descs {
		for i, enum := range nested.EnumType {
			sl = append(sl, newEnum(enum, nested, file, i))
		}
	}
	return sl
}

// TypeName returns the elements of the dotted type name.
// The package name is not part of this name.
func (e *EnumDescriptor) TypeName() (s []string) {
	if e.typeNames != nil {
		return e.typeNames
	}
	name := e.GetName()
	if e.message == nil {
		s = make([]string, 1)
	} else {
		pname := e.message.TypeName()
		s = make([]string, len(pname)+1)
		copy(s, pname)
	}
	s[len(s)-1] = name
	e.typeNames = s
	return s
}

// Everything but the last element of the full type name, CamelCased.
// The values of type Foo.Bar are call Foo_value1... not Foo_Bar_value1... .
func (e *EnumDescriptor) prefix() string {
	if e.message == nil {
		// If the enum is not part of a message, the prefix is just the type name.
		return CamelCase(*e.Name) + "_"
	}
	typeName := e.TypeName()
	return CamelCaseSlice(typeName[0:len(typeName)-1]) + "_"
}

// The integer value of the named constant in this enumerated type.
func (e *EnumDescriptor) integerValueAsString(name string) string {
	for _, c := range e.Value {
		if c.GetName() == name {
			return fmt.Sprint(c.GetNumber())
		}
	}
	log.Fatal("cannot find value for enum constant")
	return ""
}

package descriptor

import (
	"fmt"
	"strings"

	proto "github.com/golang/protobuf/protoc-gen-go/descriptor"
)

// Descriptor represents a protocol buffer message.
type MessageDescriptor struct {
	common
	*proto.DescriptorProto
	parent     *MessageDescriptor     // The containing message, if any.
	nested     []*MessageDescriptor   // Inner messages, if any.
	enums      []*EnumDescriptor      // Inner enums, if any.
	extensions []*ExtensionDescriptor // Extensions, if any.
	typename   []string               // Cached typename vector.
	index      int                    // The index into the container, whether the file or another message.
	path       string                 // The SourceCodeInfo path as comma-separated integers.
	group      bool
}

func newMessage(desc *proto.DescriptorProto, parent *MessageDescriptor, file *proto.FileDescriptorProto, index int) *MessageDescriptor {
	d := &MessageDescriptor{
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
			if field.GetType() == proto.FieldDescriptorProto_TYPE_GROUP && field.GetTypeName() == exp {
				d.group = true
				break
			}
		}
	}

	for _, field := range desc.Extension {
		d.extensions = append(d.extensions, &ExtensionDescriptor{common{file}, field, d})
	}

	return d
}

// Return a slice of all the Descriptors defined within this file
func wrapMessages(file *proto.FileDescriptorProto) []*MessageDescriptor {
	sl := make([]*MessageDescriptor, 0, len(file.MessageType)+10)
	for i, desc := range file.MessageType {
		sl = wrapThisDescriptor(sl, desc, nil, file, i)
	}
	return sl
}

// Wrap this Descriptor, recursively
func wrapThisDescriptor(sl []*MessageDescriptor, desc *proto.DescriptorProto, parent *MessageDescriptor, file *proto.FileDescriptorProto, index int) []*MessageDescriptor {
	sl = append(sl, newMessage(desc, parent, file, index))
	me := sl[len(sl)-1]
	for i, nested := range desc.NestedType {
		sl = wrapThisDescriptor(sl, nested, me, file, i)
	}
	return sl
}

// TypeName returns the elements of the dotted type name. The package name is
// not part of this name.
func (d *MessageDescriptor) TypeName() []string {
	if d.typename != nil {
		return d.typename
	}
	n := 0
	for parent := d; parent != nil; parent = parent.parent {
		n++
	}
	s := make([]string, n, n)
	for parent := d; parent != nil; parent = parent.parent {
		n--
		s[n] = parent.GetName()
	}
	d.typename = s
	return s
}

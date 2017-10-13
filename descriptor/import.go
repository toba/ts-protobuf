package descriptor

// ImportedDescriptor describes a type that has been publicly imported from
// another file.
type ImportedDescriptor struct {
	common
	o ProtoObject
}

func (id *ImportedDescriptor) TypeName() []string {
	return id.o.TypeName()
}

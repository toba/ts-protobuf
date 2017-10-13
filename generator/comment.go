package generator

import (
	"strconv"
	"strings"

	proto "github.com/golang/protobuf/protoc-gen-go/descriptor"
	"github.com/toba/ts-protobuf/descriptor"
)

func extractComments(file *descriptor.FileDescriptor) {
	file.comments = make(map[string]*proto.SourceCodeInfo_Location)
	for _, loc := range file.GetSourceCodeInfo().GetLocation() {
		if loc.LeadingComments == nil {
			continue
		}
		var p []string
		for _, n := range loc.Path {
			p = append(p, strconv.Itoa(int(n)))
		}
		file.comments[strings.Join(p, ",")] = loc
	}
}

// PrintComments prints any comments from the source .proto file.
// The path is a comma-separated list of integers.
// It returns an indication of whether any comments were printed.
// See descriptor.proto for its format.
func (g *Generator) PrintComments(path string) bool {
	if !g.writeOutput {
		return false
	}
	if loc, ok := g.file.comments[path]; ok {
		text := strings.TrimSuffix(loc.GetLeadingComments(), "\n")
		for _, line := range strings.Split(text, "\n") {
			g.P("// ", strings.TrimPrefix(line, " "))
		}
		return true
	}
	return false
}

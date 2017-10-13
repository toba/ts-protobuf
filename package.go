package generator

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/toba/ts-protobuf/descriptor"
)

var (
	isTypeScriptKeyword = map[string]bool{
		"break":     true,
		"case":      true,
		"const":     true,
		"continue":  true,
		"default":   true,
		"else":      true,
		"for":       true,
		"function":  true,
		"goto":      true,
		"if":        true,
		"import":    true,
		"interface": true,
		"map":       true,
		"return":    true,
		"select":    true,
		"switch":    true,
		"type":      true,
		"var":       true,
	}

	// For each input file, the unique package name to use, underscored.
	uniquePackageName = make(map[*descriptor.FileDescriptorProto]string)

	// Package names already registered.  Key is the name from the .proto file;
	// value is the name that appears in the generated code.
	pkgNamesInUse = make(map[string]bool)
)

// Create and remember a guaranteed unique package name for this file descriptor.
// Pkg is the candidate name.  If f is nil, it's a builtin package like "proto" and
// has no file descriptor.
func RegisterUniquePackageName(pkg string, f *descriptor.FileDescriptor) string {
	// Convert dots to underscores before finding a unique alias.
	pkg = strings.Map(badToUnderscore, pkg)

	for i, orig := 1, pkg; pkgNamesInUse[pkg]; i++ {
		// It's a duplicate; must rename.
		pkg = orig + strconv.Itoa(i)
	}
	// Install it.
	pkgNamesInUse[pkg] = true
	if f != nil {
		uniquePackageName[f.FileDescriptorProto] = pkg
	}
	return pkg
}

// DefaultPackageName returns the package name printed for the object. If its
// file is in a different package, it returns the package name we're using for
// this file, plus ".". Otherwise it returns the empty string.
func (g *Generator) DefaultPackageName(obj Object) string {
	pkg := obj.PackageName()
	if pkg == g.packageName {
		return ""
	}
	return pkg + "."
}

// defaultGoPackage returns the package name to use, derived from the import
// path of the package we're building code for.
func (g *Generator) defaultGoPackage() string {
	p := g.PackageImportPath
	if i := strings.LastIndex(p, "/"); i >= 0 {
		p = p[i+1:]
	}
	if p == "" {
		return ""
	}

	p = strings.Map(badToUnderscore, p)
	// Identifier must not be keyword: insert _.
	if isTypeScriptKeyword[p] {
		p = "_" + p
	}
	// Identifier must not begin with digit: insert _.
	if r, _ := utf8.DecodeRuneInString(p); unicode.IsDigit(r) {
		p = "_" + p
	}
	return p
}

// SetPackageNames sets the package name for this run.
// The package name must agree across all files being generated.
// It also defines unique package names for all imported files.
func (g *Generator) SetPackageNames() {
	// Register the name for this package.  It will be the first name
	// registered so is guaranteed to be unmodified.
	pkg, explicit := g.genFiles[0].goPackageName()

	// Check all files for an explicit go_package option.
	for _, f := range g.genFiles {
		thisPkg, thisExplicit := f.goPackageName()
		if thisExplicit {
			if !explicit {
				// Let this file's go_package option serve for all input files.
				pkg, explicit = thisPkg, true
			} else if thisPkg != pkg {
				g.Fail("inconsistent package names:", thisPkg, pkg)
			}
		}
	}

	// If we don't have an explicit go_package option but we have an
	// import path, use that.
	if !explicit {
		p := g.defaultGoPackage()
		if p != "" {
			pkg, explicit = p, true
		}
	}

	// If there was no go_package and no import path to use,
	// double-check that all the inputs have the same implicit
	// Go package name.
	if !explicit {
		for _, f := range g.genFiles {
			thisPkg, _ := f.goPackageName()
			if thisPkg != pkg {
				g.Fail("inconsistent package names:", thisPkg, pkg)
			}
		}
	}

	g.packageName = RegisterUniquePackageName(pkg, g.genFiles[0])

	// Register the support package names. They might collide with the
	// name of a package we import.
	g.Pkg = map[string]string{
		"fmt":   RegisterUniquePackageName("fmt", nil),
		"math":  RegisterUniquePackageName("math", nil),
		"proto": RegisterUniquePackageName("proto", nil),
	}

AllFiles:
	for _, f := range g.allFiles {
		for _, genf := range g.genFiles {
			if f == genf {
				// In this package already.
				uniquePackageName[f.FileDescriptorProto] = g.packageName
				continue AllFiles
			}
		}
		// The file is a dependency, so we want to ignore its go_package option
		// because that is only relevant for its specific generated output.
		pkg := f.GetPackage()
		if pkg == "" {
			pkg = baseName(*f.Name)
		}
		RegisterUniquePackageName(pkg, f)
	}
}

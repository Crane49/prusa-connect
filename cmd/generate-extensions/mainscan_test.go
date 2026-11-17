package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// registryPackages are the packages that own a registry. A call to Register on
// anything else, prometheus among them, is not a registration this reference
// publishes.
var registryPackages = []string{
	modulePath + "/sdk/connectorapi",
	modulePath + "/sdk/storageapi",
	modulePath + "/sdk/authapi",
	modulePath + "/sdk/eventsapi",
	modulePath + "/sdk/extension",
	modulePath + "/internal/extension/registry",
}

// registrationCalls are the ways a registry is written to.
var registrationCalls = []string{"Register", "TryRegister", "RegisterFactory"}

// registrationsInMain parses one directory and reports whether it is a main
// package and where in it an extension looks like it is being registered.
//
// Build tags are ignored on purpose. A registration behind `//go:build sqlite`
// is still a registration in the binary that tag ships in, and a main package
// has no configuration in which an unreachable entry becomes reachable.
func registrationsInMain(dir string) (bool, []string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, nil, err
	}

	fset := token.NewFileSet()
	isMain := false
	registryNames := map[string]bool{}

	var files []*ast.File

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, parseErr := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return false, nil, parseErr
		}

		if file.Name.Name != "main" {
			continue
		}

		isMain = true

		files = append(files, file)

		for _, spec := range file.Imports {
			path, unquoteErr := strconv.Unquote(spec.Path.Value)
			if unquoteErr != nil {
				continue
			}

			if slices.Contains(registryPackages, path) {
				registryNames[localName(spec, path)] = true
			}
		}
	}

	if !isMain {
		return false, nil, nil
	}

	offences := scanFiles(fset, files, registryNames)
	sort.Strings(offences)

	return true, offences, nil
}

func scanFiles(fset *token.FileSet, files []*ast.File, registryNames map[string]bool) []string {
	var offences []string

	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !slices.Contains(registrationCalls, sel.Sel.Name) {
				return true
			}

			if !registryNames[rootIdent(sel.X)] {
				return true
			}

			offences = append(offences, fset.Position(call.Pos()).String())

			return true
		})
	}

	return offences
}

// localName is the name an import is spelled with in the file that declares it.
// An alias is the cheapest way to defeat a source scan that matches a package
// name.
func localName(spec *ast.ImportSpec, path string) string {
	if spec.Name != nil {
		return spec.Name.Name
	}

	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}

	return path
}

// rootIdent is the leftmost identifier of a selector chain, so
// `registry.Register[T]` roots at `registry`.
func rootIdent(expr ast.Expr) string {
	for {
		switch node := expr.(type) {
		case *ast.Ident:
			return node.Name
		case *ast.SelectorExpr:
			expr = node.X
		case *ast.CallExpr:
			expr = node.Fun
		case *ast.IndexExpr:
			expr = node.X
		case *ast.IndexListExpr:
			expr = node.X
		case *ast.ParenExpr:
			expr = node.X
		case *ast.StarExpr:
			expr = node.X
		default:
			return ""
		}
	}
}

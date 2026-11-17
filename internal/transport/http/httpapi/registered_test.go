package httpapi

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// registeredFloor is the smallest number of operations this package may declare
// before the gate below stops believing itself. A scan that reads none, or
// almost none, has found a source layout it cannot parse rather than a package
// with nothing in it.
const registeredFloor = 30

// operationIdentity is one operation as both sides name it: the identifier a
// generated client binds to, and the route it is reached at.
type operationIdentity struct {
	ID     string
	Method string
	Path   string
}

func (o operationIdentity) String() string { return o.ID + " (" + o.Method + " " + o.Path + ")" }

// TestTheCommittedDocumentPublishesEveryRegisteredOperation is the gate over the
// layer TestCommittedSpecMatchesTheCode cannot reach. That one compares the
// document against a server a test built, so an operation whose registration is
// conditional is absent from both sides and the comparison is silent about it.
// registerCameras returns without registering when Config.Cameras is nil, so
// its three operations reach no baseline and no gate unless the server the
// document is generated from wires a fleet. tools/apicompat reads the committed
// document as the current surface, so an operation missing from it can be
// removed, renamed or given a required field while make api-check compares two
// documents that both lack it.
//
// This reads the operations out of the package's own source and requires the
// committed document to publish exactly them. It is by identity rather than by
// count, because 41 against 41 with one operation swapped for another is a
// count that agrees and a surface that changed.
func TestTheCommittedDocumentPublishesEveryRegisteredOperation(t *testing.T) {
	t.Parallel()

	declared := operationsInSource(t)
	require.GreaterOrEqualf(t, len(declared), registeredFloor,
		"the source scan found %d operations, so this gate is reading nothing", len(declared))

	published := operationsInCommittedDocument(t)

	require.ElementsMatch(t, declared, published,
		"the committed document and the registered operations disagree.\n"+
			"An operation missing from the document is in no baseline and in no gate, so a breaking "+
			"change to it is invisible to make api-check.\n"+
			"Wire whatever registers it into serverWith, then regenerate with:\n"+
			"  make generate-openapi")
}

// operationsInSource reads every huma.Operation this package declares.
//
// The source is the honest side of the comparison. A server built from a Config
// only registers what that Config wired, which is the thing being checked, so a
// second construction would report the same omission as agreement.
func operationsInSource(t *testing.T) []operationIdentity {
	t.Helper()

	names, err := filepath.Glob("*.go")
	require.NoError(t, err)

	fset := token.NewFileSet()
	files := map[string]*ast.File{}

	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}

		parsed, parseErr := parser.ParseFile(fset, name, nil, 0)
		require.NoErrorf(t, parseErr, "parse %s", name)

		files[name] = parsed
	}

	require.NotEmpty(t, files, "the package source parsed to nothing")

	paths := stringConstants(files)

	var found []operationIdentity

	for name, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			literal, isLiteral := node.(*ast.CompositeLit)
			if !isLiteral || !isHumaOperation(literal.Type) {
				return true
			}

			found = append(found, identityOf(t, name, literal, paths))

			return true
		})
	}

	return found
}

// isHumaOperation reports whether a composite literal is the operation
// descriptor. The package builds http.Cookie, huma.Response and several
// documentation records with a Path field of their own, and each one would
// otherwise read as an operation.
func isHumaOperation(expr ast.Expr) bool {
	selector, isSelector := expr.(*ast.SelectorExpr)
	if !isSelector || selector.Sel.Name != "Operation" {
		return false
	}

	pkg, isIdent := selector.X.(*ast.Ident)

	return isIdent && pkg.Name == "huma"
}

func identityOf(
	t *testing.T,
	file string,
	literal *ast.CompositeLit,
	paths map[string]string,
) operationIdentity {
	t.Helper()

	var found operationIdentity

	for _, element := range literal.Elts {
		field, keyed := element.(*ast.KeyValueExpr)
		if !keyed {
			continue
		}

		key, named := field.Key.(*ast.Ident)
		if !named {
			continue
		}

		switch key.Name {
		case "OperationID":
			found.ID = stringValue(t, file, field.Value, paths)
		case "Method":
			found.Method = methodValue(t, file, field.Value)
		case "Path":
			found.Path = stringValue(t, file, field.Value, paths)
		}
	}

	require.NotEmptyf(t, found.ID, "%s declares an operation with no OperationID", file)
	require.NotEmptyf(t, found.Method, "%s: %s declares no Method", file, found.ID)
	require.NotEmptyf(t, found.Path, "%s: %s declares no Path", file, found.ID)

	return found
}

// methodValue resolves http.MethodGet and its siblings. Huma writes the method
// into the document lowercased, and this returns it in the document's spelling
// so the two sides compare as they are written.
func methodValue(t *testing.T, file string, expr ast.Expr) string {
	t.Helper()

	selector, isSelector := expr.(*ast.SelectorExpr)
	require.Truef(t, isSelector, "%s: a Method that is not http.MethodX cannot be resolved here", file)

	pkg, isIdent := selector.X.(*ast.Ident)
	require.Truef(t, isIdent && pkg.Name == "http", "%s: Method comes from %s", file, selector.X)

	methods := map[string]string{
		"MethodGet": http.MethodGet, "MethodHead": http.MethodHead, "MethodPost": http.MethodPost,
		"MethodPut": http.MethodPut, "MethodPatch": http.MethodPatch, "MethodDelete": http.MethodDelete,
		"MethodConnect": http.MethodConnect, "MethodOptions": http.MethodOptions, "MethodTrace": http.MethodTrace,
	}

	method, known := methods[selector.Sel.Name]
	require.Truef(t, known, "%s: http.%s is not a method this gate knows", file, selector.Sel.Name)

	return strings.ToLower(method)
}

// stringValue resolves a string literal, a package-level string constant, or a
// concatenation of either. Every path in this package is one of the three, and
// an expression that is none of them fails rather than resolving to the empty
// string, because an operation this gate cannot name is an operation it cannot
// find missing.
func stringValue(t *testing.T, file string, expr ast.Expr, constants map[string]string) string {
	t.Helper()

	value, resolved := resolveString(expr, constants)
	require.Truef(t, resolved, "%s: %s is not a literal, a package constant or a concatenation of them",
		file, exprText(expr))

	return value
}

func resolveString(expr ast.Expr, constants map[string]string) (string, bool) {
	switch node := expr.(type) {
	case *ast.BasicLit:
		if node.Kind != token.STRING {
			return "", false
		}

		unquoted, err := strconv.Unquote(node.Value)

		return unquoted, err == nil
	case *ast.Ident:
		value, known := constants[node.Name]

		return value, known
	case *ast.BinaryExpr:
		if node.Op != token.ADD {
			return "", false
		}

		left, leftOK := resolveString(node.X, constants)
		right, rightOK := resolveString(node.Y, constants)

		return left + right, leftOK && rightOK
	default:
		return "", false
	}
}

// stringConstants is every package-level string constant, by name. It runs to a
// fixed point because a path constant is routinely built from another one, and
// the declaration order across files is not the resolution order.
func stringConstants(files map[string]*ast.File) map[string]string {
	values := map[string]string{}

	type pending struct {
		name string
		expr ast.Expr
	}

	var unresolved []pending

	for _, file := range files {
		for _, decl := range file.Decls {
			general, isGeneral := decl.(*ast.GenDecl)
			if !isGeneral || general.Tok != token.CONST {
				continue
			}

			for _, spec := range general.Specs {
				valueSpec, isValue := spec.(*ast.ValueSpec)
				if !isValue {
					continue
				}

				for i, name := range valueSpec.Names {
					if i < len(valueSpec.Values) {
						unresolved = append(unresolved, pending{name: name.Name, expr: valueSpec.Values[i]})
					}
				}
			}
		}
	}

	for progress := true; progress; {
		progress = false
		remaining := unresolved[:0]

		for _, item := range unresolved {
			value, resolved := resolveString(item.expr, values)
			if !resolved {
				remaining = append(remaining, item)

				continue
			}

			values[item.name] = value
			progress = true
		}

		unresolved = remaining
	}

	return values
}

// exprText renders an expression for a failure message. A position is enough to
// find it and the printer is not worth the import.
func exprText(expr ast.Expr) string {
	if ident, isIdent := expr.(*ast.Ident); isIdent {
		return ident.Name
	}

	return "the expression at this Path or OperationID"
}

// operationsInCommittedDocument reads the file on disk rather than the bytes a
// server rendered. It is the file `tools/apicompat` compares against and the
// file a client generates from, so it is the one that has to be complete.
func operationsInCommittedDocument(t *testing.T) []operationIdentity {
	t.Helper()

	raw, err := os.ReadFile(committedSpecPath)
	require.NoError(t, err, "read %s", committedSpecPath)

	// publishedPath drops the members of a path item that are not operations,
	// which a described path item such as the module file family carries.
	var document struct {
		Paths map[string]publishedPath `json:"paths"`
	}

	require.NoError(t, json.Unmarshal(raw, &document))

	var found []operationIdentity

	for path, item := range document.Paths {
		for method, op := range item {
			if op.OperationID == "" {
				continue
			}

			found = append(found, operationIdentity{ID: op.OperationID, Method: method, Path: path})
		}
	}

	return found
}

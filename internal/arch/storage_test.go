package arch_test

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// storageGuardedDirs hold the code that resolves a caller-supplied name against
// a store. Every file under them at any depth is scanned, so a provider added
// later is covered without anybody remembering this gate exists.
//
// The scan walks rather than reading each directory flat. adapters/storage
// holds no Go file of its own and every provider under it is a subdirectory, so
// a flat read contributes nothing from it and leaves the gate below green over
// a shipped adapter.
var storageGuardedDirs = []string{
	"adapters/storage",
}

// storageGuardedFiles hold a path that a provider produced and something else
// later deletes. They are named one by one because the packages around them do
// plenty of unrelated file work. The list is empty: the two files that held one
// went with the retired server, and a file named here is checked the moment one
// is added back.
var storageGuardedFiles = []string{}

// rootedOnly are the os calls that take a path and act on whatever it resolves
// to at that moment. Inside the guarded set they are replaced by a method on an
// *os.Root, which resolves the name under a directory handle instead.
var rootedOnly = map[string]bool{
	"Chdir": true, "Chmod": true, "Chown": true, "Chtimes": true,
	"Create": true, "CreateTemp": true, "DirFS": true, "Lchown": true,
	"Link": true, "Lstat": true, "Mkdir": true, "MkdirAll": true,
	"MkdirTemp": true, "Open": true, "OpenFile": true, "ReadDir": true,
	"ReadFile": true, "Readlink": true, "Remove": true, "RemoveAll": true,
	"Rename": true, "Stat": true, "Symlink": true, "Truncate": true,
	"WriteFile": true,
}

// resolvingWalks are the filepath calls that walk or resolve a path on disk
// rather than doing string arithmetic on it. filepath.Join, Dir, Base and Clean
// touch nothing and stay allowed.
var resolvingWalks = map[string]bool{
	"EvalSymlinks": true, "Glob": true, "Walk": true, "WalkDir": true,
}

// storageRootExceptions is the reviewed allow-list, keyed by "file#call". Each
// entry is a call that cannot go through a root because there is no root yet.
var storageRootExceptions = map[string]string{}

// A storage provider resolves a name a caller chose. Doing that with a path
// built by the process rather than through a directory handle is the escape
// the old backlog exists for: a link planted in the store makes the same name mean a file
// outside it, and neither Clean nor a prefix check can see one.
func TestStorageResolvesOnlyThroughARoot(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	files := storageGuardedGoFiles(t, root)

	requireScanned(t, "guarded storage Go files", len(files), 1)

	used := map[string]bool{}

	for _, rel := range files {
		for _, offence := range unrootedCalls(t, root, rel) {
			key := rel + "#" + offence.call
			if _, allowed := storageRootExceptions[key]; allowed {
				used[key] = true

				continue
			}

			t.Errorf("%s:%d calls %s, which resolves a path the operating system walks for it.\n"+
				"A link or a directory swapped in along that path redirects the call outside the store.\n"+
				"Open an *os.Root on the store and call the method on it, or use "+
				"storageapi.RemoveStaged for a name a provider handed back. See ADR-20260825-smx.",
				rel, offence.line, offence.call)
		}
	}

	for key, reason := range storageRootExceptions {
		if !used[key] {
			t.Errorf("storageRootExceptions lists %s (%s), which no longer calls it. Remove the entry.", key, reason)
		}
	}
}

// TestEveryGuardedStorageDirectoryIsScanned is the half the gate above cannot
// state about itself. Its own len(files) == 0 guard is satisfied by any one
// directory contributing, so a guarded directory that contributes nothing, by
// being absent or by holding its providers one level down, leaves that code
// unchecked while the gate reports green.
func TestEveryGuardedStorageDirectoryIsScanned(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	requireScanned(t, "guarded storage directories", len(storageGuardedDirs), 1)

	for _, dir := range storageGuardedDirs {
		t.Run(dir, func(t *testing.T) {
			t.Parallel()

			requireScanned(t, "Go files under "+dir, len(storageGoFilesUnder(t, root, dir)), 1,
				"Either the directory is gone and its entry in storageGuardedDirs belongs with it,\n"+
					"or the scan is not reaching it, and every path it resolves is unchecked.")
		})
	}
}

// storageGuardedGoFiles is every production Go file the gate covers, as paths
// relative to the repository root. Test files are excluded: the escape suites
// plant the links the production code has to refuse.
func storageGuardedGoFiles(t *testing.T, root string) []string {
	t.Helper()

	found := map[string]bool{}

	for _, dir := range storageGuardedDirs {
		for _, rel := range storageGoFilesUnder(t, root, dir) {
			found[rel] = true
		}
	}

	for _, rel := range storageGuardedFiles {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("the gate names %s, which is not there: %v", rel, err)
		}

		found[rel] = true
	}

	out := make([]string, 0, len(found))
	for rel := range found {
		out = append(out, rel)
	}

	sort.Strings(out)

	return out
}

// storageGoFilesUnder is every production Go file under one guarded directory,
// at any depth, as paths relative to the repository root. A directory that is
// not there contributes nothing rather than failing here, because
// TestEveryGuardedStorageDirectoryIsScanned is where that is reported.
func storageGoFilesUnder(t *testing.T, root, dir string) []string {
	t.Helper()

	base := filepath.Join(root, filepath.FromSlash(dir))

	var found []string

	err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			if entry.Name() == "testdata" {
				return fs.SkipDir
			}

			return nil
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		found = append(found, filepath.ToSlash(rel))

		return nil
	})

	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil {
		t.Fatalf("walking %s: %v", dir, err)
	}

	return found
}

// unrooted is one call the gate refuses, with the line it sits on.
type unrooted struct {
	call string
	line int
}

// unrootedCalls reports every path-resolving stdlib call in one file. The import
// name is resolved rather than assumed, so a file that renames the os import is
// still read correctly.
func unrootedCalls(t *testing.T, root, rel string) []unrooted {
	t.Helper()

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, filepath.Join(root, filepath.FromSlash(rel)), nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing %s: %v", rel, err)
	}

	refused := map[string]map[string]bool{}

	for _, spec := range file.Imports {
		imported, unquoteErr := strconv.Unquote(spec.Path.Value)
		if unquoteErr != nil {
			continue
		}

		var calls map[string]bool

		switch imported {
		case "os":
			calls = rootedOnly
		case "path/filepath":
			calls = resolvingWalks
		default:
			continue
		}

		local := imported[strings.LastIndex(imported, "/")+1:]
		if spec.Name != nil {
			local = spec.Name.Name
		}

		refused[local] = calls
	}

	var found []unrooted

	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		ident, ok := sel.X.(*ast.Ident)
		if !ok || !refused[ident.Name][sel.Sel.Name] {
			return true
		}

		found = append(found, unrooted{
			call: ident.Name + "." + sel.Sel.Name,
			line: fset.Position(call.Pos()).Line,
		})

		return true
	})

	return found
}

package resource

import (
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

// Two enabled modules must not read or draw files of one type. Only one of
// them would be used, and the operator would not have chosen which. A change
// that would leave two such modules enabled is refused, as Composer refuses
// two packages that conflict.

// The two uses of a file type a module declares.
const (
	// UseFileAnalysis is a module reading files of the type for the server.
	UseFileAnalysis = "file_analysis"
	// UseFileView is a module drawing what a file of the type holds.
	UseFileView = "file_view"
)

// EveryFileType is the type a file-view entry with no file_types draws for.
const EveryFileType = "*"

// FileTypeClaim is the file types one module claims for one use.
type FileTypeClaim struct {
	Instance  ref.Ref
	Namespace string
	Use       string
	Types     []string
}

// FileTypeConflict is two enabled modules that claim one file type for one
// use.
type FileTypeConflict struct {
	First, Second FileTypeClaim
	// Types is the file types both claim.
	Types []string
}

// Describe writes the conflict for a refusal and a plan.
func (c FileTypeConflict) Describe() string {
	verb := "read"
	if c.First.Use == UseFileView {
		verb = "draw"
	}

	return c.First.Namespace + " and " + c.Second.Namespace + " both " + verb + " " +
		strings.Join(c.Types, ", ") + " files (" + c.First.Use + ")"
}

// key identifies a conflict, so the same one can be found in two sets of
// declarations.
func (c FileTypeConflict) key() string {
	return c.First.Namespace + " " + c.Second.Namespace + " " + c.First.Use
}

// Involves reports whether a module of this namespace is one of the two.
func (c FileTypeConflict) Involves(namespace string) bool {
	return c.First.Namespace == namespace || c.Second.Namespace == namespace
}

// ClaimsOf is what one module claims, by use.
func ClaimsOf(instance ref.Ref, namespace string, fileTypes, viewTypes []string) []FileTypeClaim {
	var out []FileTypeClaim

	if len(fileTypes) > 0 {
		out = append(
			out,
			FileTypeClaim{Instance: instance, Namespace: namespace, Use: UseFileAnalysis, Types: fileTypes},
		)
	}

	if len(viewTypes) > 0 {
		out = append(out, FileTypeClaim{Instance: instance, Namespace: namespace, Use: UseFileView, Types: viewTypes})
	}

	return out
}

// FileTypeClaims is what each enabled module claims, by use.
func FileTypeClaims(declared []Declaration) []FileTypeClaim {
	var out []FileTypeClaim

	for _, one := range declared {
		namespace, isModule := one.ModuleNamespace()
		if !isModule || one.Spec.Module == nil || !one.Spec.Enabled {
			continue
		}

		out = append(out, ClaimsOf(one.Ref, namespace, one.Spec.Module.FileTypes, one.Spec.Module.ViewTypes)...)
	}

	return out
}

// SharedFileTypes is the types two claims share. EveryFileType shares every
// type of the other claim.
func SharedFileTypes(first, second []string) []string {
	switch {
	case slices.Contains(first, EveryFileType):
		return second
	case slices.Contains(second, EveryFileType):
		return first
	}

	var out []string

	for _, fileType := range first {
		if slices.Contains(second, fileType) {
			out = append(out, fileType)
		}
	}

	return out
}

// ConflictsAmong is every pair of claims of one use that share a type.
func ConflictsAmong(claims []FileTypeClaim) []FileTypeConflict {
	var out []FileTypeConflict

	for i, first := range claims {
		for _, second := range claims[i+1:] {
			if first.Use != second.Use || first.Namespace == second.Namespace {
				continue
			}

			if shared := SharedFileTypes(first.Types, second.Types); len(shared) > 0 {
				out = append(out, FileTypeConflict{First: first, Second: second, Types: shared})
			}
		}
	}

	return out
}

// FileTypeConflicts is every conflict among the enabled modules.
func FileTypeConflicts(declared []Declaration) []FileTypeConflict {
	return ConflictsAmong(FileTypeClaims(declared))
}

// checkFileTypes refuses a change that adds a conflict with the changed
// module. A conflict that existed before the change does not refuse it. A
// file the operator wrote can hold such a conflict.
func checkFileTypes(before, after []Declaration, changed ref.Ref) error {
	was := map[string]bool{}
	for _, conflict := range FileTypeConflicts(before) {
		was[conflict.key()] = true
	}

	var lines []string

	for _, conflict := range FileTypeConflicts(after) {
		if was[conflict.key()] || (conflict.First.Instance != changed && conflict.Second.Instance != changed) {
			continue
		}

		lines = append(lines, conflict.Describe())
	}

	if len(lines) == 0 {
		return nil
	}

	return httpfault.ModuleFileTypeConflict(changed.String(), strings.Join(lines, "; "))
}

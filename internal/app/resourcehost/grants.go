package resourcehost

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/prusa3d/prusa-connect/sdk/manifest"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// errNoDataRoot reports a module granted a data directory on a host that
// names no place to make one.
var errNoDataRoot = errors.New("this server names no directory for module data")

// ownerFile is the file in a data directory that names the module it was made
// for. The directory is kept when a module is removed. Without this file a
// different module installed later under the same instance name would read
// the first module's data.
const ownerFile = ".owner"

// dataOwnedError reports a data directory made for another module.
type dataOwnedError struct {
	owner string
}

func (e dataOwnedError) Error() string {
	return "the data directory belongs to the module " + e.owner
}

// grantsOf is what the operator granted one module beyond the API, in the
// shape plugin.configure carries, and nil for a module granted nothing. The
// data directory is made here, before the process exists, so the module
// never starts with a path it cannot write.
func grantsOf(dataRoot string, instance ref.Ref, spec *serviceSpec) (*wirerpc.Grants, error) {
	if spec == nil || (len(spec.service.Listen) == 0 && !spec.service.Data && spec.service.Database == nil) {
		return nil, nil //nolint:nilnil // a module granted nothing is sent no grants.
	}

	grants := &wirerpc.Grants{}

	for name, port := range spec.service.Listen {
		if grants.Listen == nil {
			grants.Listen = map[string]wirerpc.ListenGrant{}
		}

		grants.Listen[name] = wirerpc.ListenGrant{Network: string(port.Network), Port: port.Port}
	}

	// A SQLite database is a file in the data directory, so it needs the
	// directory whether or not the manifest asked for it.
	held := spec.service.Database
	sqlite := held != nil && held.Engine == wirerpc.DatabaseSQLite

	if held != nil && !sqlite {
		grants.Database = &wirerpc.DatabaseGrant{Engine: held.Engine, DSN: held.DSN}
	}

	if !spec.service.Data && !sqlite {
		return grants, nil
	}

	dir, err := moduleDataDir(dataRoot, instance, spec.typeName)
	if owned, ok := errors.AsType[dataOwnedError](err); ok {
		return nil, moduleDataOwned.New("the data directory "+dir+" belongs to the module "+owned.owner+
			", which was installed under this name before").
			With(fault.StringParam("resource", instance.String()), fault.StringParam("path", dir)).
			Wrap(err)
	}

	if err != nil {
		return nil, moduleDataUnavailable.New("the module's data directory cannot be made").
			With(fault.StringParam("resource", instance.String()), fault.StringParam("path", dir)).
			Wrap(err)
	}

	if spec.service.Data {
		grants.Data = dir
		// The module learns the routes the host stages, and not a route the
		// declaration also names public, whose body goes on the wire.
		grants.Staged = stagedGrantsOf(stagedApart(spec.service.PublicRoutes, spec.service.StagedRoutes))
	}

	if sqlite {
		grants.Database = &wirerpc.DatabaseGrant{
			Engine: wirerpc.DatabaseSQLite, DSN: filepath.Join(dir, sqliteDatabaseFile),
		}
	}

	return grants, nil
}

// stagedGrantsOf is each staged route the operator approved, in the shape
// plugin.configure carries. It is sent only with a data directory, because
// the host writes a staged body nowhere else (ADR-20261120-xys rule 6).
func stagedGrantsOf(routes []manifest.StagedRoute) []wirerpc.StagedGrant {
	var out []wirerpc.StagedGrant
	for _, route := range routes {
		out = append(out, wirerpc.StagedGrant{Method: route.Method, Path: route.Path, MaxMiB: route.MaxMiB})
	}

	return out
}

// sqliteDatabaseFile is the name of a module's SQLite database in its data
// directory.
const sqliteDatabaseFile = "database.sqlite"

// moduleDataDir makes the instance's data directory under root for the
// module owner and returns its absolute path. The folder is named after the
// instance, and an install refuses a second module under a name another
// module holds, so two installed modules never share one. A folder that
// another module left is refused.
func moduleDataDir(root string, instance ref.Ref, owner string) (string, error) {
	if root == "" {
		return "", errNoDataRoot
	}

	dir, err := dataPath(root, instance)
	if err != nil {
		return "", err
	}

	if err = os.MkdirAll(dir, 0o750); err != nil {
		return dir, fmt.Errorf("make the data directory: %w", err)
	}

	if err = limitDataDir(dir); err != nil {
		return dir, err
	}

	return dir, claimDataDir(dir, owner)
}

// dataPath is where the instance's data directory is under root.
func dataPath(root string, instance ref.Ref) (string, error) {
	dir, err := filepath.Abs(filepath.Join(root, instance.ID()))
	if err != nil {
		return "", fmt.Errorf("resolve the data directory: %w", err)
	}

	return dir, nil
}

// sharedBits is the part of a mode that lets the group write and gives other
// users any access.
const sharedBits = 0o027

// limitDataDir refuses a data directory that is a link, so the module never
// writes outside the folder its name gives. It removes the group's write
// permission and all access of other users from a folder that has them. A
// folder the server may not change keeps its mode: an operator who made it for
// the server, such as a volume owned by another user, chose that mode.
func limitDataDir(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("read the data directory: %w", err)
	}

	if !info.IsDir() {
		return fmt.Errorf("the data directory %s is not a directory: %w", dir, fs.ErrInvalid)
	}

	if info.Mode().Perm()&sharedBits == 0 {
		return nil
	}

	err = os.Chmod(dir, info.Mode().Perm()&^sharedBits)
	if err != nil && !errors.Is(err, fs.ErrPermission) {
		return fmt.Errorf("limit the data directory to the server: %w", err)
	}

	return nil
}

// claimDataDir records owner in a folder that names no owner, and refuses a
// folder that names another. The owner file is never followed as a link, and
// it is created only when it does not exist, so two starts cannot both write
// it.
func claimDataDir(dir, owner string) error {
	path := filepath.Join(dir, ownerFile)

	for range 2 {
		named, err := readOwner(path)
		if err == nil {
			if named != owner {
				return dataOwnedError{owner: named}
			}

			return nil
		}

		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}

		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}

		if err != nil {
			return fmt.Errorf("record the data directory's owner: %w", err)
		}

		_, err = file.WriteString(owner + "\n")
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}

		if err != nil {
			return fmt.Errorf("record the data directory's owner: %w", err)
		}

		return nil
	}

	return fmt.Errorf("record the data directory's owner: %w", fs.ErrExist)
}

// readOwner reads the owner file, refusing anything but a plain file.
func readOwner(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err //nolint:wrapcheck // the caller tells a missing file by fs.ErrNotExist.
	}

	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("the data directory's owner file %s is not a plain file: %w", path, fs.ErrInvalid)
	}

	held, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read the data directory's owner: %w", err)
	}

	return strings.TrimSpace(string(held)), nil
}

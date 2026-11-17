package app

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/app/moduledeps"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/upgrade"
	sdkextension "github.com/prusa3d/prusa-connect/sdk/extension"
)

// A core upgrade is one plan with the module releases it needs, as a
// `composer update` is. The core is the platform package. The resolver takes
// the candidate version as the core, adds the releases a signed offline bundle
// carries, and resolves again every installed module the candidate leaves
// without a fitting release. The transaction then installs the plan's releases
// after its backup and before the swap. So the trial tests them with the core,
// and a rollback removes them with it.

// moduleCatalog is what a core upgrade reads of the module index: the
// releases it offers, and a way to the bytes of one of them.
type moduleCatalog interface {
	candidates(ctx context.Context) ([]moduledeps.Release, error)
	fetchOffered(ctx context.Context, release moduledeps.Release) (string, func(), error)
}

// fetchOffered is moduleCatalog's fetch: an index row's path where it is, or
// its URL downloaded and held to its digest, with the Beta Portal rule every
// marketplace download keeps.
func (m *moduleInstaller) fetchOffered(ctx context.Context, release moduledeps.Release) (string, func(), error) {
	return m.reachable(ctx, m.marketSource(release.Source, release.Digest))
}

// bundled marks a release the signed offline bundle carries, with its index
// in the journal's extensions.
type bundled struct{ index int }

// modulePlan is what a core upgrade's resolution decided.
type modulePlan struct {
	// steps is the plan, in the order apply installs it. Empty is an upgrade
	// every installed module already allows.
	steps []moduledeps.Step
	// refused is the resolver's refusal, and nil when it found a plan.
	refused error
	// broken is every installed module the candidate leaves without its core
	// constraint, and every module that requires one, all the way up.
	broken []string
}

// planModules resolves the module releases a core upgrade to the journal's
// candidate needs. A resolver refusal is not an error: it is a plan with no
// steps whose broken modules stop serving, which the impacts report.
func (m *maintenance) planModules(ctx context.Context, journal upgrade.Journal) (modulePlan, error) {
	doc, err := resource.Load(resource.Paths{File: m.configPath, Overlay: m.core.Overlay})
	if err != nil {
		return modulePlan{}, err
	}

	lock, err := moduledeps.ReadLock(moduledeps.LockPath(m.core.Overlay))
	if err != nil {
		return modulePlan{}, err
	}

	added, err := bundledReleases(journal)
	if err != nil {
		return modulePlan{}, err
	}

	available := slices.Clone(added)

	// An offline server reads no index, and the bundle alone may be enough.
	// When the index cannot be read, the resolver reports what is missing.
	if m.catalog != nil {
		if offered, offeredErr := m.catalog.candidates(ctx); offeredErr == nil {
			available = append(available, offered...)
		}
	}

	installed := moduledeps.InstalledOf(doc.Instances)
	planned := modulePlan{broken: moduledeps.CoreBroken(installed, journal.ToVersion)}

	plan, refused := moduledeps.CoreUpgrade(moduledeps.Request{
		Installed: installed, Available: available, Lock: lock, Add: added, Settings: resolverSettings(m.core),
	}, journal.ToVersion)

	planned.steps, planned.refused = plan.Steps, refused

	return planned, nil
}

// moduleDatabasesDumped refuses an upgrade that installs module releases
// while a module keeps its data in PostgreSQL and no pg_dump protects it: a
// rollback would leave that data migrated.
func (m *maintenance) moduleDatabasesDumped() error {
	_, postgres, err := moduleDatabaseSources(
		m.configPath, m.core, upgrade.PostgresMode(m.core.Upgrade.PostgresBackup), true,
	)
	if err != nil {
		return err
	}

	if len(postgres) > 0 && m.core.Upgrade.PostgresBackup != string(upgrade.PostgresDump) {
		return fmt.Errorf(
			"%w: installing module releases requires pg_dump protection for PostgreSQL module databases",
			upgrade.ErrPreflight,
		)
	}

	return nil
}

// stagedImpacts is one impact for each release the upgrade installs that the
// server does not have yet.
func stagedImpacts(extensions []upgrade.StagedExtension, installed []moduledeps.Installed) []upgrade.ModuleImpact {
	var out []upgrade.ModuleImpact

	for _, extension := range extensions {
		namespace, err := sdkextension.NamespaceOf(extension.Module)
		if err != nil {
			continue
		}

		if _, found := installedOf(installed, namespace.String()); found {
			continue
		}

		reason := "verified offline extension archive"
		if extension.Source != "" {
			reason = "the module plan installs it beside the candidate core"
		}

		out = append(out, upgrade.ModuleImpact{
			Module: extension.Module, Outcome: upgrade.ImpactStaged, Reason: reason,
		})
	}

	return out
}

// bundledReleases is every extension the signed bundle carries, as the
// resolver reads a release.
func bundledReleases(journal upgrade.Journal) ([]moduledeps.Release, error) {
	out := make([]moduledeps.Release, 0, len(journal.Extensions))

	for index, extension := range journal.Extensions {
		namespace, err := sdkextension.NamespaceOf(extension.Module)
		if err != nil {
			return nil, fmt.Errorf("%s names no namespace: %w", extension.Module, err)
		}

		dir, done, err := stageRelease(extension.StagedPath)
		if err != nil {
			return nil, err
		}

		held, err := readRelease(dir)

		done()

		if err != nil {
			return nil, fmt.Errorf("read bundled extension %s: %w", extension.Module, err)
		}

		release := targetOf(extension.Module, namespace.String(), held.manifest)
		release.Digest, release.Extra = extension.SHA256, bundled{index: index}
		out = append(out, release)
	}

	return out, nil
}

// stagePlan is every release apply installs, in the plan's order: the
// bundle's own, and each one the plan took from the index, downloaded into
// the upgrade's staging directory now, so apply needs no network and the
// runner can hold each file to the digest recorded here.
func (m *maintenance) stagePlan(
	ctx context.Context, journal upgrade.Journal, planned modulePlan, installed []moduledeps.Installed,
) ([]upgrade.StagedExtension, error) {
	out := make([]upgrade.StagedExtension, 0, len(planned.steps)+len(journal.Extensions))
	taken := map[int]bool{}

	for _, step := range planned.steps {
		constraint := stepConstraint(step, installed)

		if from, isBundled := step.Release.Extra.(bundled); isBundled {
			one := journal.Extensions[from.index]
			one.Constraint, one.Pulled = constraint, step.Pulled
			out = append(out, one)
			taken[from.index] = true

			continue
		}

		if m.catalog == nil {
			return nil, fmt.Errorf("%w: the upgrade needs %s %s, and this server reads no module index",
				upgrade.ErrPreflight, step.Release.Module, step.Release.Version)
		}

		staged, err := m.stageOffered(ctx, journal, step.Release)
		if err != nil {
			return nil, fmt.Errorf("stage %s %s for the upgrade: %w", step.Release.Module, step.Release.Version, err)
		}

		staged.Constraint, staged.Pulled = constraint, step.Pulled
		out = append(out, staged)
	}

	// A bundled release the plan needed nothing of is the version already
	// installed. It is installed again, which writes the same declaration.
	for index, one := range journal.Extensions {
		if !taken[index] {
			out = append(out, one)
		}
	}

	return out, nil
}

// stepConstraint is the constraint the lock records for a release the
// upgrade installs: the operator's own for a module they installed, none for
// one another module pulled in, and the release's minor line for a new one.
func stepConstraint(step moduledeps.Step, installed []moduledeps.Installed) string {
	if step.Pulled {
		return ""
	}

	held, found := installedOf(installed, step.Release.Namespace)
	if !found || held.Constraint == "" {
		return "^" + step.Release.Version
	}

	return held.Constraint
}

// stageOffered downloads one release the index offers into the upgrade's
// staging directory, as an archive, and reads what the journal records of it.
func (m *maintenance) stageOffered(
	ctx context.Context, journal upgrade.Journal, release moduledeps.Release,
) (upgrade.StagedExtension, error) {
	from, done, err := m.catalog.fetchOffered(ctx, release)
	if err != nil {
		return upgrade.StagedExtension{}, err
	}

	defer done()

	dir := filepath.Join(filepath.Dir(journal.StagedBinary), "modules")
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return upgrade.StagedExtension{}, fmt.Errorf("create the module staging directory: %w", err)
	}

	name := strings.ReplaceAll(release.Namespace, "/", "_") + "-" + release.Version + archiveSuffix
	path := filepath.Join(dir, name)

	if err = archiveRelease(from, path); err != nil {
		return upgrade.StagedExtension{}, err
	}

	unpacked, cleanup, err := stageRelease(path)
	if err != nil {
		return upgrade.StagedExtension{}, err
	}

	defer cleanup()

	held, err := readRelease(unpacked)
	if err != nil {
		return upgrade.StagedExtension{}, err
	}

	if held.manifest.Version != release.Version {
		return upgrade.StagedExtension{}, fmt.Errorf("the index offered %s and the release says %s",
			release.Version, held.manifest.Version)
	}

	sum, size, err := fileDigest(path)
	if err != nil {
		return upgrade.StagedExtension{}, err
	}

	return upgrade.StagedExtension{
		BundleExtension: upgrade.BundleExtension{
			Module: release.Module, Name: held.manifest.Name, Kind: held.manifest.Kind,
			Version: held.manifest.Version, CoreRange: held.manifest.Core,
			ContractMajor: held.manifest.ContractMajor, Path: "modules/" + name, SHA256: sum, Size: size,
		},
		StagedPath: path,
		Source:     release.Source,
	}, nil
}

// archiveRelease puts a release at path as a gzipped tar: an archive is
// copied, and a directory, which is how a local index names a release, is
// packed. The runner then holds one file to one digest, whichever it was.
func archiveRelease(from, path string) error {
	info, err := os.Stat(from)
	if err != nil {
		return fmt.Errorf("read the release: %w", err)
	}

	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create the staged release: %w", err)
	}

	defer func() { _ = out.Close() }()

	if !info.IsDir() {
		in, openErr := os.Open(from)
		if openErr != nil {
			return fmt.Errorf("open the release: %w", openErr)
		}

		defer func() { _ = in.Close() }()

		if _, err = io.Copy(out, in); err != nil {
			return fmt.Errorf("copy the release: %w", err)
		}

		return out.Sync()
	}

	if err = packDirectory(out, from); err != nil {
		return err
	}

	return out.Sync()
}

// packDirectory writes every regular file under dir into w as a gzipped
// tar, which is the layout unpack reads back. It reads through an os.Root, so
// a link swapped in while it walks cannot lead it outside dir, and a link or
// a device is refused, as unpack refuses one.
func packDirectory(w io.Writer, dir string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("open the release: %w", err)
	}

	defer func() { _ = root.Close() }()

	zipped := gzip.NewWriter(w)
	archive := tar.NewWriter(zipped)

	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}

		if !entry.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file, and a release holds only those", name)
		}

		return packFile(archive, root, name, entry)
	})
	if err != nil {
		return fmt.Errorf("pack the release: %w", err)
	}

	if err = archive.Close(); err != nil {
		return fmt.Errorf("pack the release: %w", err)
	}

	return zipped.Close()
}

// packFile writes one regular file of the release into the archive.
func packFile(archive *tar.Writer, root *os.Root, name string, entry fs.DirEntry) error {
	info, err := entry.Info()
	if err != nil {
		return err
	}

	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}

	header.Name = name

	if err = archive.WriteHeader(header); err != nil {
		return err
	}

	file, err := root.Open(name)
	if err != nil {
		return err
	}

	_, err = io.Copy(archive, file)

	return errors.Join(err, file.Close())
}

// fileDigest is a file's lowercase sha256 and its size.
func fileDigest(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}

	defer func() { _ = file.Close() }()

	hash := sha256.New()

	size, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, err
	}

	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

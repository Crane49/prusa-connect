package upgrade

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

//nolint:gocognit // The extension archive is a second trust boundary inside the signed bundle.
func inspectModuleArchive(file string, limit int64) (manifest.Manifest, error) {
	source, err := os.Open(file)
	if err != nil {
		return manifest.Manifest{}, fmt.Errorf("%w: open extension archive: %w", ErrBundle, err)
	}
	defer func() { _ = source.Close() }()

	zipped, err := gzip.NewReader(bufio.NewReader(source))
	if err != nil {
		return manifest.Manifest{}, fmt.Errorf("%w: extension is not a gzip stream: %w", ErrBundle, err)
	}
	defer func() { _ = zipped.Close() }()

	reader := tar.NewReader(io.LimitReader(zipped, limit+1))
	files := 0
	var document []byte
	seen := make(map[string]bool)
	for {
		header, nextErr := reader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return manifest.Manifest{}, fmt.Errorf("%w: read extension archive: %w", ErrBundle, nextErr)
		}
		files++
		if files > maxModuleArchiveFiles {
			return manifest.Manifest{}, fmt.Errorf("%w: extension exceeds %d files", ErrBundle, maxModuleArchiveFiles)
		}
		checkedName := header.Name
		if header.Typeflag == tar.TypeDir {
			checkedName = strings.TrimSuffix(checkedName, "/")
		}
		if err = validateSafeArchiveName(checkedName); err != nil {
			return manifest.Manifest{}, err
		}
		if seen[checkedName] {
			return manifest.Manifest{}, fmt.Errorf("%w: extension repeats %q", ErrBundle, header.Name)
		}
		seen[checkedName] = true
		if header.Typeflag == tar.TypeDir {
			if header.Size != 0 {
				return manifest.Manifest{}, fmt.Errorf(
					"%w: extension directory %q has content",
					ErrBundle,
					header.Name,
				)
			}

			continue
		}
		if header.Typeflag != tar.TypeReg {
			return manifest.Manifest{}, fmt.Errorf("%w: extension %q is not a regular file", ErrBundle, header.Name)
		}
		if header.Size < 0 || header.Size > limit {
			return manifest.Manifest{}, fmt.Errorf("%w: extension file %q exceeds its limit", ErrBundle, header.Name)
		}
		if header.Name == moduleManifestName {
			document, err = io.ReadAll(io.LimitReader(reader, checksumScanMax+1))
			if err != nil || len(document) > checksumScanMax {
				return manifest.Manifest{}, fmt.Errorf("%w: read extension module.yaml", ErrBundle)
			}
		}
	}
	if document == nil {
		return manifest.Manifest{}, fmt.Errorf("%w: extension has no root module.yaml", ErrBundle)
	}

	declared, err := manifest.Parse(moduleManifestName, document, yaml.Unmarshal)
	if err != nil {
		return manifest.Manifest{}, fmt.Errorf("%w: %w", ErrBundle, err)
	}

	return declared, nil
}

func verifyExtensionIdentity(
	signed BundleExtension,
	actual manifest.Manifest,
	bundle BundleManifest,
	options BundleVerifyOptions,
) error {
	if signed.Name != actual.Name || signed.Kind != actual.Kind || signed.Version != actual.Version ||
		signed.ContractMajor != actual.ContractMajor || signed.CoreRange != moduleCoreRange(actual) {
		return fmt.Errorf("%w: extension %s metadata does not match module.yaml", ErrBundle, signed.Module)
	}
	if actual.ContractMajor > 0 &&
		!slices.Contains(actual.Platforms, bundle.GOOS+"_"+bundle.GOARCH) {
		return fmt.Errorf("%w: extension %s has no %s/%s executable",
			ErrBundleCompatibility, signed.Module, bundle.GOOS, bundle.GOARCH)
	}
	if options.ContractMajor > 0 && actual.ContractMajor > 0 &&
		actual.ContractMajor != options.ContractMajor {
		return fmt.Errorf("%w: extension %s uses wire contract %d, expected %d",
			ErrBundleCompatibility, signed.Module, actual.ContractMajor, options.ContractMajor)
	}

	return nil
}

// moduleCoreRange is the constraint an extension's require puts on the core,
// in Composer's grammar, and empty when it puts none.
func moduleCoreRange(declared manifest.Manifest) string {
	return declared.Core
}

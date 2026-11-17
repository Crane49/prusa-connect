package upgrade

import (
	"archive/tar"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/internal/kernel/atomicfile"
	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

const (
	BundleSchema             = 1
	BundleManifestName       = "manifest.json"
	BundleSignatureName      = "manifest.sig"
	DefaultMaxBundleBytes    = int64(1 << 30)
	DefaultMaxExtensionBytes = int64(256 << 20)
	DefaultMaxExtensions     = 64
	maxModuleArchiveFiles    = 4096
	maxBundlePathBytes       = 240
	moduleManifestName       = "module.yaml"
)

var (
	ErrBundle              = errors.New("upgrade: invalid offline bundle")
	ErrBundleCompatibility = errors.New("upgrade: offline bundle is incompatible")
)

// BundleManifest is the signed identity of every byte in an offline update.
type BundleManifest struct {
	Schema       int               `json:"schema"`
	CoreVersion  string            `json:"core_version"`
	GOOS         string            `json:"goos"`
	GOARCH       string            `json:"goarch"`
	BuildProfile string            `json:"build_profile"`
	BuildVariant string            `json:"build_variant"`
	Core         BundleArtifact    `json:"core"`
	Extensions   []BundleExtension `json:"extensions"`
}

type BundleArtifact struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type BundleExtension struct {
	Module        string `json:"module"               doc:"The extension module path named by the signed bundle."`
	Name          string `json:"name"                 doc:"The short extension name read from module.yaml."`
	Kind          string `json:"kind"                 doc:"The resource kind read from module.yaml."`
	Version       string `json:"version"              doc:"The extension release version read from module.yaml."`
	CoreRange     string `json:"core_range,omitempty" doc:"The constraint module.yaml's require puts on the core, in Composer's grammar."`
	ContractMajor int    `json:"contract_major"       doc:"The extension wire contract major read from module.yaml."`
	Path          string `json:"path"                 doc:"The artifact path inside the offline bundle."`
	SHA256        string `json:"sha256"               doc:"The lowercase SHA-256 digest signed for this artifact."`
	Size          int64  `json:"size"                 doc:"The signed artifact size in bytes."`
}

type BundleBuild struct {
	CoreVersion  string
	GOOS         string
	GOARCH       string
	BuildProfile string
	BuildVariant string
	CorePath     string
	Extensions   []BundleBuildExtension
	PrivateKey   ed25519.PrivateKey
}

type BundleBuildExtension struct {
	Module string
	Path   string
}

type BundleVerifyOptions struct {
	GOOS              string
	GOARCH            string
	BuildProfile      string
	BuildVariant      string
	ContractMajor     int
	MaxBundleBytes    int64
	MaxExtensionBytes int64
	MaxExtensions     int
}

type StagedBundle struct {
	Manifest      BundleManifest
	ManifestBytes []byte
	KeyID         string
	CorePath      string
	Extensions    []StagedExtension
}

type StagedExtension struct {
	BundleExtension

	StagedPath string `json:"staged_path" doc:"The private path holding the verified extension archive."`
	// Source, Constraint and Pulled are what the modules lock records for a
	// release the upgrade's plan took from the module index. A release the
	// signed bundle carries leaves them empty.
	Source     string `json:"source,omitempty"     doc:"Where the module index offered this release, absent for one the signed bundle carries."`
	Constraint string `json:"constraint,omitempty" doc:"The constraint the modules lock records for the module, absent for a module another one pulled in."`
	Pulled     bool   `json:"pulled,omitempty"     doc:"True for a module the upgrade's plan installs because another module requires it."`
}

// BuildBundle writes a deterministic uncompressed POSIX tar archive.
//
//nolint:gocognit // Each branch rejects one part of the signed format before output is published.
func BuildBundle(out io.Writer, request BundleBuild) (BundleManifest, error) {
	if len(request.PrivateKey) != ed25519.PrivateKeySize {
		return BundleManifest{}, fmt.Errorf("%w: an Ed25519 private key is required", ErrBundle)
	}
	if request.GOOS == "" {
		request.GOOS = runtime.GOOS
	}
	if request.GOARCH == "" {
		request.GOARCH = runtime.GOARCH
	}

	core, err := artifactOf(request.CorePath, "core/"+filepath.Base(request.CorePath), DefaultMaxBundleBytes)
	if err != nil {
		return BundleManifest{}, err
	}
	bundle := BundleManifest{
		Schema: BundleSchema, CoreVersion: request.CoreVersion, GOOS: request.GOOS, GOARCH: request.GOARCH,
		BuildProfile: request.BuildProfile, BuildVariant: request.BuildVariant, Core: core,
		Extensions: make([]BundleExtension, 0, len(request.Extensions)),
	}

	sources := map[string]string{core.Path: request.CorePath}
	for _, item := range request.Extensions {
		name := filepath.Base(item.Path)
		artifact, artifactErr := artifactOf(item.Path, "extensions/"+name, DefaultMaxExtensionBytes)
		if artifactErr != nil {
			return BundleManifest{}, artifactErr
		}
		declared, inspectErr := inspectModuleArchive(item.Path, DefaultMaxExtensionBytes)
		if inspectErr != nil {
			return BundleManifest{}, inspectErr
		}
		extension := BundleExtension{
			Module: item.Module, Name: declared.Name, Kind: declared.Kind, Version: declared.Version,
			CoreRange: moduleCoreRange(declared), ContractMajor: declared.ContractMajor,
			Path: artifact.Path, SHA256: artifact.SHA256, Size: artifact.Size,
		}
		if err = verifyExtensionIdentity(extension, declared, bundle, BundleVerifyOptions{}); err != nil {
			return BundleManifest{}, err
		}
		bundle.Extensions = append(bundle.Extensions, extension)
		if _, exists := sources[artifact.Path]; exists {
			return BundleManifest{}, fmt.Errorf("%w: path %q is repeated", ErrBundle, artifact.Path)
		}
		sources[artifact.Path] = item.Path
	}
	slices.SortFunc(bundle.Extensions, func(left, right BundleExtension) int {
		return strings.Compare(left.Path, right.Path)
	})
	if err = validateBundleManifest(bundle, bundleVerifyDefaults(BundleVerifyOptions{
		GOOS: request.GOOS, GOARCH: request.GOARCH,
		BuildProfile: request.BuildProfile, BuildVariant: request.BuildVariant,
	})); err != nil {
		return BundleManifest{}, err
	}

	document, err := json.Marshal(bundle)
	if err != nil {
		return BundleManifest{}, fmt.Errorf("%w: encode manifest: %w", ErrBundle, err)
	}
	public, ok := request.PrivateKey.Public().(ed25519.PublicKey)
	if !ok {
		return BundleManifest{}, fmt.Errorf("%w: Ed25519 private key returned another public key type", ErrBundle)
	}
	signature := Signature{
		Algorithm: "ed25519",
		KeyID:     KeyID(public),
		Value:     base64.StdEncoding.EncodeToString(ed25519.Sign(request.PrivateKey, document)),
	}
	detached, err := json.Marshal(signature)
	if err != nil {
		return BundleManifest{}, fmt.Errorf("%w: encode signature: %w", ErrBundle, err)
	}

	writer := tar.NewWriter(out)
	if err = writeTarBytes(writer, BundleManifestName, document, privateFile); err == nil {
		err = writeTarBytes(writer, BundleSignatureName, detached, privateFile)
	}
	for _, name := range sortedKeys(sources) {
		if err != nil {
			break
		}
		err = writeTarFile(writer, name, sources[name], bundle.Core.Path == name)
	}
	err = errors.Join(err, writer.Close())
	if err != nil {
		return BundleManifest{}, fmt.Errorf("%w: write archive: %w", ErrBundle, err)
	}

	return bundle, nil
}

// VerifyAndStageBundle verifies all metadata and artefacts before publishing a
// private staging directory. destination must not already exist.
//
//nolint:gocognit,cyclop // The parser keeps every archive trust-boundary check in read order.
func (v Verifier) VerifyAndStageBundle(
	source io.Reader,
	destination string,
	options BundleVerifyOptions,
) (StagedBundle, error) {
	options = bundleVerifyDefaults(options)
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, privateDir); err != nil {
		return StagedBundle{}, fmt.Errorf("%w: create staging parent: %w", ErrBundle, err)
	}
	temporary, err := os.MkdirTemp(parent, ".offline-bundle-")
	if err != nil {
		return StagedBundle{}, fmt.Errorf("%w: create staging directory: %w", ErrBundle, err)
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(temporary)
		}
	}()

	limited := &io.LimitedReader{R: source, N: options.MaxBundleBytes + 1}
	reader := tar.NewReader(limited)
	manifestHeader, err := reader.Next()
	if err != nil {
		return StagedBundle{}, fmt.Errorf("%w: read manifest: %w", ErrBundle, err)
	}
	if err = validateTarHeader(manifestHeader, BundleManifestName, int64(checksumScanMax)); err != nil {
		return StagedBundle{}, err
	}
	document, err := io.ReadAll(io.LimitReader(reader, checksumScanMax+1))
	if err != nil || len(document) > checksumScanMax {
		return StagedBundle{}, fmt.Errorf("%w: read manifest", ErrBundle)
	}

	signatureHeader, err := reader.Next()
	if err != nil {
		return StagedBundle{}, fmt.Errorf("%w: read signature: %w", ErrBundle, err)
	}
	if err = validateTarHeader(signatureHeader, BundleSignatureName, int64(checksumScanMax)); err != nil {
		return StagedBundle{}, err
	}
	detached, err := io.ReadAll(io.LimitReader(reader, checksumScanMax+1))
	if err != nil || len(detached) > checksumScanMax {
		return StagedBundle{}, fmt.Errorf("%w: read signature", ErrBundle)
	}
	v.allowUnsigned = false
	keyID, err := v.verifySignature(document, detached)
	if err != nil {
		return StagedBundle{}, err
	}

	var declared BundleManifest
	if err = decodeStrictJSON(document, &declared); err != nil {
		return StagedBundle{}, fmt.Errorf("%w: decode manifest: %w", ErrBundle, err)
	}
	if err = validateBundleManifest(declared, options); err != nil {
		return StagedBundle{}, err
	}

	artifacts := make(map[string]BundleArtifact, len(declared.Extensions)+1)
	artifacts[declared.Core.Path] = declared.Core
	for _, extension := range declared.Extensions {
		artifacts[extension.Path] = BundleArtifact{
			Path: extension.Path, SHA256: extension.SHA256, Size: extension.Size,
		}
	}

	result := StagedBundle{Manifest: declared, ManifestBytes: bytes.Clone(document), KeyID: keyID}
	seen := make(map[string]bool, len(artifacts))
	for {
		header, nextErr := reader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return StagedBundle{}, fmt.Errorf("%w: read artifact: %w", ErrBundle, nextErr)
		}
		if err = validateSafeArchiveName(header.Name); err != nil {
			return StagedBundle{}, err
		}
		artifact, found := artifacts[header.Name]
		if !found {
			return StagedBundle{}, fmt.Errorf("%w: undeclared file %q", ErrBundle, header.Name)
		}
		if seen[header.Name] {
			return StagedBundle{}, fmt.Errorf("%w: duplicate file %q", ErrBundle, header.Name)
		}
		if err = validateTarHeader(header, header.Name, artifact.Size); err != nil {
			return StagedBundle{}, err
		}

		target := filepath.Join(temporary, filepath.FromSlash(header.Name))
		mode := os.FileMode(privateFile)
		if header.Name == declared.Core.Path {
			mode = executablePerm
		}
		if err = writeVerifiedArtifact(reader, target, artifact, mode); err != nil {
			return StagedBundle{}, err
		}
		seen[header.Name] = true
	}
	if len(seen) != len(artifacts) {
		return StagedBundle{}, fmt.Errorf("%w: archive has %d of %d declared artifacts",
			ErrBundle, len(seen), len(artifacts))
	}
	var trailing [1]byte
	if read, readErr := limited.Read(trailing[:]); readErr != nil && !errors.Is(readErr, io.EOF) {
		return StagedBundle{}, fmt.Errorf("%w: read archive trailer: %w", ErrBundle, readErr)
	} else if read != 0 {
		return StagedBundle{}, fmt.Errorf("%w: bytes follow the tar end marker", ErrBundle)
	}

	result.CorePath = filepath.Join(destination, filepath.FromSlash(declared.Core.Path))
	for _, extension := range declared.Extensions {
		staged := filepath.Join(temporary, filepath.FromSlash(extension.Path))
		actual, inspectErr := inspectModuleArchive(staged, options.MaxExtensionBytes)
		if inspectErr != nil {
			return StagedBundle{}, inspectErr
		}
		if err = verifyExtensionIdentity(extension, actual, declared, options); err != nil {
			return StagedBundle{}, err
		}
		result.Extensions = append(result.Extensions, StagedExtension{
			BundleExtension: extension,
			StagedPath:      filepath.Join(destination, filepath.FromSlash(extension.Path)),
		})
	}

	if err = atomicfile.Write(filepath.Join(temporary, BundleManifestName), document, privateFile); err != nil {
		return StagedBundle{}, fmt.Errorf("%w: stage manifest: %w", ErrBundle, err)
	}
	if err = atomicfile.Write(filepath.Join(temporary, BundleSignatureName), detached, privateFile); err != nil {
		return StagedBundle{}, fmt.Errorf("%w: stage signature: %w", ErrBundle, err)
	}
	if err = os.Rename(temporary, destination); err != nil {
		return StagedBundle{}, fmt.Errorf("%w: publish staging directory: %w", ErrBundle, err)
	}
	keep = true

	return result, nil
}

//nolint:gocognit // Manifest validation reports the exact incompatible field.
func validateBundleManifest(declared BundleManifest, options BundleVerifyOptions) error {
	if declared.Schema != BundleSchema {
		return fmt.Errorf("%w: schema is %d, expected %d", ErrBundle, declared.Schema, BundleSchema)
	}
	if _, err := manifest.ParseVersion(declared.CoreVersion); err != nil {
		return fmt.Errorf("%w: core_version %q: %w", ErrBundle, declared.CoreVersion, err)
	}
	if declared.GOOS != options.GOOS || declared.GOARCH != options.GOARCH {
		return fmt.Errorf("%w: bundle is %s/%s, server is %s/%s",
			ErrBundleCompatibility, declared.GOOS, declared.GOARCH, options.GOOS, options.GOARCH)
	}
	if options.BuildProfile != "" && declared.BuildProfile != options.BuildProfile {
		return fmt.Errorf("%w: bundle profile is %q, expected %q",
			ErrBundleCompatibility, declared.BuildProfile, options.BuildProfile)
	}
	if options.BuildVariant != "" && declared.BuildVariant != options.BuildVariant {
		return fmt.Errorf("%w: bundle variant is %q, expected %q",
			ErrBundleCompatibility, declared.BuildVariant, options.BuildVariant)
	}
	if len(declared.Extensions) > options.MaxExtensions {
		return fmt.Errorf("%w: %d extensions exceed limit %d",
			ErrBundle, len(declared.Extensions), options.MaxExtensions)
	}
	if err := validateArtifact(declared.Core, "core/", options.MaxBundleBytes); err != nil {
		return err
	}

	paths := map[string]bool{declared.Core.Path: true}
	modules := make(map[string]bool, len(declared.Extensions))
	for _, extension := range declared.Extensions {
		if err := validateSafeArchiveName(extension.Module); err != nil {
			return fmt.Errorf("%w: extension module %q is invalid", ErrBundle, extension.Module)
		}
		if modules[extension.Module] {
			return fmt.Errorf("%w: extension module %q is empty or repeated", ErrBundle, extension.Module)
		}
		modules[extension.Module] = true
		if err := validateArtifact(BundleArtifact{
			Path: extension.Path, SHA256: extension.SHA256, Size: extension.Size,
		}, "extensions/", options.MaxExtensionBytes); err != nil {
			return err
		}
		if paths[extension.Path] {
			return fmt.Errorf("%w: artifact path %q is repeated", ErrBundle, extension.Path)
		}
		paths[extension.Path] = true
		if _, err := manifest.ParseVersion(extension.Version); err != nil {
			return fmt.Errorf("%w: extension %s version: %w", ErrBundle, extension.Module, err)
		}
		if extension.CoreRange != "" {
			wanted, rangeErr := manifest.ParseConstraint(extension.CoreRange)
			candidate, versionErr := manifest.ParseRelease(declared.CoreVersion)
			if rangeErr != nil || versionErr != nil || !wanted.HoldsCore(candidate) {
				return fmt.Errorf("%w: extension %s requires core %s",
					ErrBundleCompatibility, extension.Module, extension.CoreRange)
			}
		}
		if options.ContractMajor > 0 && extension.ContractMajor > 0 &&
			extension.ContractMajor != options.ContractMajor {
			return fmt.Errorf("%w: extension %s uses wire contract %d, expected %d",
				ErrBundleCompatibility, extension.Module, extension.ContractMajor, options.ContractMajor)
		}
	}

	return nil
}

func validateArtifact(artifact BundleArtifact, prefix string, limit int64) error {
	if err := validateSafeArchiveName(artifact.Path); err != nil {
		return err
	}
	if !strings.HasPrefix(artifact.Path, prefix) || strings.TrimPrefix(artifact.Path, prefix) == "" {
		return fmt.Errorf("%w: artifact path %q must be below %s", ErrBundle, artifact.Path, prefix)
	}
	if artifact.Size < 1 || artifact.Size > limit {
		return fmt.Errorf("%w: artifact %q size %d is outside 1..%d",
			ErrBundle, artifact.Path, artifact.Size, limit)
	}
	decoded, err := hex.DecodeString(artifact.SHA256)
	if err != nil || len(decoded) != sha256.Size || artifact.SHA256 != strings.ToLower(artifact.SHA256) {
		return fmt.Errorf("%w: artifact %q has an invalid SHA-256", ErrBundle, artifact.Path)
	}

	return nil
}

func artifactOf(file, archivePath string, limit int64) (BundleArtifact, error) {
	if err := validateSafeArchiveName(archivePath); err != nil {
		return BundleArtifact{}, err
	}
	source, err := os.Open(file)
	if err != nil {
		return BundleArtifact{}, fmt.Errorf("%w: open %s: %w", ErrBundle, file, err)
	}
	defer func() { _ = source.Close() }()
	info, err := source.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return BundleArtifact{}, fmt.Errorf("%w: %s is not a regular file", ErrBundle, file)
	}
	if info.Size() < 1 || info.Size() > limit {
		return BundleArtifact{}, fmt.Errorf("%w: %s exceeds %d bytes", ErrBundle, file, limit)
	}
	digest := sha256.New()
	if _, err = io.Copy(digest, source); err != nil {
		return BundleArtifact{}, fmt.Errorf("%w: hash %s: %w", ErrBundle, file, err)
	}

	return BundleArtifact{
		Path: archivePath, SHA256: hex.EncodeToString(digest.Sum(nil)), Size: info.Size(),
	}, nil
}

func writeTarBytes(writer *tar.Writer, name string, content []byte, mode int64) error {
	header := deterministicHeader(name, int64(len(content)), mode)
	if err := writer.WriteHeader(&header); err != nil {
		return err
	}
	_, err := writer.Write(content)

	return err
}

func writeTarFile(writer *tar.Writer, name, file string, executable bool) error {
	source, err := os.Open(file)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	info, err := source.Stat()
	if err != nil {
		return err
	}
	mode := int64(privateFile)
	if executable {
		mode = executablePerm
	}
	header := deterministicHeader(name, info.Size(), mode)
	if err = writer.WriteHeader(&header); err != nil {
		return err
	}
	_, err = io.Copy(writer, source)

	return err
}

func deterministicHeader(name string, size int64, mode int64) tar.Header {
	epoch := time.Unix(0, 0).UTC()

	return tar.Header{
		Name: name, Size: size, Mode: mode, Typeflag: tar.TypeReg,
		ModTime: epoch, Format: tar.FormatUSTAR,
	}
}

func validateTarHeader(header *tar.Header, expected string, limit int64) error {
	if header.Name != expected {
		return fmt.Errorf("%w: expected %q, found %q", ErrBundle, expected, header.Name)
	}
	if header.Typeflag != tar.TypeReg {
		return fmt.Errorf("%w: %q is not a regular file", ErrBundle, header.Name)
	}
	if header.Size < 0 || header.Size > limit {
		return fmt.Errorf("%w: %q size %d exceeds %d", ErrBundle, header.Name, header.Size, limit)
	}

	return nil
}

func validateSafeArchiveName(name string) error {
	if name == "" || len(name) > maxBundlePathBytes || name != path.Clean(name) ||
		!fs.ValidPath(name) || path.IsAbs(name) || strings.Contains(name, `\`) {
		return fmt.Errorf("%w: unsafe archive path %q", ErrBundle, name)
	}

	return nil
}

func writeVerifiedArtifact(reader io.Reader, target string, artifact BundleArtifact, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), privateDir); err != nil {
		return fmt.Errorf("%w: create artifact directory: %w", ErrBundle, err)
	}
	file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("%w: create artifact: %w", ErrBundle, err)
	}
	digest := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, digest), io.LimitReader(reader, artifact.Size+1))
	syncErr := file.Sync()
	closeErr := file.Close()
	if err = errors.Join(copyErr, syncErr, closeErr); err != nil {
		return fmt.Errorf("%w: write artifact: %w", ErrBundle, err)
	}
	got := hex.EncodeToString(digest.Sum(nil))
	if written != artifact.Size || got != artifact.SHA256 {
		return fmt.Errorf("%w: artifact %q does not match its signed size and SHA-256", ErrChecksum, artifact.Path)
	}

	return nil
}

func decodeStrictJSON(document []byte, into any) error {
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("more than one JSON value")
	}

	return nil
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	return keys
}

func bundleVerifyDefaults(options BundleVerifyOptions) BundleVerifyOptions {
	if options.GOOS == "" {
		options.GOOS = runtime.GOOS
	}
	if options.GOARCH == "" {
		options.GOARCH = runtime.GOARCH
	}
	if options.MaxBundleBytes <= 0 {
		options.MaxBundleBytes = DefaultMaxBundleBytes
	}
	if options.MaxExtensionBytes <= 0 {
		options.MaxExtensionBytes = DefaultMaxExtensionBytes
	}
	if options.MaxExtensions <= 0 {
		options.MaxExtensions = DefaultMaxExtensions
	}

	return options
}

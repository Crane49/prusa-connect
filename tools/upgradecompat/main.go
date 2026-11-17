// Command upgradecompat makes the ephemeral release identity used by the
// scheduled upgrade compatibility lane. It is not used by a product build.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/upgrade"
	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

type fixtureKey struct {
	Seed   string `json:"seed"`
	Public string `json:"public"`
	ID     string `json:"id"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "upgradecompat: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 1 && args[0] == "key" {
		return writeKey()
	}
	if len(args) == 3 && args[0] == "next-version" {
		version, err := nextVersion(args[1], args[2])
		if err != nil {
			return err
		}
		if _, err = fmt.Fprintln(os.Stdout, version); err != nil {
			return fmt.Errorf("write version: %w", err)
		}

		return nil
	}

	return errors.New("usage: upgradecompat key | next-version <source> <run>")
}

func writeKey() error {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate Ed25519 key: %w", err)
	}

	fixture := fixtureKey{
		Seed:   base64.StdEncoding.EncodeToString(private.Seed()),
		Public: base64.StdEncoding.EncodeToString(public),
		ID:     upgrade.KeyID(public),
	}
	if err = json.NewEncoder(os.Stdout).Encode(fixture); err != nil {
		return fmt.Errorf("write key: %w", err)
	}

	return nil
}

func nextVersion(source, run string) (string, error) {
	version, err := manifest.ParseVersion(strings.TrimPrefix(source, "v"))
	if err != nil {
		return "", fmt.Errorf("parse source version: %w", err)
	}
	if run == "" {
		return "", errors.New("run identifier is empty")
	}
	for _, char := range run {
		if char < '0' || char > '9' {
			return "", fmt.Errorf("run identifier %q is not numeric", run)
		}
	}

	return strconv.Itoa(version.Major) + "." +
		strconv.Itoa(version.Minor) + "." +
		strconv.Itoa(version.Patch+1) + "-upgrade-ci." + run, nil
}

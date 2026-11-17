package arch_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// privateNameDigests are the digests of names of repositories, products and
// hosts that are not public. This tree is published, so no tracked file may
// name them, and the guard keeps only a digest so that it does not name them
// either. A name is written in lowercase with a hyphen between its parts, and
// its digest is the first 32 hex characters of its SHA-256. To add a name, run
//
//	printf '%s' 'some-private-name' | shasum -a 256 | cut -c1-32
//
// (sha256sum on Linux) and add the output here. Never write the name itself
// into the tree, a commit message or a pull request.
//
// To describe what a private thing does, write it in plain words: "the Prusa
// Connect cloud API", "the printer firmware".
var privateNameDigests = []string{
	"034042cac3ba6aefa821ed05b106520d",
	"0f2f8aa5059325d1a59efb36a51a46bf",
	"2fd9479da42fd2df18844e7d4bbae74d",
	"30649a2da0c26c0da8f875aa1eef8605",
	"30cce014727386344ef954b48eaef950",
	"362aee4b8cf7747c33683cf459d0095f",
	"39e3d2162e155348ed4b4ccf51ac1e12",
	"4747cca4b1a23e766af2592a02518dd3",
	"5976f723a3da28e1a918afe91e0fc6a4",
	"5ef0327cc321c6c35b26377784f3fd57",
	"790819a27deecbaec9b2003dd00e79cc",
	"82ca7c4e0aa8beba8446f84f5dcc6448",
	"8c52cd578db5aab73d60bdbdb3f96b11",
	"9871b263033d86b566266e8c1c09f2d8",
	"9f02e6e7014a1eaef740c888c71d5324",
	"a0a153dab2fe23ff424997596d419c8b",
	"b0846b23ead9fed9fced7c3e89021cd4",
	"b8d1a7ab29d10d6078a0b4d0f3d3004b",
	"d087ee8196afa2ff461aed248e9b90d5",
	"e3cd3460951a45704b051dea3c669ade",
	"ea74891f06054a7b761a5b6c9f87df91",
}

// publicNameDigests are the digests of public names that contain a private
// one, such as a public repository named after a private one plus a suffix.
// A private name inside one of these is not a finding. They are written the
// same way as privateNameDigests.
var publicNameDigests = []string{
	"9e5081276e6b550abcc7f1f63fa75e0d",
	"93e1bf827fea16ad1fd18e48ca97e661",
}

// nameDigestLength is 16 bytes of SHA-256: 128 bits, so two different names
// never share a digest by chance.
const nameDigestLength = 16

// maxNameParts is the most parts a candidate name is built from. The longest
// name the guard holds has four.
const maxNameParts = 5

// maxNameJoiner is the longest run of joining characters that still links two
// parts into one name, such as "://" or an escaped backslash.
const maxNameJoiner = 3

type nameDigest [nameDigestLength]byte

// nameRules is the set of names the matcher refuses and the public names it
// lets through.
type nameRules struct {
	private map[nameDigest]struct{}
	public  map[nameDigest]struct{}
}

func digestOf(name string) nameDigest {
	sum := sha256.Sum256([]byte(name))

	var digest nameDigest

	copy(digest[:], sum[:nameDigestLength])

	return digest
}

func parseDigests(t *testing.T, hexDigests []string) map[nameDigest]struct{} {
	t.Helper()

	set := make(map[nameDigest]struct{}, len(hexDigests))

	for _, hexDigest := range hexDigests {
		raw, err := hex.DecodeString(hexDigest)
		if err != nil || len(raw) != nameDigestLength {
			t.Fatalf("%q is not %d bytes of hex", hexDigest, nameDigestLength)
		}

		var digest nameDigest

		copy(digest[:], raw)

		if _, seen := set[digest]; seen {
			t.Fatalf("%s is listed twice", hexDigest)
		}

		set[digest] = struct{}{}
	}

	return set
}

func isNameByte(c byte) bool {
	return ('a' <= c && c <= 'z') || ('0' <= c && c <= '9')
}

func isNameJoiner(c byte) bool {
	return strings.IndexByte(`-_./\:`, c) >= 0
}

// nameRuns splits a lowercased line into runs of words. Two words belong to
// one run when only a few joining characters stand between them, so
// "some_repo/src" is one run of three words and "some repo" is two runs.
func nameRuns(line string) [][]string {
	var (
		runs [][]string
		run  []string
	)

	for at := 0; at < len(line); {
		if !isNameByte(line[at]) {
			gap := at
			for gap < len(line) && !isNameByte(line[gap]) {
				gap++
			}

			if !joinsNames(line[at:gap]) && len(run) > 0 {
				runs = append(runs, run)
				run = nil
			}

			at = gap

			continue
		}

		end := at
		for end < len(line) && isNameByte(line[end]) {
			end++
		}

		run = append(run, line[at:end])
		at = end
	}

	if len(run) > 0 {
		runs = append(runs, run)
	}

	return runs
}

func joinsNames(gap string) bool {
	if len(gap) > maxNameJoiner {
		return false
	}

	for i := range len(gap) {
		if !isNameJoiner(gap[i]) {
			return false
		}
	}

	return true
}

// findings returns every private name in body as its line number and the hex
// of its digest, in the order they appear. It never returns the name.
func (rules nameRules) findings(body []byte) []string {
	var found []string

	for index, line := range strings.Split(strings.ToLower(string(body)), "\n") {
		for _, run := range nameRuns(line) {
			for start := range run {
				for parts := 1; parts <= maxNameParts && start+parts <= len(run); parts++ {
					digest := digestOf(strings.Join(run[start:start+parts], "-"))
					if _, private := rules.private[digest]; !private {
						continue
					}

					if rules.insidePublicName(run, start, start+parts) {
						continue
					}

					found = append(found, "line "+strconv.Itoa(index+1)+
						" (digest "+hex.EncodeToString(digest[:])+")")
				}
			}
		}
	}

	return found
}

// insidePublicName reports whether the words run[from:to] sit inside a longer
// public name in the same run.
func (rules nameRules) insidePublicName(run []string, from, to int) bool {
	for start := max(0, to-maxNameParts); start <= from; start++ {
		for end := to; end <= len(run) && end-start <= maxNameParts; end++ {
			if end-start == to-from {
				continue
			}

			if _, public := rules.public[digestOf(strings.Join(run[start:end], "-"))]; public {
				return true
			}
		}
	}

	return false
}

// TestNoTrackedFileNamesAPrivateRepository reads every tracked file and
// refuses one that names a private repository, product or host.
func TestNoTrackedFileNamesAPrivateRepository(t *testing.T) {
	t.Parallel()

	rules := nameRules{
		private: parseDigests(t, privateNameDigests),
		public:  parseDigests(t, publicNameDigests),
	}

	root := repoRoot(t)

	out, err := exec.Command("git", "-C", root, "ls-files", "-z").Output()
	if err != nil {
		t.Fatalf("listing the tracked files: %v", err)
	}

	scanned := 0

	for path := range strings.SplitSeq(strings.TrimRight(string(out), "\x00"), "\x00") {
		body, readErr := os.ReadFile(filepath.Join(root, path))
		if errors.Is(readErr, fs.ErrNotExist) {
			// Deleted in the working tree and not yet committed.
			continue
		}

		if readErr != nil {
			t.Fatalf("reading %s: %v", path, readErr)
		}

		scanned++

		if bytes.IndexByte(body, 0) >= 0 {
			// A binary file, such as an image or a font.
			continue
		}

		for _, finding := range rules.findings(body) {
			t.Errorf("%s names a private repository, product or host on %s. "+
				"Describe the thing in plain words, such as \"the Prusa Connect cloud API\".", path, finding)
		}
	}

	requireScanned(t, "tracked files", scanned, 1000)
}

// TestThePrivateNameGateFires proves the matcher refuses each shape of name
// and passes the public ones. The names are made up, and the test builds their
// digests itself.
func TestThePrivateNameGateFires(t *testing.T) {
	t.Parallel()

	rules := nameRules{private: map[nameDigest]struct{}{}, public: map[nameDigest]struct{}{}}
	for _, name := range []string{"acme-secret-repo", "zorblax", "acme-sdk", "gitlab-example-acme-corp", "acme-gitlab"} {
		rules.private[digestOf(name)] = struct{}{}
	}

	rules.public[digestOf("acme-sdk-printer")] = struct{}{}

	for name, testCase := range map[string]struct {
		body    string
		refused bool
	}{
		"a repository at a commit":     {body: "read in Acme-Secret-Repo at abc123\n", refused: true},
		"a package path":               {body: "see acme_secret_repo/lib/auth.py\n", refused: true},
		"a host and a group":           {body: "go get gitlab.example/acme-corp/connect/x\n", refused: true},
		"an escaped backslash":         {body: `"acme\\gitlab"` + "\n", refused: true},
		"a code name in capitals":      {body: "the ZORBLAX printers\n", refused: true},
		"a code name in mixed case":    {body: "the ZorBlax printers\n", refused: true},
		"a name with a private suffix": {body: "clone acme-secret-repo-tools\n", refused: true},
		"the private sdk":              {body: "the Acme-SDK draft\n", refused: true},
		"the private sdk at an end":    {body: "Acme-SDK", refused: true},
		"the public printer sdk":       {body: "github.example/acme/Acme-SDK-Printer\n", refused: false},
		"the name in plain words":      {body: "the acme secret repo\n", refused: false},
		"a longer word":                {body: "zorblaxes\n", refused: false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			found := rules.findings([]byte(testCase.body))
			if refused := len(found) > 0; refused != testCase.refused {
				t.Errorf("refused=%t, want %t. What it said: %v", refused, testCase.refused, found)
			}
		})
	}

	t.Run("a finding gives the line and the digest, never the name", func(t *testing.T) {
		t.Parallel()

		found := rules.findings([]byte("one\ntwo\nuses zorblax here\n"))

		digest := digestOf("zorblax")
		want := "line 3 (digest " + hex.EncodeToString(digest[:]) + ")"

		if len(found) != 1 || found[0] != want {
			t.Fatalf("found %v, want [%s]", found, want)
		}

		if strings.Contains(found[0], "zorblax") {
			t.Errorf("the finding %q carries the name", found[0])
		}
	})
}

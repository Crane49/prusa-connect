package devtools

import (
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"strings"
	"sync"
)

//go:embed page.html
var pageHTML string

//go:embed page.css
var pageCSS string

// scripts holds every script file the page carries. They are separate files
// because one console covering five areas in one file is a file nobody edits
// safely, and they are concatenated into one block because the policy names the
// script by the hash of its bytes and a browser hashes each block on its own.
//
//go:embed page.js events.js access.js config.js setup.js boot.js
var scripts embed.FS

// ScriptFiles is the order the script files are concatenated in, which is also
// the order they run in. It is exported because the gate that proves the served
// bytes are the files on disk has to rebuild the same block, and a second list
// of names in the test is a second answer to what the page carries.
//
// page.js is first because it holds the helpers the rest call, and boot.js is
// last because it is the only file that runs anything.
//
//nolint:gochecknoglobals // a fixed list of file names, read-only.
var ScriptFiles = []string{"page.js", "events.js", "access.js", "config.js", "setup.js", "boot.js"}

// fileRule is how wide the comment rule between two script files is drawn. It is
// a separator and nothing reads it, so the only thing it has to be is uniform.
const fileRule = 60

// pageJS is every script file joined, with a comment naming each. The name is
// there so that a stack trace or a line number read out of a browser can be
// traced back to a file, which one anonymous block of six files cannot be.
func pageJS() string {
	var block strings.Builder

	for _, name := range ScriptFiles {
		source, err := scripts.ReadFile(name)
		if err != nil {
			// Unreachable: the names are the embed pattern's own, so a missing
			// one fails the build. Panicking here beats serving a page whose
			// script is silently short of a panel.
			panic("devtools: " + name + " is not embedded: " + err.Error())
		}

		block.WriteString("\n// ── " + name + " ")
		block.WriteString(strings.Repeat("─", fileRule))
		block.WriteString("\n")
		block.Write(source)
	}

	return block.String()
}

// The markers page.html carries where its stylesheet and its script go. The two
// are held in files of their own and stitched in here so that each can be named
// in the policy by the hash of its bytes: a document whose only script is one
// this binary hashed will not run anything else, whatever gets onto the page.
const (
	styleMarker  = "<!--style-->"
	scriptMarker = "<!--script-->"
)

// Page is the developer view as it goes on the wire: one HTML document that
// reaches nothing outside this origin, and the policy that holds it to that.
type Page struct {
	// Body is the whole document.
	Body []byte
	// CSP is the Content-Security-Policy header it is served with.
	CSP string
	// ETag identifies these bytes, which change only when the binary does.
	ETag string
}

// Rendered returns the page, assembled once. The document is fixed at compile
// time, so there is nothing here for a cache to go stale against.
//
//nolint:gochecknoglobals // a once-value over embedded bytes, read-only after init.
var Rendered = sync.OnceValue(func() Page {
	script := pageJS()

	body := strings.NewReplacer(
		styleMarker, "<style>"+pageCSS+"</style>",
		scriptMarker, "<script>"+script+"</script>",
	).Replace(pageHTML)

	return Page{
		Body: []byte(body),
		CSP:  policy(script),
		ETag: `"` + digest(body) + `"`,
	}
})

// policy is what the page may reach. Nothing leaves the origin, and the only
// script and the only stylesheet are named by content: default-src 'none' means
// an addition to either file that was not rebuilt into this binary is refused by
// the browser rather than run.
//
// connect-src is what the live wire needs. EventSource and fetch are both
// governed by it, and both go to this origin alone.
func policy(script string) string {
	return strings.Join([]string{
		"default-src 'none'",
		"base-uri 'none'",
		"connect-src 'self'",
		"img-src 'self' data:",
		"script-src '" + digestSource(script) + "'",
		"style-src '" + digestSource(pageCSS) + "'",
		"frame-ancestors 'none'",
		"form-action 'none'",
	}, "; ")
}

// digestSource is the sha256-<base64> form CSP names an inline block by, which
// is the raw digest and not the hex one an entity-tag uses.
func digestSource(body string) string {
	sum := sha256.Sum256([]byte(body))

	return "sha256-" + base64.StdEncoding.EncodeToString(sum[:])
}

func digest(body string) string {
	sum := sha256.Sum256([]byte(body))

	return base64.RawURLEncoding.EncodeToString(sum[:])
}

package scaffold

import (
	"bytes"
	"io/fs"
	"os/exec"
	"path"
	"runtime"
	"strings"
	"testing"
	"text/template"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// TestEverySlotHasATemplate keeps a slot from being added to the table
// without a file make new-module can write for it.
func TestEverySlotHasATemplate(t *testing.T) {
	t.Parallel()

	for _, slot := range manifest.Slots() {
		c := Contribution{Slot: slot.Name, ID: "x", Title: "X"}

		_, err := fs.Stat(templates, "templates/slots/"+path.Base(c.File())+".tmpl")
		require.NoError(t, err, "slot %s has no template", slot.Name)
	}
}

// TestASlotTemplateReadsItsOwnContributionNotThePlans renders every
// templates/slots/*.tmpl with a slotContext whose Plan.Title and
// Contribution.Title differ. Plan and Contribution both have a Title field,
// so embedding both anonymously would make `{{.Title}}` an ambiguous
// selector that fails only the moment a template reads it, and a template
// that instead wrote `{{.Title}}` meaning the contribution's would silently
// read the plan's. Naming the Contribution field (`{{.Contribution.Title}}`)
// rules out both: this proves every shipped template still renders, and
// that the field a template addresses explicitly is the one it gets.
func TestASlotTemplateReadsItsOwnContributionNotThePlans(t *testing.T) {
	t.Parallel()

	data := slotContext{
		Plan:         Plan{Name: "shelf", Title: "Plan Title"},
		Contribution: Contribution{Slot: manifest.SlotPage, ID: "shelf", Title: "Contribution Title"},
	}

	entries, err := fs.ReadDir(templates, "templates/slots")
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	for _, entry := range entries {
		source := "templates/slots/" + entry.Name()

		t.Run(entry.Name(), func(t *testing.T) {
			t.Parallel()

			_, renderErr := Render(templates, source, data)
			require.NoError(t, renderErr, "%s did not render with a slotContext", source)
		})
	}

	parsed, err := template.New("t").Parse("{{.Name}}/{{.Contribution.Title}}")
	require.NoError(t, err)

	var out bytes.Buffer
	require.NoError(t, parsed.Execute(&out, data))
	require.Equal(t, "shelf/Contribution Title", out.String(),
		".Contribution.Title read the plan's title instead of the contribution's")
}

// TestEverySlotChangesTheGeneratedDocs keeps a slot from being added to the
// table without README.md, CHANGELOG.md and the locales file having
// something to say about it: it renders each against a plan with no
// contributions and again with one contribution of the slot under test, and
// requires the two to differ. A slot handled by a branch the template does
// not carry renders identically to the empty plan and fails here.
func TestEverySlotChangesTheGeneratedDocs(t *testing.T) {
	t.Parallel()

	templatesToCheck := []string{
		"templates/README.md.tmpl",
		"templates/CHANGELOG.md.tmpl",
		"templates/ui-locales-en.json.tmpl",
	}

	base := Plan{Name: "shelf", Title: "Shelf", UI: true}

	for _, tmpl := range templatesToCheck {
		t.Run(tmpl, func(t *testing.T) {
			t.Parallel()

			empty, err := Render(templates, tmpl, base)
			require.NoError(t, err)

			for _, slot := range manifest.Slots() {
				t.Run(string(slot.Name), func(t *testing.T) {
					t.Parallel()

					plan := base
					plan.Contributions = []Contribution{
						{Slot: slot.Name, ID: "x", Title: "X"},
					}

					filled, renderErr := Render(templates, tmpl, plan)
					require.NoError(t, renderErr)
					require.NotEqual(t, string(empty), string(filled),
						"%s says nothing new for slot %s", tmpl, slot.Name)
				})
			}
		})
	}
}

// TestInstallShReadsACheckoutPathWithASpace. A checkout under a folder such as
// "My Projects" is common on macOS and Windows, and an unquoted path there is
// two words to the shell.
func TestInstallShReadsACheckoutPathWithASpace(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("install.sh is a POSIX shell script")
	}

	checkout := "/home/you/My Projects/it's here"

	body, err := Render(templates, "templates/install.sh.tmpl", Plan{Name: "shelf", Checkout: checkout})
	require.NoError(t, err)

	var line string

	for candidate := range strings.Lines(string(body)) {
		if strings.HasPrefix(candidate, "CHECKOUT=") {
			line = candidate
		}
	}

	require.NotEmpty(t, line, "install.sh sets no CHECKOUT")

	said, err := exec.CommandContext(t.Context(), "sh", "-c", line+`printf %s "$CHECKOUT"`).CombinedOutput()
	require.NoError(t, err, "%s", said)
	require.Equal(t, checkout, string(said))
}

// TestScaffoldWritesMemoryNeed. The figure lands in needs.memory_mib where the
// manifest parser reads it, and with none the knob is written as a comment.
func TestScaffoldWritesMemoryNeed(t *testing.T) {
	t.Parallel()

	render := func(memory int) (string, manifest.Manifest) {
		plan := Plan{
			Name: "shelf", Title: "Shelf", Namespace: "com.github.aux.acme.shelf", Kind: "sink", Version: "0.1.0",
			Library: "^1.0.0", Process: true, MemoryMiB: memory,
		}

		document, err := Render(templates, "templates/module.yaml.tmpl", plan)
		require.NoError(t, err)

		held, err := manifest.Parse("module.yaml", document, yaml.Unmarshal)
		require.NoError(t, err)

		return string(document), held
	}

	_, held := render(512)
	require.Equal(t, 512, held.Needs.MemoryMiB)

	text, held := render(0)
	require.Zero(t, held.Needs.MemoryMiB)
	require.Contains(t, text, "# needs:\n#   memory_mib: 256")
}

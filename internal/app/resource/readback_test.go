package resource_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The regression these three hold is one measured deployment. A file declaring
// `auth: {idp: {type: core/oidc}}` and no core.auth block started, reported
// `state=ready serving=true` on /api/v1/resources, and answered
// GET /api/v1/principal with 200 and user:operator to a request carrying no
// credential. Nothing anywhere read the instance back, so the identity provider
// authenticated nobody while reporting that it was serving.
//
// auth has a reader since the bearer path, so the cases below name driver, the
// kind that is still read by nothing. The kinds nothing reads are
// ref.UnreadKinds, and internal/arch holds that list against the call sites
// that do the reading. These cases hold the other half: a declaration of one
// of them is refused at every door it can arrive at.

// TestADeclarationOfAKindNothingReadsBackFailsThatInstanceAlone is the
// operator's file. The refusal takes the instance and nothing else, because a
// server that will not start over one block is the outage the refusal exists to
// prevent.
func TestADeclarationOfAKindNothingReadsBackFailsThatInstanceAlone(t *testing.T) {
	t.Parallel()

	doc, err := load(t, `
resources:
  storage:
    local:
      type: filesystem
      config:
        root: `+storageRoot(t)+`
  driver:
    bench:
      type: core/xbuddy
      config:
        listen: 127.0.0.1:0
`)
	require.NoError(t, err, "one refused instance took the whole document with it")

	require.Equal(t, []ref.Ref{refOf(t, ref.KindStorage, "local")}, refsOf(doc),
		"a driver instance nothing reads back must not reach the supervision plane")

	require.Len(t, doc.Failures, 1)
	require.Equal(t, "resources.driver.bench", doc.Failures[0].Key)
	require.ErrorIs(t, doc.Failures[0].Err, resource.ErrConfigInvalid)
	require.Contains(t, doc.Failures[0].Err.Error(), "reads back an instance of kind driver",
		"the refusal has to say why, or the operator reads it as a typo")
}

// TestAnOverlayEntryOfAKindNothingReadsBackIsDropped is the machine-owned file.
// An overlay written by a binary that accepted the kind still holds the entry,
// so refusing at the API alone would leave the defect in the deployments that
// already have it.
func TestAnOverlayEntryOfAKindNothingReadsBackIsDropped(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	h.writeOverlayByHand(`{
  "version": 1,
  "instances": [
    {"kind": "driver", "name": "bench", "type": "core/xbuddy", "enabled": true, "required": false,
     "config": {"listen": "127.0.0.1:0"}}
  ]
}
`)

	doc, err := resource.Load(h.paths)
	require.NoError(t, err, "an entry an older binary wrote must not stop this one starting")

	require.Empty(t, refsOf(doc), "a dropped entry must not reach the supervision plane")
	require.Len(t, doc.Failures, 1)
	require.Equal(t, "resources.driver.bench", doc.Failures[0].Key)
	require.ErrorIs(t, doc.Failures[0].Err, resource.ErrConfigInvalid)
}

// TestTheResourceAPIRefusesAKindNothingReadsBack is the third door. The write
// surface names the kinds it does accept, so an operator reading the refusal
// knows what to write instead.
func TestTheResourceAPIRefusesAKindNothingReadsBack(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	_, err := h.create(mustRef(t, ref.KindDriver, "bench"), spec("core/xbuddy", `{"listen":"127.0.0.1:0"}`))
	require.Error(t, err, "the API must not write an instance the loader would refuse")
	require.True(t, fault.HasCode(err, httpfault.CodeRequestInvalid()), "want a request refusal, got %v", err)
	require.Contains(t, err.Error(), "reads back an instance of kind driver")

	var refused *fault.Fault

	require.ErrorAs(t, err, &refused)

	reason, carried := refused.Params().Get("reason")
	require.True(t, carried, "the refusal carries no reason, so a client cannot say what is legal")

	for _, kind := range ref.ReadKinds() {
		require.Contains(t, reason.String(), kind.String(), "the refusal has to name the kinds that work")
	}
}

// TestAModuleDeclarationProjectsWhatItsManifestSaid is the old backlog's read
// side. A card in the extensions screen needs the release, one line about the
// module and who answers for it, and the listing is where it reads them.
//
// The switch is off on purpose. A module an operator has just switched off is
// still on the page, and a card with no name and no version beside it is not
// one anybody can act on.
func TestAModuleDeclarationProjectsWhatItsManifestSaid(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	h.writeOverlayByHand(`{
  "version": 1,
  "instances": [
    {"kind": "asset", "name": "spools", "type": "com.github.aux.acme.spools/spools",
     "enabled": false, "required": false, "runtime": "asset",
     "asset": {"path": "/var/lib/prusa/modules/spools/ui",
               "checksum": "sha256:` + strings.Repeat("a", 64) + `"},
     "module": {"version": "1.4.0", "summary": "Counts what each print used.", "author": "acme"}}
  ]
}
`)

	require.NoError(t, h.store.Reload(t.Context()))

	instance, err := h.get(mustRef(t, ref.KindAsset, "spools"))
	require.NoError(t, err)

	require.NotNil(t, instance.Module, "the row carries nothing a card could print")
	require.Equal(t, "1.4.0", instance.Module.Version)
	require.Equal(t, "Counts what each print used.", instance.Module.Summary)
	require.Equal(t, "acme", instance.Module.Author)
}

// TestAnInstalledModuleCanBeSwitchedOnThroughTheAPI is the write the extensions
// page's switch makes. The install command writes the declaration and this API
// writes the one field a person can move, so the two rules that judge a type
// are asked against the runtime the declaration has rather than against
// builtin. Asked against builtin, every module's own row was readable and never
// writable, which is the whole page refused with 422.
//
// The artefact is checked afterwards because the body carries none. A write
// that rebuilt the entry from the body alone would drop the directory the
// bundle is served from, and the module would answer nothing at the next start.
func TestAnInstalledModuleCanBeSwitchedOnThroughTheAPI(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	checksum := "sha256:" + strings.Repeat("a", 64)

	h.writeOverlayByHand(`{
  "version": 1,
  "instances": [
    {"kind": "asset", "name": "spools", "type": "com.github.aux.acme.spools/spools",
     "enabled": false, "required": false, "runtime": "asset",
     "asset": {"path": "/var/lib/prusa/modules/spools/ui", "checksum": "` + checksum + `"},
     "module": {"version": "1.4.0", "summary": "Counts what each print used.", "author": "acme"}}
  ]
}
`)

	require.NoError(t, h.store.Reload(t.Context()))

	instance := mustRef(t, ref.KindAsset, "spools")

	read, err := h.get(instance)
	require.NoError(t, err)
	require.False(t, read.Enabled)

	on := read.ResourceSpec
	on.Enabled = true

	written, err := h.update(instance, on, h.version())
	require.NoError(t, err)
	require.True(t, written.Instance.Enabled)

	doc, err := resource.Load(h.paths)
	require.NoError(t, err)

	declared, found := doc.Find(instance)
	require.True(t, found)
	require.True(t, declared.Spec.Enabled)
	require.NotNil(t, declared.Spec.Asset, "the write dropped the directory the bundle is served from")
	require.Equal(t, checksum, declared.Spec.Asset.Checksum)
	require.NotNil(t, declared.Spec.Module, "the write dropped what the card prints")
}

// TestAnInstanceThatIsNoModuleProjectsNoModuleBlock keeps the key absent where
// there is nothing to say.
func TestAnInstanceThatIsNoModuleProjectsNoModuleBlock(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	_, err := h.create(mustRef(t, ref.KindStorage, "local"),
		spec("filesystem", `{"root":"`+storageRoot(t)+`"}`))
	require.NoError(t, err)

	instance, err := h.get(mustRef(t, ref.KindStorage, "local"))
	require.NoError(t, err)
	require.Nil(t, instance.Module)
}

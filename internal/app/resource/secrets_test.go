package resource_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// sinkOptions is a type that declares one credential, one address a credential
// can be written into, and one key that is neither.
func sinkOptions() []extension.Option {
	return []extension.Option{
		{Key: "url", Type: extension.OptionURL, Summary: "The broker."},
		{Key: "username", Type: extension.OptionString, Summary: "The broker user."},
		{Key: "password", Type: extension.OptionSecret, Summary: "The broker password."},
		{Key: "routes", Type: extension.OptionList, Summary: "The rewrite rules."},
		{Key: "tls", Type: extension.OptionObject, Summary: "The transport settings."},
	}
}

// declaredTypes answers for one type name and refuses every other, so a test
// can drive both the known and the unknown arm.
type declaredTypes struct {
	name    string
	options []extension.Option
}

func (d declaredTypes) DeclaredOptions(_ ref.Kind, typeName string) ([]extension.Option, bool) {
	if typeName != d.name {
		return nil, false
	}

	return d.options, true
}

func TestRedactHidesEveryDeclaredCredential(t *testing.T) {
	t.Parallel()

	cases := map[string]struct{ written, want string }{
		"a credential at the top of the block": {
			written: `{"username":"bob","password":"hunter2"}`,
			want:    `{"username":"bob","password":"xxxxx"}`,
		},
		"a credential inside a nested block": {
			written: `{"tls":{"password":"hunter2","insecure":true}}`,
			want:    `{"tls":{"password":"xxxxx","insecure":true}}`,
		},
		"a credential inside a block in a list": {
			written: `{"routes":[{"to":"a"},{"to":"b","password":"hunter2"}]}`,
			want:    `{"routes":[{"to":"a"},{"to":"b","password":"xxxxx"}]}`,
		},
		"a password written into a declared address": {
			written: `{"url":"amqps://bob:hunter2@broker:5671/vhost"}`,
			want:    `{"url":"amqps://bob:xxxxx@broker:5671/vhost"}`,
		},
		"an address with no password is returned as it is": {
			written: `{"url":"tcp://broker:1883"}`,
			want:    `{"url":"tcp://broker:1883"}`,
		},
		"a block that will not parse answers as an empty one": {
			written: `{"password":`,
			want:    `{}`,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := resource.Redact(sinkOptions(), true, json.RawMessage(tc.written))
			require.JSONEq(t, tc.want, string(got))
			require.NotContains(t, string(got), "hunter2")
		})
	}
}

func TestRestoreKeepsTheStoredCredentialBehindTheMarker(t *testing.T) {
	t.Parallel()

	cases := map[string]struct{ written, stored, want string }{
		"the marker written back for an untouched credential": {
			written: `{"username":"alice","password":"xxxxx"}`,
			stored:  `{"username":"bob","password":"hunter2"}`,
			want:    `{"username":"alice","password":"hunter2"}`,
		},
		"a new credential replaces the stored one": {
			written: `{"password":"opensesame"}`,
			stored:  `{"password":"hunter2"}`,
			want:    `{"password":"opensesame"}`,
		},
		"the marker inside a nested block": {
			written: `{"tls":{"password":"xxxxx","insecure":false}}`,
			stored:  `{"tls":{"password":"hunter2","insecure":true}}`,
			want:    `{"tls":{"password":"hunter2","insecure":false}}`,
		},
		"the marker in an address keeps the stored password": {
			written: `{"url":"amqps://bob:xxxxx@broker:5671/other"}`,
			stored:  `{"url":"amqps://bob:hunter2@broker:5671/vhost"}`,
			want:    `{"url":"amqps://bob:hunter2@broker:5671/other"}`,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := resource.Restore(sinkOptions(), true, json.RawMessage(tc.written), json.RawMessage(tc.stored))
			require.NoError(t, err)
			require.JSONEq(t, tc.want, string(got))
		})
	}
}

func TestRestoreRefusesTheMarkerWhenNothingIsStored(t *testing.T) {
	t.Parallel()

	_, err := resource.Restore(sinkOptions(), true, json.RawMessage(`{"password":"xxxxx"}`), nil)
	require.ErrorIs(t, err, resource.ErrSecretUnknown)
}

// TestAReadOfABuiltinHidesItsCredentialAndAWriteKeepsIt is the round trip an
// operator makes: read the instance, change one key, write the whole block
// back. The credential they never saw survives it.
func TestAReadOfABuiltinHidesItsCredentialAndAWriteKeepsIt(t *testing.T) {
	t.Parallel()

	h := newHarness(t, func(cfg *resource.Config) {
		cfg.Declared = declaredTypes{name: "mqtt", options: sinkOptions()}
	})

	instance := mustRef(t, ref.KindSink, "broker")

	created, err := h.create(instance, spec("mqtt", `{"username":"bob","password":"hunter2"}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"password":"xxxxx","username":"bob"}`, string(created.Instance.Config))

	read, err := h.get(instance)
	require.NoError(t, err)
	require.NotContains(t, string(read.Config), "hunter2")

	// The client writes back what it read, with one key changed.
	back := spec("mqtt", `{"username":"alice","password":"xxxxx"}`)

	written, err := h.update(instance, back, h.version())
	require.NoError(t, err)
	require.JSONEq(t, `{"password":"xxxxx","username":"alice"}`, string(written.Instance.Config))

	doc := h.store.Document()

	declared, found := doc.Find(instance)
	require.True(t, found)
	require.JSONEq(t, `{"username":"alice","password":"hunter2"}`, string(declared.Spec.Config),
		"the stored credential must survive a write that sent the marker")
}

// TestATypeThisBuildDoesNotHaveHidesEveryValue: an option list nobody can
// produce is not a reason to hand the block over. The keys stay, so an operator
// still reads which settings they wrote.
func TestATypeThisBuildDoesNotHaveHidesEveryValue(t *testing.T) {
	t.Parallel()

	got := resource.Redact(nil, false, json.RawMessage(`{"url":"tcp://b:1883","tls":{"password":"hunter2"}}`))
	require.JSONEq(t, `{"url":"xxxxx","tls":{"password":"xxxxx"}}`, string(got))
}

// TestATypeThisBuildDoesNotHaveKeepsWhatTheMarkerCovers: a write against such
// an instance sends the marker for every value, and every one of them keeps
// what is stored.
func TestATypeThisBuildDoesNotHaveKeepsWhatTheMarkerCovers(t *testing.T) {
	t.Parallel()

	got, err := resource.Restore(nil, false,
		json.RawMessage(`{"url":"xxxxx","password":"xxxxx"}`),
		json.RawMessage(`{"url":"tcp://b:1883","password":"hunter2"}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"url":"tcp://b:1883","password":"hunter2"}`, string(got))
}

// TestRestoreRefusesTheMarkerInsideAnAddressWhenNothingIsStored: an address
// carrying xxxxx as its password is one the instance cannot connect with, and
// the caller pasted it back from a read rather than choosing it.
func TestRestoreRefusesTheMarkerInsideAnAddressWhenNothingIsStored(t *testing.T) {
	t.Parallel()

	_, err := resource.Restore(sinkOptions(), true,
		json.RawMessage(`{"url":"amqps://bob:xxxxx@broker:5671/vhost"}`), nil)
	require.ErrorIs(t, err, resource.ErrSecretUnknown)
}

// TestANewInstanceOfATypeThisBuildDoesNotHaveIsRefusedAtTheWrite is decision
// G5 for a misspelt type. The caller is told which field is wrong, and the
// overlay is not touched, so nothing is left to fail at its first start.
func TestANewInstanceOfATypeThisBuildDoesNotHaveIsRefusedAtTheWrite(t *testing.T) {
	t.Parallel()

	h := newHarness(t, func(cfg *resource.Config) {
		cfg.Declared = declaredTypes{name: "mqtt", options: sinkOptions()}
	})

	before := h.version()

	_, err := h.create(mustRef(t, ref.KindSink, "broker"), spec("nope", `{}`))
	require.True(t, fault.HasCode(err, httpfault.CodeRequestInvalid()), "got %v", err)
	require.ErrorContains(t, err, `this build has no sink type named "nope"`)
	require.Equal(t, before, h.version(), "a refused write leaves the overlay as it was")

	_, err = h.create(mustRef(t, ref.KindSink, "broker"), spec("mqtt", `{}`))
	require.NoError(t, err, "a type this build has is written")
}

package files_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/files"
	"github.com/prusa3d/fractal-link/natsbind/joinwire"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

func TestSignProof(t *testing.T) {
	t.Parallel()

	signer, identity := newSigner(t)

	proof, err := files.SignProof(signer, time.Unix(1_800_000_000, 0))
	if err != nil {
		t.Fatalf("SignProof: %v", err)
	}

	parts := strings.Split(proof, ".")
	if len(parts) != 3 {
		t.Fatalf("the proof has %d parts, want 3", len(parts))
	}

	type decoded struct {
		Header  joinwire.Header
		Payload map[string]any
	}

	got := decoded{Header: decodePart[joinwire.Header](t, parts[0]), Payload: decodePart[map[string]any](t, parts[1])}
	want := decoded{
		Header:  joinwire.Header{Typ: joinwire.ProofTyp, Alg: joinwire.AlgEdDSA, NKey: string(identity)},
		Payload: map[string]any{"iat": float64(1_800_000_000)},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("the proof is %+v, want %+v", got, want)
	}
}

func TestVerifyProof(t *testing.T) {
	t.Parallel()

	signer, identity := newSigner(t)
	now := time.Unix(1_800_000_000, 0)

	at := func(when time.Time) string { return fmt.Sprintf(`{"iat":%d}`, when.Unix()) }
	signed := func(typ, payload string) string {
		compact, err := joinwire.SignCompact(typ, []byte(payload), signer)
		if err != nil {
			t.Fatalf("sign: %v", err)
		}

		return string(compact)
	}

	made, err := files.SignProof(signer, now)
	if err != nil {
		t.Fatalf("SignProof: %v", err)
	}

	zeroSignature := base64.RawURLEncoding.EncodeToString(make([]byte, 64))
	proofHeader := joinwire.Header{Typ: joinwire.ProofTyp, Alg: joinwire.AlgEdDSA, NKey: string(identity)}

	type outcome struct {
		Identity fractal.Identity
		Err      error
	}

	accepted := outcome{Identity: identity}
	refused := func(err error) outcome { return outcome{Err: err} }

	for name, tc := range map[string]struct {
		header string
		want   outcome
	}{
		"made by SignProof":            {made, accepted},
		"at the tolerance":             {signed(joinwire.ProofTyp, at(now.Add(-time.Minute))), accepted},
		"iat written with an exponent": {signed(joinwire.ProofTyp, `{"iat":1.8e9}`), accepted},
		"too old":                      {signed(joinwire.ProofTyp, at(now.Add(-61*time.Second))), refused(files.ErrProof)},
		"from the future":              {signed(joinwire.ProofTyp, at(now.Add(61*time.Second))), refused(files.ErrProof)},
		"iat with a fraction":          {signed(joinwire.ProofTyp, `{"iat":1800000000.5}`), refused(files.ErrProof)},
		"iat as text":                  {signed(joinwire.ProofTyp, `{"iat":"1800000000"}`), refused(files.ErrProof)},
		"no iat":                       {signed(joinwire.ProofTyp, `{}`), refused(files.ErrProof)},
		"not an object":                {signed(joinwire.ProofTyp, `[1800000000]`), refused(files.ErrProof)},
		"null":                         {signed(joinwire.ProofTyp, `null`), refused(files.ErrProof)},
		"a join request":               {signed(joinwire.JoinTyp, at(now)), refused(joinwire.ErrBadType)},
		"another algorithm": {
			forge(t, joinwire.Header{Typ: joinwire.ProofTyp, Alg: "none", NKey: string(identity)}, at(now), ""),
			refused(joinwire.ErrBadAlgorithm),
		},
		"a forged signature":   {forge(t, proofHeader, at(now), zeroSignature), refused(joinwire.ErrBadSignature)},
		"a forged bad payload": {forge(t, proofHeader, "not json", zeroSignature), refused(joinwire.ErrBadSignature)},
		"empty":                {"", refused(joinwire.ErrNotCompact)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, verifyErr := files.VerifyProof(tc.header, now, time.Minute)
			if result := (outcome{Identity: got, Err: sentinelOf(verifyErr)}); result != tc.want {
				t.Errorf("VerifyProof gave %+v, want %+v", result, tc.want)
			}
		})
	}
}

// sentinelOf returns the sentinel err matches, so a table compares errors whole.
func sentinelOf(err error) error {
	for _, known := range []error{
		files.ErrProof, joinwire.ErrBadType, joinwire.ErrBadAlgorithm,
		joinwire.ErrBadSignature, joinwire.ErrNotCompact,
	} {
		if errors.Is(err, known) {
			return known
		}
	}

	return err
}

// forge builds a compact JWS from parts nobody signed.
func forge(t *testing.T, header joinwire.Header, payload, signature string) string {
	t.Helper()

	encoded, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("encode the header: %v", err)
	}

	enc := base64.RawURLEncoding

	return enc.EncodeToString(encoded) + "." + enc.EncodeToString([]byte(payload)) + "." + signature
}

// decodePart decodes one base64url JSON part of a compact JWS.
func decodePart[T any](t *testing.T, part string) T {
	t.Helper()

	raw, err := base64.RawURLEncoding.DecodeString(part)
	if err != nil {
		t.Fatalf("decode a part of the proof: %v", err)
	}

	var out T
	if err = json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("parse a part of the proof: %v", err)
	}

	return out
}

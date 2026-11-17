package main

import (
	"errors"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// entryWithParams is one registered code reduced to what the key gate reads.
func entryWithParams(code string, params ...string) fault.Entry {
	parsed, err := fault.NewCode(fault.DomainValidation(), code)
	if err != nil {
		panic(err)
	}

	return fault.Entry{Code: parsed, Params: params}
}

func TestCheckParamKeysRefusesAKeyOutsideTheSet(t *testing.T) {
	t.Parallel()

	// Every published key is declared, so only the invented one is wrong and
	// the unused-key direction stays quiet.
	entries := []fault.Entry{entryWithParams("EVERY_KEY", fault.ParamKeys()...)}
	entries = append(entries, entryWithParams("INVENTED", "printer_name"))

	err := checkParamKeys(entries)
	if !errors.Is(err, errParamKeyUnpublished) {
		t.Fatalf("want a refusal naming the unpublished key, got %v", err)
	}

	if errors.Is(err, errParamKeyUnused) {
		t.Fatalf("every published key is declared here, so nothing is unused: %v", err)
	}
}

func TestCheckParamKeysRefusesAKeyNoCodeDeclares(t *testing.T) {
	t.Parallel()

	err := checkParamKeys([]fault.Entry{entryWithParams("ONE_KEY", "resource")})
	if !errors.Is(err, errParamKeyUnused) {
		t.Fatalf("want a refusal naming the keys no code declares, got %v", err)
	}
}

func TestCheckParamKeysAcceptsTheSetItself(t *testing.T) {
	t.Parallel()

	if err := checkParamKeys([]fault.Entry{entryWithParams("EVERY_KEY", fault.ParamKeys()...)}); err != nil {
		t.Fatalf("the published set has to pass its own gate: %v", err)
	}
}

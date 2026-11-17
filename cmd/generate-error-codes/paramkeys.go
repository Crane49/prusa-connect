package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The param keys are a closed set, checked in both directions.
var (
	// errParamKeyUnpublished means a code declares a key outside the set.
	errParamKeyUnpublished = errors.New("param key outside the published set")
	// errParamKeyUnused means no registered code declares a published key.
	errParamKeyUnused = errors.New("published param key that no code declares")
)

// checkParamKeys compares the keys the codes declare with fault.ParamKeys.
// Only declarations need checking: Fault.With drops an undeclared key.
func checkParamKeys(entries []fault.Entry) error {
	declared := map[string]bool{}

	var unpublished []string

	for _, entry := range entries {
		for _, key := range entry.Params {
			declared[key] = true

			if !fault.ParamKeyDeclared(key) {
				unpublished = append(unpublished, entry.Code.String()+" declares "+key)
			}
		}
	}

	var unused []string

	for _, key := range fault.ParamKeys() {
		if !declared[key] {
			unused = append(unused, key)
		}
	}

	return errors.Join(
		reportKeys(errParamKeyUnpublished, unpublished,
			"add the key to sdk/fault.paramKeys in this change, or use the key that already names that fact"),
		reportKeys(errParamKeyUnused, unused,
			"remove the key from sdk/fault.paramKeys, so the published set is what the codes carry"),
	)
}

func reportKeys(sentinel error, found []string, remedy string) error {
	if len(found) == 0 {
		return nil
	}

	slices.Sort(found)

	return fmt.Errorf("%w: %s\n  %s", sentinel, strings.Join(found, ", "), remedy)
}

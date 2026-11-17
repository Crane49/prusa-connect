package main

import (
	"errors"
	"fmt"

	"github.com/prusa3d/prusa-connect/internal/codegen"
)

// The committed registries a caller of the API branches on. Only the fields
// the tests of this package read are decoded.

type errorCodesFile struct {
	Codes []errorCode `json:"codes"`
}

type errorCode struct {
	Code      string `json:"code"`
	Status    int    `json:"status"`
	Summary   string `json:"summary"`
	Action    string `json:"action"`
	Retryable bool   `json:"retryable"`
}

type eventTypesFile struct {
	Version string      `json:"version"`
	Entries []eventType `json:"entries"`
}

type eventType struct {
	Type    string `json:"type"`
	Class   string `json:"class"`
	Summary string `json:"summary"`
}

type authzFile struct {
	Version   string          `json:"version"`
	Actions   []authzAction   `json:"actions"`
	Types     []authzType     `json:"types"`
	Relations []authzRelation `json:"relations"`
}

type authzAction struct {
	Action  string `json:"action"`
	Domain  string `json:"domain"`
	Summary string `json:"summary"`
}

type authzType struct {
	Type    string `json:"type"`
	Summary string `json:"summary"`
	Subject bool   `json:"subject"`
	Object  bool   `json:"object"`
}

type authzRelation struct {
	Relation string `json:"relation"`
	Summary  string `json:"summary"`
}

var errRegistryMissing = errors.New("a registry is not there")

// readRegistry decodes one committed registry. A missing file is an error,
// not an empty set.
func readRegistry[T any](path string) (*T, error) {
	file, err := codegen.ReadCatalogue[T](path)
	if err != nil {
		return nil, err
	}

	if file == nil {
		return nil, fmt.Errorf("%w: %s", errRegistryMissing, path)
	}

	return file, nil
}

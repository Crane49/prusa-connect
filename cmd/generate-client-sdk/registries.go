package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// The committed registries this command reads. Only the fields the package
// needs are decoded.

type errorCodesFile struct {
	Codes []errorCode `json:"codes"`
}

type errorCode struct {
	Code      string   `json:"code"`
	Status    int      `json:"status"`
	Summary   string   `json:"summary"`
	Action    string   `json:"action"`
	Retryable bool     `json:"retryable"`
	Params    []string `json:"params"`
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

var errEmptyRegistry = errors.New("a registry holds no entries")

// readRegistry decodes one committed registry. A missing file is an error,
// not an empty set.
func readRegistry[T any](path string) (*T, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var file T
	if err = json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}

	return &file, nil
}

// readVersion reads the core's VERSION file. The package version follows it.
func readVersion(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}

	version := strings.TrimSpace(string(data))
	if version == "" {
		return "", fmt.Errorf("%s is empty", path)
	}

	return version, nil
}

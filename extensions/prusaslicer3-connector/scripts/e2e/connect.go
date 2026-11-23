package main

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"reflect"
	"slices"
	"strings"
)

// The paths PrusaSlicer calls on Prusa Connect, under the module's mount.
const (
	pathPrinters = mount + "/connect/slicer/v2/printers"
	pathUploads  = mount + "/connect/app/users/teams/1/uploads"
	pathRaw      = mount + "/connect/app/teams/1/files/raw"
)

// smallFile is the size the refused upload request names.
const smallFile = 1024

// shape is what the list answers for a printer of a model: the fields of the
// cloud's legacy table and whether a tool carries an MMU.
type shape struct {
	legacy    string
	model     string
	baseModel string
	mmu       bool
}

// shapes are the demo's printers as the cloud would list them. The XL's
// precise name depends on its tool count, so only its prefix is held.
//
//nolint:gochecknoglobals // a fixed table, read-only.
var shapes = map[string]shape{
	modelCoreOne: {legacy: "COREONE", model: "COREONE", baseModel: "COREONE"},
	modelMK4S:    {legacy: "MK4SMMU3", model: "MK4S", baseModel: "MK4", mmu: true},
	modelXL:      {legacy: "XL", model: "XL", baseModel: "XL"},
}

// printers reads the list as each person and holds it to who may see what,
// then holds alice's list to the cloud's shape.
func (g *gate) printers(ctx context.Context) error {
	for _, template := range cast {
		listed, err := g.listFor(ctx, template.Username)
		if err != nil {
			return err
		}

		if err = g.holdVisible(template.Username, listed); err != nil {
			return err
		}

		if template.Username != "alice" {
			continue
		}

		for model, want := range shapes {
			if err = holdShape(model, listed[g.held.Printers[model]], want); err != nil {
				return err
			}
		}
	}

	return nil
}

// holdVisible holds the list one person reads to the printers they may see.
func (g *gate) holdVisible(username string, listed map[string]map[string]any) error {
	for model := range shapes {
		if _, has := listed[g.held.Printers[model]]; has != sees(username, model) {
			return fmt.Errorf("%s lists the %s: %t, want %t", username, model, has, !has)
		}
	}

	// Carol and dave see no printer beyond the ones granted, the demo's
	// other machine included.
	if (username == "carol" || username == "dave") && len(listed) != len(visible[username]) {
		return fmt.Errorf("%s lists %d printers: %v", username, len(listed), slices.Sorted(maps.Keys(listed)))
	}

	return nil
}

// listFor is the printer list one person reads, by uuid.
func (g *gate) listFor(ctx context.Context, username string) (map[string]map[string]any, error) {
	p, err := g.someone(username)
	if err != nil {
		return nil, err
	}

	got, err := g.bearing(p.Access).send(ctx, http.MethodGet, pathPrinters, "", nil)
	if err != nil {
		return nil, err
	}

	if err = expect("the printer list for "+username, got, http.StatusOK); err != nil {
		return nil, err
	}

	var list struct {
		Printers []map[string]any `json:"printers"`
	}

	if err = got.decode(&list); err != nil {
		return nil, err
	}

	out := map[string]map[string]any{}

	for _, printer := range list.Printers {
		id, _ := printer["uuid"].(string)
		if id == "" {
			return nil, fmt.Errorf("a printer in the list for %s has no uuid: %v", username, printer)
		}

		out[id] = printer
	}

	return out, nil
}

// holdShape holds one listed printer to the cloud's shape.
func holdShape(model string, printer map[string]any, want shape) error {
	if printer == nil {
		return fmt.Errorf("the list holds no %s", model)
	}

	state, _ := printer["connect_state"].(string)
	if state == "" || state == "OFFLINE" || state == "UNKNOWN" {
		return fmt.Errorf("the %s is listed %q, want the state of a connected printer", model, state)
	}

	legacy, _ := printer["legacy_printer_model"].(string)
	if !strings.HasPrefix(legacy, want.legacy) || printer["model"] != want.model ||
		printer["base_model"] != want.baseModel {
		return fmt.Errorf("the %s is listed as legacy %v, model %v, base_model %v; want %s, %s, %s",
			model, printer["legacy_printer_model"], printer["model"], printer["base_model"],
			want.legacy, want.model, want.baseModel)
	}

	tools, ok := printer["tools"].(map[string]any)
	if !ok || len(tools) == 0 {
		return fmt.Errorf("the %s lists no tools: %v", model, printer["tools"])
	}

	feeders := 0

	for key, tool := range tools {
		fields, _ := tool.(map[string]any)

		feeder, has := fields["feeder"]
		if !has {
			continue
		}

		feeders++

		if err := holdFeeder(model, key, feeder); err != nil {
			return err
		}
	}

	if (feeders > 0) != want.mmu {
		return fmt.Errorf("the %s lists %d tools with a feeder, want a feeder only on an MMU", model, feeders)
	}

	return nil
}

// holdFeeder holds a feeder to the cloud's MMU3, which PrusaSlicer needs the
// model of.
func holdFeeder(model, tool string, raw any) error {
	feeder, _ := raw.(map[string]any)
	slots, _ := feeder["num_slots"].(float64)

	if feeder["type"] != "MMU" || feeder["model"] != "MMU3" || feeder["base_model"] != "MMU3" || slots <= 0 {
		return fmt.Errorf("the %s lists tool %s with the feeder %v, want an MMU3 with slots", model, tool, raw)
	}

	return nil
}

// refusal is one upload the module refuses, and the body it answers.
type refusal struct {
	what   string
	who    string
	body   map[string]any
	status int
	// want is the whole body, or nil to hold the code and the reason alone.
	want   map[string]any
	code   string
	reason string
}

// register holds the upload request of a person who may not operate the
// printer to the cloud's refusal, before the slicer sends the file.
func (g *gate) register(ctx context.Context) error {
	coreOne, err := g.printer(modelCoreOne)
	if err != nil {
		return err
	}

	upload := func(changes map[string]any) map[string]any {
		body := map[string]any{
			"filename": "gate.bgcode", "size": smallFile, "printer_uuid": coreOne, "set_ready": true, "position": 0,
		}
		maps.Copy(body, changes)

		for key, value := range changes {
			if value == nil {
				delete(body, key)
			}
		}

		return body
	}

	xl, err := g.printer(modelXL)
	if err != nil {
		return err
	}

	// The refusals the cloud's rules make are the connect package's tests.
	// This one needs the host: the check of the person's grant on the
	// printer, which the operator approved at install. Carol may observe the
	// XL and not operate it.
	refusals := []refusal{
		{
			what: "a print by a person who may only observe the printer", who: "carol",
			body:   upload(map[string]any{"printer_uuid": xl}),
			status: http.StatusForbidden, want: map[string]any{
				"code": "FORBIDDEN", "message": "The server understood the request but refuses to authorize it",
			},
		},
	}

	for _, r := range refusals {
		if err = g.refused(ctx, r); err != nil {
			return err
		}
	}

	got, err := g.anonymous().post(ctx, pathUploads, upload(nil))
	if err != nil {
		return err
	}

	return expect("an upload with no credential", got, http.StatusUnauthorized)
}

func (g *gate) refused(ctx context.Context, r refusal) error {
	p, err := g.someone(r.who)
	if err != nil {
		return err
	}

	got, err := g.bearing(p.Access).post(ctx, pathUploads, r.body)
	if err != nil {
		return err
	}

	if err = expect(r.what, got, r.status); err != nil {
		return err
	}

	body, err := got.fields()
	if err != nil {
		return err
	}

	if r.want != nil {
		if !reflect.DeepEqual(body, r.want) {
			return fmt.Errorf("%s answered %s, want %v", r.what, got.body, r.want)
		}

		return nil
	}

	reason, _ := body["reason"].(string)
	if body["code"] != r.code || !strings.Contains(reason, r.reason) {
		return fmt.Errorf("%s answered %s, want %s with a reason naming %q", r.what, got.body, r.code, r.reason)
	}

	return nil
}

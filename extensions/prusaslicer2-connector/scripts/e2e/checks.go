package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
)

// unknownKeyLength is the length of the random part of a key this server
// never minted.
const unknownKeyLength = 70

// uploadBytes is the size of every file the gate sends: over the 1 MiB a
// request body carries without staging, so the staged route is what takes it.
const uploadBytes = 3 << 19

// gcode is a file of uploadBytes, as a slicer writes one.
func gcode() []byte {
	line := []byte("G1 X10 Y10 E0.5 ; the gate's print\n")

	return bytes.Repeat(line, uploadBytes/len(line)+1)[:uploadBytes]
}

// ready waits for the demo, its printers and the module.
func (g *gate) ready(ctx context.Context) error {
	err := poll(ctx, "the demo serves its health", func() (bool, error) {
		got, err := g.anonymous().send(ctx, http.MethodGet, "/api/v1/health", "", nil)

		return err == nil && got.status == http.StatusOK, err
	})
	if err != nil {
		return err
	}

	var admin *client

	err = poll(ctx, "the demo account signs in", func() (bool, error) {
		var signErr error
		admin, signErr = g.admin(ctx)

		return signErr == nil, signErr
	})
	if err != nil {
		return err
	}

	err = poll(ctx, "the printers connect", func() (bool, error) { return g.fleetConnected(ctx, admin) })
	if err != nil {
		return err
	}

	// The transfer listing the print checks read is kept by the history
	// store, which starts after the printers connect.
	err = poll(ctx, "the history store serves", func() (bool, error) { return historyServes(ctx, admin) })
	if err != nil {
		return err
	}

	mk4s, err := g.printer(modelMK4S)
	if err != nil {
		return err
	}

	// The page of a signed-in person reaches the module as the slicer does.
	return poll(ctx, "the module answers the version", func() (bool, error) {
		got, sendErr := admin.send(ctx, http.MethodGet, address(mk4s)+"api/version", "", nil)
		if sendErr != nil {
			return false, sendErr
		}

		if got.status != http.StatusOK {
			return false, fmt.Errorf("answered %d: %s", got.status, got.body)
		}

		return true, nil
	})
}

// historyServes reports whether the demo's history store, history/past,
// serves.
func historyServes(ctx context.Context, admin *client) (bool, error) {
	got, err := admin.send(ctx, http.MethodGet, "/api/v1/resources/history/past", "", nil)
	if err != nil {
		return false, err
	}

	var resource struct {
		Status struct {
			State   string `json:"state"`
			Serving bool   `json:"serving"`
		} `json:"status"`
	}

	if err = got.decode(&resource); err != nil {
		return false, err
	}

	if !resource.Status.Serving {
		return false, fmt.Errorf("the history store is %s", resource.Status.State)
	}

	return true, nil
}

// fleetConnected reads the devices and records the printers the checks use,
// once each is connected.
func (g *gate) fleetConnected(ctx context.Context, admin *client) (bool, error) {
	got, err := admin.send(ctx, http.MethodGet, "/api/v1/devices", "", nil)
	if err != nil {
		return false, err
	}

	var listing struct {
		Items []struct {
			ID        string `json:"id"`
			Model     string `json:"model"`
			Connected bool   `json:"connected"`
		} `json:"items"`
	}

	if err = got.decode(&listing); err != nil {
		return false, err
	}

	for _, device := range listing.Items {
		if device.Connected {
			g.held.Printers[device.Model] = device.ID
		}
	}

	for _, model := range []string{modelCoreOne, modelMK4S, modelXL} {
		if _, ok := g.held.Printers[model]; !ok {
			return false, fmt.Errorf("no connected %s among %s", model, got.body)
		}
	}

	return true, nil
}

// keys mints the two API credentials the person types into PrusaSlicer, the
// way the module's section on the printer page mints one.
func (g *gate) keys(ctx context.Context) error {
	admin, err := g.admin(ctx)
	if err != nil {
		return err
	}

	mint := func(name, scope string) (string, error) {
		got, postErr := admin.post(ctx, "/api/v1/auth/credentials",
			map[string]any{"name": name, "scopes": []string{scope}})
		if postErr != nil {
			return "", postErr
		}

		if postErr = expect("minting the "+scope+" key", got, http.StatusCreated); postErr != nil {
			return "", postErr
		}

		var minted struct {
			Token string `json:"token"`
		}

		if postErr = got.decode(&minted); postErr != nil {
			return "", postErr
		}

		if minted.Token == "" {
			return "", fmt.Errorf("minting the %s key answered no token: %s", scope, got.body)
		}

		return minted.Token, nil
	}

	if g.held.Operate, err = mint("PrusaSlicer 2 on the gate", "operate"); err != nil {
		return err
	}

	g.held.Read, err = mint("PrusaSlicer 2 that may only read", "read")

	return err
}

// link is PrusaSlicer's Test button and the storage list it reads before an
// upload.
func (g *gate) link(ctx context.Context) error {
	mk4s, err := g.printer(modelMK4S)
	if err != nil {
		return err
	}

	slicer := g.keyed(g.held.Operate)

	got, err := slicer.send(ctx, http.MethodGet, address(mk4s)+"api/version", "", nil)
	if err != nil {
		return err
	}

	if err = expect("the version", got, http.StatusOK); err != nil {
		return err
	}

	version, err := got.fields()
	if err != nil {
		return err
	}

	text, _ := version["text"].(string)
	if _, ok := version["api"].(string); !ok || !strings.HasPrefix(text, "PrusaLink") {
		return fmt.Errorf("PrusaSlicer refuses this version: %s", got.body)
	}

	if capabilities, _ := version["capabilities"].(map[string]any); capabilities["upload-by-put"] != true {
		return fmt.Errorf("the version does not offer PUT: %s", got.body)
	}

	got, err = slicer.send(ctx, http.MethodGet, address(mk4s)+"api/v1/storage", "", nil)
	if err != nil {
		return err
	}

	if err = expect("the storage list", got, http.StatusOK); err != nil {
		return err
	}

	// PrusaSlicer reads the first key of the object, and wants storage_list.
	listed := bytes.HasPrefix(got.body, []byte(`{"storage_list":[{`))
	if !listed || !bytes.Contains(got.body, []byte(`"path":"/local"`)) {
		return fmt.Errorf("the storage list is not one PrusaSlicer reads: %s", got.body)
	}

	for _, refused := range []struct {
		what   string
		client *client
		path   string
		status int
	}{
		{"the version with no API key", g.anonymous(), address(mk4s) + "api/version", http.StatusUnauthorized},
		{"the version with a key this server never minted", g.keyed("pcl_" + strings.Repeat("x", unknownKeyLength)), address(mk4s) + "api/version", http.StatusUnauthorized},
		{"the version of a printer this server does not have", slicer, address("no-such-printer") + "api/version", http.StatusNotFound},
	} {
		got, err = refused.client.send(ctx, http.MethodGet, refused.path, "", nil)
		if err != nil {
			return err
		}

		if err = expect(refused.what, got, refused.status); err != nil {
			return err
		}
	}

	return nil
}

// put sends a file the way PrusaSlicer 2.6 and later send one.
func (g *gate) put(ctx context.Context, key, printer, name string, startPrint bool) (reply, error) {
	headers := map[string]string{"Overwrite": "?1"}
	if startPrint {
		headers["Print-After-Upload"] = "?1"
	}

	return g.keyed(key).sendWith(ctx, http.MethodPut,
		address(printer)+"api/v1/files/local/"+url.PathEscape(name), "text/x.gcode", gcode(), headers)
}

// print sends a file with Print-After-Upload and waits until the printer has
// every byte of it.
func (g *gate) print(ctx context.Context) error {
	mk4s, err := g.printer(modelMK4S)
	if err != nil {
		return err
	}

	got, err := g.put(ctx, g.held.Operate, mk4s, "gate benchy.gcode", true)
	if err != nil {
		return err
	}

	if err = expect("Upload and Print", got, http.StatusCreated); err != nil {
		return err
	}

	return g.transferred(ctx, mk4s, "gate benchy.gcode")
}

// keep sends a file without Print-After-Upload. It lands on the printer and
// nothing starts.
func (g *gate) keep(ctx context.Context) error {
	xl, err := g.printer(modelXL)
	if err != nil {
		return err
	}

	got, err := g.put(ctx, g.held.Operate, xl, "gate kept.gcode", false)
	if err != nil {
		return err
	}

	if err = expect("Upload", got, http.StatusCreated); err != nil {
		return err
	}

	if err = g.transferred(ctx, xl, "gate kept.gcode"); err != nil {
		return err
	}

	state, err := g.deviceState(ctx, xl)
	if err != nil {
		return err
	}

	if state == "PRINTING" {
		return errors.New("an upload without a print started the file")
	}

	return nil
}

// form sends the form PrusaSlicer 2.5 and older send, with print=true.
func (g *gate) form(ctx context.Context) error {
	coreOne, err := g.printer(modelCoreOne)
	if err != nil {
		return err
	}

	var body bytes.Buffer

	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("print", "true")
	_ = writer.WriteField("path", "")

	part, err := writer.CreateFormFile("file", "gate form.gcode")
	if err != nil {
		return fmt.Errorf("build the form: %w", err)
	}

	_, _ = part.Write(gcode())
	_ = writer.Close()

	got, err := g.keyed(g.held.Operate).send(ctx, http.MethodPost,
		address(coreOne)+"api/files/local", writer.FormDataContentType(), body.Bytes())
	if err != nil {
		return err
	}

	if err = expect("the form upload", got, http.StatusCreated); err != nil {
		return err
	}

	return g.transferred(ctx, coreOne, "gate form.gcode")
}

// refused sends the uploads the module refuses.
func (g *gate) refused(ctx context.Context) error {
	xl, err := g.printer(modelXL)
	if err != nil {
		return err
	}

	mk4s, err := g.printer(modelMK4S)
	if err != nil {
		return err
	}

	got, err := g.put(ctx, g.held.Read, xl, "gate read.gcode", true)
	if err != nil {
		return err
	}

	if err = expect("Upload and Print with a key that may only read", got, http.StatusForbidden); err != nil {
		return err
	}

	got, err = g.put(ctx, g.held.Operate, xl, "gate model.stl", true)
	if err != nil {
		return err
	}

	if err = expect("a file a printer does not print", got, http.StatusUnsupportedMediaType); err != nil {
		return err
	}

	// The printer of the print check is printing now, or has just finished.
	// While it prints it refuses a second file with 409.
	err = poll(ctx, "the MK4S prints", func() (bool, error) {
		current, stateErr := g.deviceState(ctx, mk4s)

		return current == "PRINTING", stateErr
	})
	if err != nil {
		return err
	}

	got, err = g.put(ctx, g.held.Operate, mk4s, "gate busy.gcode", true)
	if err != nil {
		return err
	}

	return expect("Upload and Print to a printer that prints", got, http.StatusConflict)
}

// deviceState is the state a printer last reported, in upper case.
func (g *gate) deviceState(ctx context.Context, printer string) (string, error) {
	admin, err := g.admin(ctx)
	if err != nil {
		return "", err
	}

	got, err := admin.send(ctx, http.MethodGet, "/api/v1/devices/"+printer, "", nil)
	if err != nil {
		return "", err
	}

	var device struct {
		State map[string]any `json:"state"`
	}

	if err = got.decode(&device); err != nil {
		return "", err
	}

	current, _ := device.State["/state"].(string)

	return strings.ToUpper(current), nil
}

// transfers is the transfer listing of one printer.
func (g *gate) transfers(ctx context.Context, printer string) ([]transfer, string, error) {
	admin, err := g.admin(ctx)
	if err != nil {
		return nil, "", err
	}

	got, err := admin.send(ctx, http.MethodGet, "/api/v1/devices/"+printer+"/transfers", "", nil)
	if err != nil {
		return nil, "", err
	}

	var listing struct {
		Items []transfer `json:"items"`
	}

	if err = got.decode(&listing); err != nil {
		return nil, "", err
	}

	return listing.Items, string(got.body), nil
}

type transfer struct {
	Path       string `json:"path"`
	State      string `json:"state"`
	BytesTotal int64  `json:"bytes_total"`
	Reason     string `json:"reason"`
}

// transferred waits until the server's transfer of the file to the printer
// is done, with every byte.
func (g *gate) transferred(ctx context.Context, printer, name string) error {
	return poll(ctx, "the file reaches the printer", func() (bool, error) {
		items, body, err := g.transfers(ctx, printer)
		if err != nil {
			return false, err
		}

		for _, item := range items {
			if !strings.HasSuffix(item.Path, "/"+name) {
				continue
			}

			switch item.State {
			case "done":
				if item.BytesTotal != uploadBytes {
					return true, fmt.Errorf("the transfer carried %d bytes, want %d", item.BytesTotal, uploadBytes)
				}

				return true, nil
			case "failed", "cancelled", "interrupted":
				return true, fmt.Errorf("the transfer ended %s: %s", item.State, item.Reason)
			}

			return false, fmt.Errorf("the transfer is %s", item.State)
		}

		return false, fmt.Errorf("no transfer of %s among %s", name, body)
	})
}

package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// uploadBytes is the size of the file the gate prints, over the 1 MiB a route
// that is not staged takes.
const uploadBytes = 3 << 19

// upload prints a file over 1 MiB as bob, the way the slicer sends it: the
// upload request, then the file on the staged route. The transfer reaches the
// printer, and the printer list bob reads shows the CoreOne printing, which
// is how the slicer learns of it: it polls the list.
func (g *gate) upload(ctx context.Context) error {
	coreOne, err := g.printer(modelCoreOne)
	if err != nil {
		return err
	}

	bob, err := g.someone("bob")
	if err != nil {
		return err
	}

	name := fmt.Sprintf("gate-%d.bgcode", time.Now().UnixNano())

	id, err := g.registerUpload(ctx, bob, coreOne, name)
	if err != nil {
		return err
	}

	if err = g.sendFile(ctx, bob, id, name); err != nil {
		return err
	}

	if err = g.transferred(ctx, coreOne, name); err != nil {
		return err
	}

	return poll(ctx, "the printer list shows the CoreOne printing", func() (bool, error) {
		listed, listErr := g.listFor(ctx, "bob")
		if listErr != nil {
			return false, listErr
		}

		state, _ := listed[coreOne]["connect_state"].(string)
		if state == "PRINTING" {
			return true, nil
		}

		return false, errors.New("the CoreOne is listed " + state)
	})
}

func (g *gate) registerUpload(ctx context.Context, bob *person, printer, name string) (uint64, error) {
	got, err := g.bearing(bob.Access).post(ctx, pathUploads, map[string]any{
		"filename": name, "size": uploadBytes, "printer_uuid": printer, "set_ready": true, "position": 0,
	})
	if err != nil {
		return 0, err
	}

	if err = expect("the upload request", got, http.StatusOK); err != nil {
		return 0, err
	}

	var registered struct {
		ID    uint64 `json:"id"`
		State string `json:"state"`
		Size  int64  `json:"size"`
	}

	if err = got.decode(&registered); err != nil {
		return 0, err
	}

	if registered.ID == 0 || registered.State != "INITIATED" || registered.Size != uploadBytes {
		return 0, fmt.Errorf("the upload request answered %s", got.body)
	}

	return registered.ID, nil
}

// sendFile puts the file on the staged route, as bob.
func (g *gate) sendFile(ctx context.Context, bob *person, id uint64, name string) error {
	file := make([]byte, uploadBytes)
	copy(file, "GCDE")

	query := url.Values{"upload_id": {strconv.FormatUint(id, 10)}}.Encode()

	got, err := g.bearing(bob.Access).send(ctx, http.MethodPut, pathRaw+"?"+query, "application/octet-stream", file)
	if err != nil {
		return err
	}

	if err = expect("the file", got, http.StatusOK); err != nil {
		return err
	}

	var kept struct {
		Name     string `json:"name"`
		Size     int64  `json:"size"`
		UploadID uint64 `json:"upload_id"`
		Type     string `json:"type"`
	}

	if err = got.decode(&kept); err != nil {
		return err
	}

	if kept.Name != name || kept.Size != uploadBytes || kept.UploadID != id || kept.Type != "PRINT_FILE" {
		return fmt.Errorf("the file answered %s", got.body)
	}

	return nil
}

// transferred waits until the server's transfer of the file to the printer
// is done, with every byte.
func (g *gate) transferred(ctx context.Context, printer, name string) error {
	admin, err := g.admin(ctx)
	if err != nil {
		return err
	}

	return poll(ctx, "the file reaches the printer", func() (bool, error) {
		got, sendErr := admin.send(ctx, http.MethodGet, "/api/v1/devices/"+printer+"/transfers", "", nil)
		if sendErr != nil {
			return false, sendErr
		}

		var listing struct {
			Items []struct {
				Path       string `json:"path"`
				State      string `json:"state"`
				BytesTotal int64  `json:"bytes_total"`
				Reason     string `json:"reason"`
			} `json:"items"`
		}

		if readErr := got.decode(&listing); readErr != nil {
			return false, readErr
		}

		for _, item := range listing.Items {
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

		return false, fmt.Errorf("no transfer of %s among %s", name, got.body)
	})
}

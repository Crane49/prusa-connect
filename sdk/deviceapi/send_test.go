package deviceapi_test

import (
	"errors"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
)

// The host reads a send as the module wrote it, and the transfer comes back.
func TestTheHostReadsASendAsItWasWritten(t *testing.T) {
	t.Parallel()

	fleet := &fakeFleet{sent: deviceapi.Sent{TransferID: "01TRANSFER", Path: "usb/part[1].bgcode"}}

	params := deviceapi.SendParams{
		Principal: "user:alpha", ID: "p1",
		Storage: deviceapi.ModuleDataStorage, Source: "1-x/part.bgcode", Path: "usb/part.bgcode",
		KeepBoth: true, DeleteSource: true,
	}

	answered, err := call(t, deviceapi.Handlers(fleet), deviceapi.MethodSend, params)
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	if fleet.sawSend != params {
		t.Fatalf("the host was asked for %+v, want %+v", fleet.sawSend, params)
	}

	sent, ok := answered.(deviceapi.Sent)
	if !ok || sent.TransferID != "01TRANSFER" || sent.Path != "usb/part[1].bgcode" {
		t.Fatalf("the answer is %+v, want the transfer the host opened", answered)
	}
}

// A send moves a file from a storage, so one that names none is refused on
// both sides of the wire, as are the bounds a print is held to.
func TestASendNamingNoStorageIsRefused(t *testing.T) {
	t.Parallel()

	for name, params := range map[string]deviceapi.SendParams{
		"no storage":       {ID: "p1", Path: "usb/part.bgcode"},
		"half a source":    {ID: "p1", Storage: "bench", Path: "usb/part.bgcode"},
		"no path":          {ID: "p1", Storage: "bench", Source: "part.bgcode"},
		"delete another's": {ID: "p1", Storage: "bench", Source: "part.bgcode", Path: "usb/part.bgcode", DeleteSource: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := params.Validate(); !errors.Is(err, deviceapi.ErrInvalidQuery) {
				t.Fatalf("Validate = %v, want ErrInvalidQuery", err)
			}

			fleet := &fakeFleet{}
			if _, err := call(t, deviceapi.Handlers(fleet), deviceapi.MethodSend, params); err == nil {
				t.Fatal("the host took a send the contract refuses")
			}

			if fleet.sawSend != (deviceapi.SendParams{}) {
				t.Fatalf("the refused send reached the host as %+v", fleet.sawSend)
			}
		})
	}
}

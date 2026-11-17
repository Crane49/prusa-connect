package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// demoOwnerRelation is the tuple the demo writes on every printer it started.
//
// Ownership is what reaches a dangerous verb. Raw gcode is conferred by no
// role, no level and no class, so the only ways to it are a row naming the verb
// and ownership of the machine, and a row naming the verb cannot be written by
// somebody who does not already reach it. On a real deployment the owner is
// whoever approved the registration; the demo admits its printers with nobody
// approving them, so without this nothing in the demonstration can send a
// printer a line of gcode, which is half of what the developer view is for.
const demoOwnerRelation = "owner"

// claimPrinters makes the demo account the owner of every printer this run
// started, and returns what to say about it.
//
// It is written over the published grants API, as an operator would write it,
// rather than into the graph behind the API's back. The subject is read back
// from the server rather than assumed, because who a token resolves to is the
// identity layer's answer and not this command's.
//
// Nothing here is fatal. A demonstration whose printers all work and whose
// ownership was not written is still the demonstration, and the line it returns
// says which half is missing.
func (f *demoFleet) claimPrinters(ctx context.Context, opts DemoOptions, page demoListing) string {
	subject, err := f.principal(ctx, opts)
	if err != nil {
		return "nobody owns the printers, so raw gcode is refused: " + err.Error()
	}

	body := fmt.Sprintf(`{"relation":%q,"subject":%q}`, demoOwnerRelation, subject)

	for _, printer := range page.Items {
		url := opts.baseURL() + "/api/v1/grants/device/" + printer.ID

		status, writeErr := f.write(ctx, http.MethodPost, url, body, nil)
		if writeErr != nil {
			return "the printers could not be claimed: " + writeErr.Error()
		}

		if status != http.StatusCreated && status != http.StatusOK {
			return fmt.Sprintf("claiming %s was refused with status %d", printer.Serial, status)
		}
	}

	return "owns every printer, so raw gcode and the other dangerous verbs are reachable"
}

// principal is the subject the demo's own token resolves to. It is one read of
// the endpoint any client uses to find out who it is signed in as.
func (f *demoFleet) principal(ctx context.Context, opts DemoOptions) (string, error) {
	var who struct {
		Principal string `json:"principal"`
	}

	status, err := f.call(ctx, http.MethodGet, opts.baseURL()+"/api/v1/principal", "", &who)
	if err != nil {
		return "", err
	}

	if status != http.StatusOK {
		return "", fmt.Errorf("the principal endpoint answered with status %d", status)
	}

	if who.Principal == "" {
		return "", errors.New("the principal endpoint named nobody")
	}

	return who.Principal, nil
}

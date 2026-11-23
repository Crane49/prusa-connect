package main

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// The me reply PrusaSlicer reads from Prusa Account, under the module's
// mount.
const pathMe = mount + "/account/api/v1/me/"

// avatarSize is the width and the height of the avatar, in pixels.
const avatarSize = 128

// The demo's printers the gate reads, by the model the demo reports. The
// demo fits an MMU to its MK4S.
const (
	modelCoreOne = "CoreOne"
	modelMK4S    = "MK4S"
	modelXL      = "XL"
)

// cast are the people the gate adds. Alice views every printer and bob
// operates every printer, by their org roles. Carol may observe the XL alone,
// and dave no printer.
//
// Carol and dave are also granted operate on the module's kind, as they hold
// no role. The me reply needs view_resource on the module, and the upload
// request operate_resource.
//
//nolint:gochecknoglobals // a fixed list, read-only.
var cast = []person{
	{Username: "alice", Password: "alice-pass-123", Display: "Alice (viewer)"},
	{Username: "bob", Password: "bob-pass-123", Display: "Bob (operator)"},
	{Username: "carol", Password: "carol-pass-123", Display: "Carol (one printer)"},
	{Username: "dave", Password: "dave-pass-123", Display: "Dave (no printer)"},
}

// roles are the org roles of the people that hold one.
//
//nolint:gochecknoglobals // a fixed table, read-only.
var roles = map[string]string{"alice": "viewer", "bob": "operator"}

// ready waits until the demo serves, the demo account signs in, the three
// printers the checks use are connected, the history store serves, and the
// module answers. The upload check reads the transfer back from the history
// store, and the demo attaches it only after it has written its sample prints,
// so a transfer that ended before then would never be listed.
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

	err = poll(ctx, "the history store serves", func() (bool, error) { return historyServes(ctx, admin) })
	if err != nil {
		return err
	}

	return poll(ctx, "the module answers the printer list", func() (bool, error) {
		got, sendErr := admin.send(ctx, http.MethodGet, pathPrinters, "", nil)
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

// people adds the four people and their grants.
func (g *gate) people(ctx context.Context) error {
	admin, err := g.admin(ctx)
	if err != nil {
		return err
	}

	for _, template := range cast {
		added := template

		body := map[string]string{
			"username": added.Username, "password": added.Password, "display_name": added.Display,
		}
		if role, ok := roles[added.Username]; ok {
			body["role"] = role
		}

		got, addErr := admin.post(ctx, "/api/v1/auth/accounts", body)
		if addErr != nil {
			return addErr
		}

		if addErr = expect("adding "+added.Username, got, http.StatusCreated); addErr != nil {
			return addErr
		}

		var account struct {
			Subject string `json:"subject"`
		}

		if addErr = got.decode(&account); addErr != nil {
			return addErr
		}

		if account.Subject == "" {
			return fmt.Errorf("adding %s answered no subject: %s", added.Username, got.body)
		}

		added.Subject = account.Subject
		g.held.People[added.Username] = &added
	}

	xl, err := g.printer(modelXL)
	if err != nil {
		return err
	}

	for _, grant := range [][4]string{
		{"device", xl, "viewer", g.held.People["carol"].Subject},
		{"resource_kind", "sink", "operator", g.held.People["carol"].Subject},
		{"resource_kind", "sink", "operator", g.held.People["dave"].Subject},
	} {
		got, grantErr := admin.post(ctx, "/api/v1/grants/"+grant[0]+"/"+grant[1], map[string]string{
			"object": grant[0] + ":" + grant[1], "relation": grant[2], "subject": grant[3],
		})
		if grantErr != nil {
			return grantErr
		}

		if got.status < http.StatusOK || got.status >= http.StatusMultipleChoices {
			return fmt.Errorf("granting %s %s on %s:%s answered %d: %s",
				grant[3], grant[2], grant[0], grant[1], got.status, got.body)
		}
	}

	return nil
}

// The app pairing of the core, which PrusaSlicer runs to sign in
// (ADR-20261025-tpp, ADR-20261112-nay).
const (
	pathPairings    = "/api/v1/auth/app-pairings"
	pathPairingPoll = "/api/v1/auth/app-pairings/token"
	pathPageSession = "/api/v1/auth/app-pairings/session"
	appName         = "PrusaSlicer on the gate"
	tokenCookie     = "auth.access_token" //nolint:gosec // a cookie name, not a credential
)

// signIn pairs the slicer for every person and reads the me reply with the
// credential it collected. Alice's credential then goes where the page inside
// PrusaSlicer keeps it, the auth.access_token cookie. The refusals of the
// pairing itself are the core's tests.
func (g *gate) signIn(ctx context.Context) error {
	for _, template := range cast {
		held, ok := g.held.People[template.Username]
		if !ok {
			return fmt.Errorf("%s was not added; run the people check first", template.Username)
		}

		if err := g.signInPerson(ctx, held); err != nil {
			return fmt.Errorf("%s: %w", held.Username, err)
		}
	}

	return g.inCookie(ctx, g.held.People["alice"])
}

// signInPerson is what the slicer does: it asks for a pairing with no
// credential, the person approves the code while signed in, the slicer
// collects the credential and reads the me reply with it.
func (g *gate) signInPerson(ctx context.Context, p *person) error {
	collected, err := g.pair(ctx, p)
	if err != nil {
		return err
	}

	p.Access, p.Bare = collected.AccessToken, collected.Token

	got, err := g.bearing(p.Access).send(ctx, http.MethodGet, pathMe, "", nil)
	if err != nil {
		return err
	}

	if err = expect("me", got, http.StatusOK); err != nil {
		return err
	}

	var me struct {
		ID             int64  `json:"id"`
		PublicUsername string `json:"public_username"`
		AvatarSmall    string `json:"avatar_small"`
	}

	if err = got.decode(&me); err != nil {
		return err
	}

	if me.ID <= 0 || me.PublicUsername != p.Display {
		return fmt.Errorf("me answered %s, want a positive id and the name %q", got.body, p.Display)
	}

	p.ID = me.ID

	return g.avatar(ctx, me.AvatarSmall)
}

// avatar resolves avatar_small against the me address, as PrusaSlicer does,
// and downloads it with no credential. The route is public, so the host
// passes the request on without one.
func (g *gate) avatar(ctx context.Context, ref string) error {
	relative, err := url.Parse(ref)
	if err != nil || ref == "" || relative.IsAbs() || strings.HasPrefix(ref, "/") {
		return fmt.Errorf("me answered avatar_small %q, want a reference relative to the me address", ref)
	}

	resolved := (&url.URL{Path: pathMe}).ResolveReference(relative)
	if resolved.Path != pathMe+"avatar.png" {
		return fmt.Errorf("avatar_small %q resolves to %s, want %savatar.png", ref, resolved, pathMe)
	}

	got, err := g.anonymous().send(ctx, http.MethodGet, resolved.String(), "", nil)
	if err != nil {
		return err
	}

	if err = expect("the avatar", got, http.StatusOK); err != nil {
		return err
	}

	picture, err := png.Decode(bytes.NewReader(got.body))
	if err != nil {
		return fmt.Errorf("the avatar is not a PNG: %w", err)
	}

	if bounds := picture.Bounds(); got.header.Get("Content-Type") != "image/png" || bounds.Dx() != avatarSize ||
		bounds.Dy() != avatarSize {
		return fmt.Errorf("the avatar is %s of %v, want image/png of %dx%d",
			got.header.Get("Content-Type"), bounds, avatarSize, avatarSize)
	}

	return nil
}

// collected is the answer of a poll that collects the credential.
type collected struct {
	ID          string   `json:"id"`
	Token       string   `json:"token"`
	AccessToken string   `json:"access_token"`
	Subject     string   `json:"subject"`
	Scopes      []string `json:"scopes"`
	Origin      string   `json:"origin"`
}

// pair runs one app pairing for a person, and holds the credential to what
// the person approved and to the JWT shape the page keeps.
func (g *gate) pair(ctx context.Context, p *person) (collected, error) {
	got, err := g.anonymous().post(ctx, pathPairings, map[string]any{
		"app_name": appName, "scopes": []string{"operate"},
	})
	if err != nil {
		return collected{}, err
	}

	if err = expect("the pairing request", got, http.StatusCreated); err != nil {
		return collected{}, err
	}

	var issued struct {
		DeviceCode string `json:"device_code"`
		UserCode   string `json:"user_code"`
	}

	if err = got.decode(&issued); err != nil {
		return collected{}, err
	}

	browser, err := g.signedIn(ctx, p.Username, p.Password)
	if err != nil {
		return collected{}, err
	}

	got, err = browser.send(ctx, http.MethodPost, pathPairings+"/"+url.PathEscape(issued.UserCode)+"/approve", "", nil)
	if err != nil {
		return collected{}, err
	}

	if err = expect("the approval", got, http.StatusNoContent); err != nil {
		return collected{}, err
	}

	poll := map[string]string{"device_code": issued.DeviceCode}

	got, err = g.anonymous().post(ctx, pathPairingPoll, poll)
	if err != nil {
		return collected{}, err
	}

	if err = expect("the collection", got, http.StatusOK); err != nil {
		return collected{}, err
	}

	var answer collected
	if err = got.decode(&answer); err != nil {
		return collected{}, err
	}

	segments := strings.Split(answer.AccessToken, ".")
	if answer.Subject != p.Subject || answer.Origin != "app_pairing" ||
		!slices.Equal(answer.Scopes, []string{"operate"}) || len(segments) != 3 || segments[2] != answer.Token {
		return collected{}, fmt.Errorf("the collection answered %s, want an operate credential of %s with the "+
			"token as the third segment of access_token", got.body, p.Subject)
	}

	// A device code collects once.
	got, err = g.anonymous().post(ctx, pathPairingPoll, poll)
	if err != nil {
		return collected{}, err
	}

	if err = expect("a second collection", got, http.StatusBadRequest); err != nil {
		return collected{}, err
	}

	if !strings.Contains(string(got.body), "APP_PAIRING_UNKNOWN") {
		return collected{}, fmt.Errorf("a second collection answered %s, want APP_PAIRING_UNKNOWN", got.body)
	}

	return answer, nil
}

// inCookie does what PrusaSlicer does before it shows a page: it opens a page
// session with its credential and puts the cookie the answer carries into the
// web view. The cookie alone then reads the print dialog's printer list and
// the core's devices. The credential itself in the auth.access_token cookie
// is refused, and the bare token still works as a bearer.
func (g *gate) inCookie(ctx context.Context, p *person) error {
	got, err := g.bearing(p.Access).send(ctx, http.MethodPost, pathPageSession, "", nil)
	if err != nil {
		return err
	}

	if err = expect("the page session", got, http.StatusOK); err != nil {
		return err
	}

	if err = g.holdPageCookie(got.header.Get("Set-Cookie")); err != nil {
		return err
	}

	var answer struct {
		Cookie struct {
			Name     string `json:"name"`
			Value    string `json:"value"`
			HTTPOnly bool   `json:"http_only"`
			SameSite string `json:"same_site"`
			Secure   bool   `json:"secure"`
		} `json:"cookie"`
	}

	if err = got.decode(&answer); err != nil {
		return err
	}

	cookie := answer.Cookie
	if cookie.Name != "pcl_session" || cookie.Value == "" || !cookie.HTTPOnly || cookie.SameSite != "Strict" ||
		cookie.Secure != strings.HasPrefix(g.base, "https:") ||
		!strings.Contains(got.header.Get("Set-Cookie"), cookie.Value) {
		return fmt.Errorf("the page session answered the cookie %s, which is not the one it set", got.body)
	}

	page := g.withCookie(cookie.Name, cookie.Value)

	for _, path := range []string{pathPrinters, "/api/v1/devices"} {
		got, err = page.send(ctx, http.MethodGet, path, "", nil)
		if err != nil {
			return err
		}

		if err = expect(path+" with the page session cookie", got, http.StatusOK); err != nil {
			return err
		}
	}

	got, err = g.withCookie(tokenCookie, p.Access).send(ctx, http.MethodGet, pathPrinters, "", nil)
	if err != nil {
		return err
	}

	err = expect("the printer list with the credential in auth.access_token", got, http.StatusUnauthorized)
	if err != nil {
		return err
	}

	got, err = g.bearing(p.Bare).send(ctx, http.MethodGet, pathMe, "", nil)
	if err != nil {
		return err
	}

	return expect("me with the bare token", got, http.StatusOK)
}

// holdPageCookie holds the page session cookie to HttpOnly and
// SameSite=Strict, and to Secure over HTTPS.
func (g *gate) holdPageCookie(raw string) error {
	lower := strings.ToLower(raw)
	secure := strings.HasPrefix(g.base, "https:")

	if !strings.HasPrefix(raw, "pcl_session=") || !strings.Contains(lower, "httponly") ||
		!strings.Contains(lower, "samesite=strict") || strings.Contains(lower, "secure") != secure {
		return fmt.Errorf("the page session cookie is %q, want pcl_session, HttpOnly, SameSite=Strict "+
			"and Secure exactly over HTTPS", raw)
	}

	return nil
}

// visible is who may observe which printer, by model.
//
//nolint:gochecknoglobals // a fixed table, read-only.
var visible = map[string][]string{
	"alice": {modelCoreOne, modelMK4S, modelXL},
	"bob":   {modelCoreOne, modelMK4S, modelXL},
	"carol": {modelXL},
	"dave":  {},
}

// sees reports whether a person may observe the printer of a model.
func sees(username, model string) bool {
	return slices.Contains(visible[username], model)
}

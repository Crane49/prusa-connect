package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"time"
)

// requestBudget bounds one request. The largest is the upload of the file.
const requestBudget = time.Minute

// maxReply bounds the body the gate reads.
const maxReply = 4 << 20

// reply is one answer of the demo.
type reply struct {
	status int
	body   []byte
	header http.Header
}

// decode reads the body as JSON into out.
func (r reply) decode(out any) error {
	if err := json.Unmarshal(r.body, out); err != nil {
		return fmt.Errorf("the reply %d is not JSON: %w: %s", r.status, err, r.body)
	}

	return nil
}

// fields reads the body as one JSON object.
func (r reply) fields() (map[string]any, error) {
	out := map[string]any{}

	return out, r.decode(&out)
}

// client is a browser of the demo: it keeps the cookies of a sign-in.
type client struct {
	gate *gate
	http *http.Client
	// bearer is the token sent in Authorization, empty for none.
	bearer string
	// cookie is sent with every request, nil for none.
	cookie *http.Cookie
}

// anonymous is a client with no credential.
func (g *gate) anonymous() *client {
	return &client{gate: g, http: &http.Client{Transport: g.transport, Timeout: requestBudget}}
}

// bearing is a client that sends a token, as the slicer does.
func (g *gate) bearing(token string) *client {
	c := g.anonymous()
	c.bearer = token

	return c
}

// withCookie is a client that sends one cookie, as the page inside
// PrusaSlicer does.
func (g *gate) withCookie(name, value string) *client {
	c := g.anonymous()
	c.cookie = &http.Cookie{Name: name, Value: value} //nolint:gosec // a request cookie, which carries no attribute

	return c
}

// cookies is a client that keeps the cookies it is answered with, as a page
// does.
func (g *gate) cookies() (*client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("a cookie jar: %w", err)
	}

	c := g.anonymous()
	c.http.Jar = jar

	return c, nil
}

// signedIn is a client holding the session of a person signed in on the
// demo's login page.
func (g *gate) signedIn(ctx context.Context, username, password string) (*client, error) {
	c, err := g.cookies()
	if err != nil {
		return nil, err
	}

	got, err := c.post(ctx, "/api/v1/auth/login",
		map[string]string{"username": username, "password": password})
	if err != nil {
		return nil, err
	}

	if got.status != http.StatusOK && got.status != http.StatusNoContent {
		return nil, fmt.Errorf("%s did not sign in: %d %s", username, got.status, got.body)
	}

	return c, nil
}

// admin is the demo account, signed in.
func (g *gate) admin(ctx context.Context) (*client, error) {
	return g.signedIn(ctx, "demo", g.password)
}

// post sends body as JSON.
func (c *client) post(ctx context.Context, path string, body any) (reply, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return reply{}, fmt.Errorf("encode the body of %s: %w", path, err)
	}

	return c.send(ctx, http.MethodPost, path, "application/json", raw)
}

// send makes one request to a path of the demo.
func (c *client) send(ctx context.Context, method, path, contentType string, body []byte) (reply, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	request, err := http.NewRequestWithContext(ctx, method, c.gate.base+path, reader)
	if err != nil {
		return reply{}, fmt.Errorf("%s %s: %w", method, path, err)
	}

	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}

	if c.bearer != "" {
		request.Header.Set("Authorization", "Bearer "+c.bearer)
	}

	if c.cookie != nil {
		request.AddCookie(c.cookie)
	}

	response, err := c.http.Do(request)
	if err != nil {
		return reply{}, fmt.Errorf("%s %s: %w", method, path, err)
	}

	defer func() { _ = response.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(response.Body, maxReply))
	if err != nil {
		return reply{}, fmt.Errorf("%s %s: read the reply: %w", method, path, err)
	}

	return reply{status: response.StatusCode, body: raw, header: response.Header}, nil
}

// expect fails unless the reply has the status.
func expect(what string, got reply, status int) error {
	if got.status != status {
		return fmt.Errorf("%s answered %d, want %d: %s", what, got.status, status, got.body)
	}

	return nil
}

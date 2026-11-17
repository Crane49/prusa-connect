package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"time"
)

// client speaks to the server under test the way the wizard's page does: one
// cookie jar, and a certificate the wizard made itself, which nothing trusts.
type client struct {
	http *http.Client
}

func newClient() (*client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("make a cookie jar: %w", err)
	}

	transport := &http.Transport{
		//nolint:gosec // the server's certificate is the one this run asked it to make
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	return &client{http: &http.Client{Jar: jar, Transport: transport, Timeout: requestLimit}}, nil
}

// reply is one answer: the status and the body, read as JSON when it is.
type reply struct {
	status int
	raw    []byte
	object map[string]any
}

func (r reply) text() string { return string(r.raw) }

func (r reply) str(key string) string {
	value, _ := r.object[key].(string)

	return value
}

func (r reply) num(key string) float64 {
	value, _ := r.object[key].(float64)

	return value
}

// call sends one request. A body that is a []byte goes as it is, anything
// else as JSON.
func (c *client) call(ctx context.Context, method, url string, body any, headers map[string]string) (reply, error) {
	var reader io.Reader

	contentType := ""

	switch value := body.(type) {
	case nil:
	case []byte:
		reader, contentType = bytes.NewReader(value), "application/octet-stream"
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return reply{}, fmt.Errorf("encode the body: %w", err)
		}

		reader, contentType = bytes.NewReader(encoded), "application/json"
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return reply{}, fmt.Errorf("build %s %s: %w", method, url, err)
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	res, err := c.http.Do(req)
	if err != nil {
		return reply{}, fmt.Errorf("%s %s: %w", method, url, err)
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(res.Body, bodyLimit))
	if err != nil {
		return reply{}, fmt.Errorf("read %s %s: %w", method, url, err)
	}

	out := reply{status: res.StatusCode, raw: raw}
	_ = json.Unmarshal(raw, &out.object) // a body that is not an object stays raw

	return out, nil
}

// waitUp waits until the server answers its health check at base.
func (c *client) waitUp(ctx context.Context, base string, limit time.Duration) (reply, bool) {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		got, err := c.call(ctx, http.MethodGet, base+"/api/v1/health", nil, nil)
		if err == nil && got.status == http.StatusOK {
			return got, true
		}

		if !pause(ctx) {
			break
		}
	}

	return reply{}, false
}

// waitResources waits until every declared resource serves. A storage starts a
// moment after the port opens.
func (c *client) waitResources(ctx context.Context, base string) reply {
	var got reply

	deadline := time.Now().Add(resourcesLimit)
	for time.Now().Before(deadline) {
		var err error

		got, err = c.call(ctx, http.MethodGet, base+"/api/v1/readiness", nil, nil)
		if err == nil && got.status == http.StatusOK && got.num("serving") == got.num("declared") {
			return got
		}

		if !pause(ctx) {
			break
		}
	}

	return got
}

// printerTLS is the TLS version the printers' port speaks to a client that
// offers 1.2 at most, which is what a printer offers.
func printerTLS(ctx context.Context) string {
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(printerPort))
	dialer := &tls.Dialer{Config: &tls.Config{
		//nolint:gosec // the printers' certificate is the one this run asked for
		InsecureSkipVerify: true,
		MaxVersion:         tls.VersionTLS12,
	}}

	deadline := time.Now().Add(tlsLimit)
	for time.Now().Before(deadline) {
		conn, err := dialer.DialContext(ctx, "tcp", address)
		if err == nil {
			version := ""
			if secured, ok := conn.(*tls.Conn); ok {
				version = tls.VersionName(secured.ConnectionState().Version)
			}

			_ = conn.Close() // the handshake was the whole question

			return version
		}

		if !pause(ctx) {
			break
		}
	}

	return ""
}

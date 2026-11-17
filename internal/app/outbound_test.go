package app

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnOutboundFailureNamesTheHostAndTheCause(t *testing.T) {
	t.Parallel()

	const address = "https://plugin-marketplace.prusa3d.com/api"

	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()

	_, refused := http.Get(closed.URL)

	require.Error(t, refused)

	cases := map[string]struct {
		err  error
		want string
	}{
		"a resolver that failed": {
			err: &url.Error{Op: "Get", URL: address, Err: &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{
				Err:         "server misbehaving",
				Name:        "plugin-marketplace.prusa3d.com",
				Server:      "127.0.0.11:53",
				IsTemporary: true,
			}}},
			want: `cannot resolve plugin-marketplace.prusa3d.com (DNS failure on this host): ` +
				`the DNS server 127.0.0.11:53 answered "server misbehaving". Check the host's /etc/resolv.conf`,
		},
		"a resolver with no address": {
			err:  &url.Error{Op: "Get", URL: address, Err: &net.DNSError{Err: "no such host", Name: "x.test"}},
			want: `cannot resolve x.test (DNS failure on this host). Check the host's /etc/resolv.conf`,
		},
		"a name that does not exist": {
			err: &url.Error{Op: "Get", URL: address, Err: &net.DNSError{
				Err: "no such host", Name: "plugin-marketplace.prusa3d.com", IsNotFound: true,
			}},
			want: "the name plugin-marketplace.prusa3d.com does not exist in DNS",
		},
		"a closed port": {
			err:  refused,
			want: " refused the connection",
		},
		"a host that stayed silent": {
			err:  &url.Error{Op: "Get", URL: address, Err: &net.OpError{Op: "dial", Err: timeoutError{}}},
			want: "plugin-marketplace.prusa3d.com did not answer in time",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := reachFailure(tc.err)

			require.Error(t, got)
			require.Contains(t, got.Error(), tc.want)
			require.NotContains(t, got.Error(), "dial tcp")
			require.ErrorIs(t, got, tc.err)
		})
	}

	t.Run("the lookup stays reachable through the sentence", func(t *testing.T) {
		t.Parallel()

		got := reachFailure(cases["a resolver that failed"].err)

		lookup, isLookup := errors.AsType[*net.DNSError](got)
		require.True(t, isLookup)
		require.Equal(t, "127.0.0.11:53", lookup.Server)
	})

	t.Run("an error that is no network failure", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, reachFailure(errors.New("unexpected EOF")))
	})
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

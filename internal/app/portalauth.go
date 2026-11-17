package app

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/betaportal"
	"github.com/prusa3d/prusa-connect/internal/kernel/httporigin"
)

// portalAuthorization scopes proof-of-possession authorization to the
// configured Beta Portal origin.
type portalAuthorization struct {
	origin *url.URL
	store  *betaportal.Store
}

func newPortalAuthorization(server string, store *betaportal.Store) portalAuthorization {
	origin, _ := url.Parse(strings.TrimSpace(server))

	return portalAuthorization{origin: origin, store: store}
}

func (a portalAuthorization) matches(address *url.URL) bool {
	return a.store != nil && httporigin.Same(a.origin, address)
}

func (a portalAuthorization) authorize(request *http.Request) error {
	request.Header.Del("Authorization")
	request.Header.Del("DPoP")
	if !a.matches(request.URL) {
		return nil
	}

	return a.store.Authorize(request)
}

// send sends a request that authorize signed. When the portal refuses it with
// 401, the store corrects its clock and the request goes once more, marks the
// pairing revoked so the Updates page asks for a new one, or answers
// betaportal.ErrAccessSuspended.
func (a portalAuthorization) send(client *http.Client, request *http.Request) (*http.Response, error) {
	signed := request.Header.Get("Authorization") != ""
	response, err := client.Do(request) //nolint:gosec // G704: authorize scoped the request to the portal origin
	if err != nil || response.StatusCode != http.StatusUnauthorized || !signed ||
		response.Request == nil || !a.matches(response.Request.URL) {
		return response, err
	}

	retry, err := a.store.Rejected(response)
	if err != nil {
		_ = response.Body.Close()

		return nil, err
	}
	if !retry {
		return response, nil
	}

	_ = response.Body.Close()
	if err = a.authorize(request); err != nil {
		return nil, err
	}

	return client.Do(request) //nolint:gosec // G704: the same request, signed again
}

// firstEntitlement is the Beta Portal pairing a composition root hands in,
// and nil for a caller that hands in none.
func firstEntitlement(entitlements []*betaportal.Store) *betaportal.Store {
	if len(entitlements) == 0 {
		return nil
	}

	return entitlements[0]
}

// redirects is the download client's redirect check. A request that carried
// the pairing carries it again only to the portal, and every other request
// loses any credential a redirect would forward.
func (a portalAuthorization) redirects(next *http.Request, via []*http.Request) error {
	if err := checkRemoteRedirect(next, via); err != nil {
		return err
	}

	if len(via) == 0 || via[0].Header.Get("Authorization") == "" {
		next.Header.Del("Authorization")
		next.Header.Del("DPoP")

		return nil
	}

	return a.authorize(next)
}

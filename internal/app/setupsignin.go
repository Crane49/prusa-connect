package app

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/oidc"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// issuerTimeout bounds how long the confirm waits on an issuer's discovery
// document.
const issuerTimeout = 10 * time.Second

// discoverIssuer reads an issuer's discovery document the way the next start
// does, and checks it the same way.
func discoverIssuer(ctx context.Context, issuer string) error {
	ctx, cancel := context.WithTimeout(ctx, issuerTimeout)
	defer cancel()

	_, err := oidc.Discover(ctx, &http.Client{Timeout: issuerTimeout}, issuer)
	if unreachable := reachFailure(err); unreachable != nil {
		return unreachable
	}

	return err
}

// checkIssuer refuses an issuer the next start could not read. That start
// reads the issuer's discovery document before it serves anything, so an
// issuer it cannot reach is a server that does not come back.
func (f *firstRun) checkIssuer(ctx context.Context, core config.Core) error {
	if !core.Auth.Enabled() || f.discover == nil {
		return nil
	}

	if err := f.discover(ctx, core.Auth.Issuer); err != nil {
		return httpfault.RequestInvalid("body.configuration.core.auth.issuer",
			"an OpenID Connect issuer this server reaches", err.Error())
	}

	return nil
}

// setupModeFor is the mode of the file the wizard writes: readable by others
// like the operator's own file, unless an answer is a secret, such as a client
// secret or a connection string with a password in it.
func setupModeFor(answers map[string]any, contract httpapi.SetupConfiguration) os.FileMode {
	for _, key := range contract.Keys {
		if _, answered := answers[key.Path]; answered && key.Secret {
			return privateFileMode
		}
	}

	return setupFileMode
}

// CheckOIDC tries the sign-in provider the wizard would configure, behind the
// setup code, and signs nobody in. It reads the discovery document the way
// the next start does, then asks the token endpoint about the client and the
// authorization endpoint about the redirect address. A refusal is a failed
// step and not an error, so the page can show every step.
func (f *firstRun) CheckOIDC(
	ctx context.Context, client httpapi.SetupClient, code string, probe httpapi.SetupOIDCProbe,
) (httpapi.SetupOIDCCheck, error) {
	if !f.Pending() {
		return httpapi.SetupOIDCCheck{}, fault.AuthSetupClosed.New(
			"this server has already recorded what it does about identity")
	}

	if err := f.code.admit(ctx, client, code); err != nil {
		return httpapi.SetupOIDCCheck{}, err
	}

	return checkOIDC(ctx, probe), nil
}

// checkOIDC is CheckOIDC without the gate, for a test.
func checkOIDC(ctx context.Context, probe httpapi.SetupOIDCProbe) httpapi.SetupOIDCCheck {
	ctx, cancel := context.WithTimeout(ctx, issuerTimeout)
	defer cancel()

	skipped := httpapi.SetupOIDCStep{Status: oidcSkipped, Detail: "the discovery document could not be used"}

	provider, err := oidc.New(ctx, oidc.Config{
		Issuer:       probe.Issuer,
		ClientID:     probe.ClientID,
		ClientSecret: probe.ClientSecret,
		RedirectURI:  probe.RedirectURI,
		HTTPClient:   &http.Client{Timeout: issuerTimeout},
	})
	if err != nil {
		detail := err.Error()
		if unreachable := reachFailure(err); unreachable != nil {
			detail = unreachable.Error()
		}

		return httpapi.SetupOIDCCheck{
			Discovery: httpapi.SetupOIDCStep{Status: string(oidc.VerdictFailed), Detail: detail},
			Client:    skipped,
			Redirect:  skipped,
		}
	}

	if err = provider.CheckAlgorithms(); err != nil {
		return httpapi.SetupOIDCCheck{
			Issuer:    provider.Issuer(),
			Discovery: httpapi.SetupOIDCStep{Status: string(oidc.VerdictFailed), Detail: err.Error()},
			Client:    skipped,
			Redirect:  skipped,
		}
	}

	return httpapi.SetupOIDCCheck{
		Issuer:    provider.Issuer(),
		Discovery: httpapi.SetupOIDCStep{Status: string(oidc.VerdictPassed)},
		Client:    stepOf(provider.ProbeClient(ctx)),
		Redirect:  stepOf(provider.ProbeRedirect(ctx)),
	}
}

// oidcSkipped is a step that did not run because the one before it failed.
const oidcSkipped = "skipped"

func stepOf(found oidc.Finding) httpapi.SetupOIDCStep {
	return httpapi.SetupOIDCStep{Status: string(found.Verdict), Detail: found.Detail}
}

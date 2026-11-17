package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// ErrUserInfo reports a userinfo answer this server cannot use.
var ErrUserInfo = errors.New("oidc: userinfo")

// maxUserInfoBytes bounds what a userinfo answer can make this process
// allocate.
const maxUserInfoBytes = 64 << 10

// Profile is what a person is called at the issuer.
type Profile struct {
	Name              string `json:"name"`
	PreferredUsername string `json:"preferred_username"`
	// Username is not an OpenID Connect claim. Synology SSO sends it in place
	// of preferred_username.
	Username string `json:"username"`
	Email    string `json:"email"`
}

// DisplayName is the first label the profile carries: the name, else the
// preferred username, else the username, else the address. Empty when it
// carries none.
func (p Profile) DisplayName() string {
	for _, label := range []string{p.Name, p.PreferredUsername, p.Username, p.Email} {
		if label != "" {
			return label
		}
	}

	return ""
}

// complete fills what p lacks from other.
func (p Profile) complete(other Profile) Profile {
	if p.Name == "" {
		p.Name = other.Name
	}

	if p.PreferredUsername == "" {
		p.PreferredUsername = other.PreferredUsername
	}

	if p.Username == "" {
		p.Username = other.Username
	}

	if p.Email == "" {
		p.Email = other.Email
	}

	return p
}

// ProfileOf is what an ID token says about the person.
func ProfileOf(claims Claims) Profile {
	return Profile{
		Name:              claims.Name,
		PreferredUsername: claims.PreferredUsername,
		Username:          claims.Username,
		Email:             claims.Email,
	}
}

// Profile is what the issuer says about the person a login signed in. The ID
// token is read first. A field it lacks is read from the userinfo endpoint
// with the access token, because some issuers put the name and the address
// only there, as Authelia does by default. The userinfo answer counts only
// when its `sub` is the ID token's, as OpenID Connect Core section 5.3.2
// requires.
//
// A userinfo failure is returned beside what the ID token said. The person is
// still signed in, and the caller logs it.
func (c *Client) Profile(ctx context.Context, claims Claims, accessToken string) (Profile, error) {
	profile := ProfileOf(claims)
	if profile.Name != "" && profile.Email != "" {
		return profile, nil
	}

	if c.meta.UserinfoEndpoint == "" || accessToken == "" {
		return profile, nil
	}

	fetched, err := c.userInfo(ctx, claims.Subject, accessToken)
	if err != nil {
		return profile, err
	}

	return profile.complete(fetched), nil
}

func (c *Client) userInfo(ctx context.Context, subject, accessToken string) (Profile, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.meta.UserinfoEndpoint, nil)
	if err != nil {
		return Profile{}, fmt.Errorf("%w: build the request: %w", ErrUserInfo, err)
	}

	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Accept", "application/json")

	response, err := c.http.Do(request)
	if err != nil {
		return Profile{}, fmt.Errorf("%w: %w", ErrUserInfo, err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return Profile{}, fmt.Errorf("%w: %s answered status %d", ErrUserInfo, c.meta.UserinfoEndpoint,
			response.StatusCode)
	}

	var answer struct {
		Profile

		Subject string `json:"sub"`
	}

	// A signed or encrypted answer is application/jwt, which this decode
	// refuses, and the ID token's fields are kept.
	if err = json.NewDecoder(io.LimitReader(response.Body, maxUserInfoBytes)).Decode(&answer); err != nil {
		return Profile{}, fmt.Errorf("%w: decode the answer: %w", ErrUserInfo, err)
	}

	if answer.Subject != subject {
		return Profile{}, fmt.Errorf("%w: the answer names another subject", ErrUserInfo)
	}

	return answer.Profile, nil
}

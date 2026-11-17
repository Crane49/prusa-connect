package natsbind

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
)

// keyDir is the folder in the data directory with the private seeds of the
// operator key and of the account keys. A seed is the private part of an
// NKEY, the Ed25519 key pair NATS uses to sign and to authenticate.
const keyDir = "keys"

// accountSpec is the input of accounts.mint for one NATS account. A NATS
// account is an isolated set of subjects: a connection in one account cannot
// read the messages of another.
type accountSpec struct {
	// revokedAll is a time. The server refuses every user credential of the
	// account that was issued before it. The zero value revokes nothing.
	revokedAll time.Time
	// jetStream allows the account to have streams and key-value buckets.
	jetStream bool
}

// accounts has this node's operator key, the key of each account, and the
// resolver. In NATS the operator is the key that signs every account. An
// account key signs the users of that account. A signed description of an
// account or a user is a JWT, a signed token. The resolver is the table where
// the server looks up the JWT of an account. Every JWT this package signs is
// signed here.
type accounts struct {
	dir        string
	operator   nkeys.KeyPair
	operatorJT string
	resolver   *server.MemAccResolver

	// mintMu lets one mint call run at a time. A mint stores the account JWT
	// in the resolver and then updates the running server. With two mints of
	// one account at once, the resolver could end with one JWT and the server
	// with the other.
	mintMu sync.Mutex

	mu     sync.Mutex
	pairs  map[string]nkeys.KeyPair
	pubs   map[string]string
	minted map[string]bool
	specs  map[string]accountSpec
	srv    *server.Server
}

// ErrNotMinted means a caller asked for a user credential in an account that
// mint has not signed yet. The server cannot find that account, so it would
// refuse the credential.
var ErrNotMinted = errors.New("the account has not been minted")

// newAccounts reads the operator seed from the data directory, or creates and
// saves one on the first start, and signs the operator JWT with it.
func newAccounts(dir string) (*accounts, error) {
	a := &accounts{
		dir:      filepath.Join(dir, keyDir),
		resolver: &server.MemAccResolver{},
		pairs:    map[string]nkeys.KeyPair{},
		pubs:     map[string]string{},
		minted:   map[string]bool{},
		specs:    map[string]accountSpec{},
	}
	pair, err := a.loadOrCreate("operator", nkeys.CreateOperator)
	if err != nil {
		return nil, fmt.Errorf("operator key: %w", err)
	}
	pub, err := pair.PublicKey()
	if err != nil {
		return nil, fmt.Errorf("operator public key: %w", err)
	}
	claims := jwt.NewOperatorClaims(pub)
	claims.Name = "fractal-node"
	token, err := claims.Encode(pair)
	if err != nil {
		return nil, fmt.Errorf("encode the operator: %w", err)
	}
	a.operator, a.operatorJT = pair, token
	return a, nil
}

// accountPub returns the public key of the account with this name. The first
// call creates and saves the account's key pair. It does not sign the account
// JWT: mint does that.
func (a *accounts) accountPub(name string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.accountPubLocked(name)
}

// accountPubLocked does the work of accountPub. The caller holds a.mu.
func (a *accounts) accountPubLocked(name string) (string, error) {
	if pub, ok := a.pubs[name]; ok {
		return pub, nil
	}
	pair, err := a.loadOrCreate("account-"+name, nkeys.CreateAccount)
	if err != nil {
		return "", fmt.Errorf("account %s key: %w", name, err)
	}
	pub, err := pair.PublicKey()
	if err != nil {
		return "", fmt.Errorf("account %s public key: %w", name, err)
	}
	a.pairs[name] = pair
	a.pubs[name] = pub
	return pub, nil
}

// mint signs the JWT of one account with the operator key and stores it in the
// resolver. When the server is running, mint also updates the account there.
//
// When spec has no revocation time, mint keeps the one from the earlier mint
// of this account. So a later mint of the account cannot make a revoked
// credential valid again.
func (a *accounts) mint(name string, spec accountSpec) error {
	a.mintMu.Lock()
	defer a.mintMu.Unlock()

	claims, srv, err := a.mintLocked(name, spec)
	if err != nil {
		return err
	}
	if srv == nil {
		return nil
	}
	// The server reads the resolver only the first time it needs an account.
	// An account it has already loaded has to be updated on the server.
	loaded, err := srv.LookupAccount(claims.Subject)
	if err != nil {
		return fmt.Errorf("look up account %s: %w", name, err)
	}
	srv.UpdateAccountClaims(loaded, claims)
	return nil
}

// mintLocked signs and stores the account JWT under a.mu. It returns the
// claims and the running server, and mint updates the server after a.mu is
// released, because the server call takes the server's own locks.
func (a *accounts) mintLocked(name string, spec accountSpec) (*jwt.AccountClaims, *server.Server, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if held, ok := a.specs[name]; ok && spec.revokedAll.IsZero() {
		spec.revokedAll = held.revokedAll
	}
	pub, err := a.accountPubLocked(name)
	if err != nil {
		return nil, nil, err
	}
	claims := jwt.NewAccountClaims(pub)
	claims.Name = name
	if spec.jetStream {
		claims.Limits.JetStreamLimits = jwt.JetStreamLimits{
			MemoryStorage: jwt.NoLimit,
			DiskStorage:   jwt.NoLimit,
			Streams:       jwt.NoLimit,
			Consumer:      jwt.NoLimit,
		}
	}
	if !spec.revokedAll.IsZero() {
		// Revoke would use the current time. Every later mint would then move
		// the revocation time forward and refuse credentials that were issued
		// after the operator revoked. RevokeAt keeps the recorded time.
		claims.RevokeAt(jwt.All, spec.revokedAll)
	}
	token, err := claims.Encode(a.operator)
	if err != nil {
		return nil, nil, fmt.Errorf("encode account %s: %w", name, err)
	}
	if storeErr := a.resolver.Store(pub, token); storeErr != nil {
		return nil, nil, fmt.Errorf("store account %s: %w", name, storeErr)
	}
	a.minted[name] = true
	a.specs[name] = spec
	return claims, a.srv, nil
}

// revoke mints the account again with a revocation time. The server then
// refuses every user credential of the account issued before that time. It
// closes the connections that already use one and refuses new ones.
func (a *accounts) revoke(name string, at time.Time, jetStream bool) error {
	return a.mint(name, accountSpec{jetStream: jetStream, revokedAll: at})
}

// signingKey returns the key pair that signs user credentials for one
// account. It returns ErrNotMinted until mint has signed the account.
// accountPub creates the key pair earlier, but the server refuses a credential
// for an account that is not in the resolver.
func (a *accounts) signingKey(name string) (nkeys.KeyPair, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	pair, ok := a.pairs[name]
	if !ok || !a.minted[name] {
		return nil, fmt.Errorf("%s: %w", name, ErrNotMinted)
	}
	return pair, nil
}

// newUserClaims builds the claims of a user JWT with the given permissions.
//
// jwt.UserPermissionLimits contains the limits on subscriptions, payload and
// data as well as the permissions. Assigning perms therefore replaces the
// "no limit" defaults of NewUserClaims with zero, and zero means nothing is
// allowed. This function sets the limits back to "no limit".
func newUserClaims(account, user string, perms jwt.UserPermissionLimits) *jwt.UserClaims {
	claims := jwt.NewUserClaims(user)
	claims.Name = account + "-user"
	claims.UserPermissionLimits = perms
	if claims.Limits.NatsLimits == (jwt.NatsLimits{}) {
		claims.Limits.NatsLimits = jwt.NatsLimits{
			Subs:    jwt.NoLimit,
			Payload: jwt.NoLimit,
			Data:    jwt.NoLimit,
		}
	}
	return claims
}

// issueUser signs a user JWT in one account for the public key in user. The
// JWT alone does not open a connection: the client also has to sign the
// server's nonce, a random value the server sends, with the private key.
func (a *accounts) issueUser(account, user string, perms jwt.UserPermissionLimits) (string, error) {
	pair, err := a.signingKey(account)
	if err != nil {
		return "", err
	}
	token, err := newUserClaims(account, user, perms).Encode(pair)
	if err != nil {
		return "", fmt.Errorf("encode a user for %s: %w", account, err)
	}
	return token, nil
}

// issueLeafUser signs the connection credentials for a node that joined: a
// user JWT for the public key in user, which expires after lifetime.
//
// The expiry time is cut to whole seconds, because the JWT stores whole
// seconds. Otherwise the answer to the join request would name an expiry time
// up to a second after the real one, and a node that renews at that time
// would be too late.
//
// The JWT allows leafnode connections only. A JWT with no list of allowed
// connection types allows every type. The node could then also connect as a
// client on the client listener, inside its link account, and watchLeaves
// does not see client connections.
func (a *accounts) issueLeafUser(
	account, user string,
	perms jwt.UserPermissionLimits,
	lifetime time.Duration,
	now time.Time,
) (token string, expires time.Time, err error) {
	if lifetime <= 0 {
		return "", time.Time{}, fmt.Errorf("issue for %s: the credential lifetime is not positive", account)
	}
	pair, err := a.signingKey(account)
	if err != nil {
		return "", time.Time{}, err
	}
	expires = now.Add(lifetime).Truncate(time.Second)
	claims := newUserClaims(account, user, perms)
	claims.Expires = expires.Unix()
	claims.AllowedConnectionTypes = jwt.StringList{jwt.ConnectionTypeLeafnode}
	token, err = claims.Encode(pair)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("encode a leaf user for %s: %w", account, err)
	}
	return token, expires, nil
}

// issueSentinel signs the sentinel: the user JWT the server uses for a client
// that connects with no credentials. A node that wants to join has no
// credentials yet, so the sentinel is a bearer token, a JWT that needs no
// signature from the client. Its key pair is created here and thrown away.
//
// The sentinel allows standard client connections only. The server uses the
// sentinel for every kind of connection that has no credentials. Without the
// restriction, a leafnode connection with no credentials would enter the
// bootstrap account, and the server must refuse a leafnode connection that
// has no credentials.
func (a *accounts) issueSentinel(account string, perms jwt.UserPermissionLimits) (string, error) {
	pair, err := nkeys.CreateUser()
	if err != nil {
		return "", fmt.Errorf("create a sentinel key: %w", err)
	}
	pub, err := pair.PublicKey()
	if err != nil {
		return "", fmt.Errorf("read the sentinel public key: %w", err)
	}
	accountPair, err := a.signingKey(account)
	if err != nil {
		return "", err
	}
	claims := newUserClaims(account, pub, perms)
	claims.BearerToken = true
	claims.AllowedConnectionTypes = jwt.StringList{jwt.ConnectionTypeStandard}
	token, err := claims.Encode(accountPair)
	if err != nil {
		return "", fmt.Errorf("encode the sentinel for %s: %w", account, err)
	}
	return token, nil
}

// connectOption creates a key pair and a user JWT for a connection from this
// node to its own server, and returns the nats.Option that authenticates with
// them. The key pair stays in memory and is never written to disk.
func (a *accounts) connectOption(account string, perms jwt.UserPermissionLimits) (nats.Option, error) {
	pair, err := nkeys.CreateUser()
	if err != nil {
		return nil, fmt.Errorf("create a client key: %w", err)
	}
	pub, err := pair.PublicKey()
	if err != nil {
		return nil, fmt.Errorf("read the client public key: %w", err)
	}
	token, err := a.issueUser(account, pub, perms)
	if err != nil {
		return nil, err
	}
	return nats.UserJWT(
		func() (string, error) { return token, nil },
		func(nonce []byte) ([]byte, error) { return pair.Sign(nonce) },
	), nil
}

// attachServer records the running server, so that a later mint also updates
// the account on the server.
func (a *accounts) attachServer(srv *server.Server) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.srv = srv
}

// applyTo sets the server options for operator mode. In that mode the server
// trusts this node's operator key, reads accounts from the resolver, and uses
// the account named accountSystem as its system account.
func (a *accounts) applyTo(opts *server.Options) error {
	sys, err := a.accountPub(accountSystem)
	if err != nil {
		return err
	}
	claims, err := jwt.DecodeOperatorClaims(a.operatorJT)
	if err != nil {
		return fmt.Errorf("decode the operator: %w", err)
	}
	// The server fills TrustedKeys from the operator claims. It refuses options
	// that set both.
	opts.TrustedOperators = []*jwt.OperatorClaims{claims}
	opts.AccountResolver = a.resolver
	opts.SystemAccount = sys
	return nil
}

// loadOrCreate reads the seed file name.nk from the key directory. When the
// file does not exist it calls create and saves the new seed.
func (a *accounts) loadOrCreate(name string, create func() (nkeys.KeyPair, error)) (nkeys.KeyPair, error) {
	path := filepath.Join(a.dir, name+".nk")
	seed, err := os.ReadFile(path)
	switch {
	case err == nil:
		pair, parseErr := nkeys.FromSeed(seed)
		if parseErr != nil {
			return nil, fmt.Errorf("%s: %w: %w", path, ErrNotASeed, parseErr)
		}
		return pair, nil
	case !errors.Is(err, os.ErrNotExist):
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	pair, err := create()
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", name, err)
	}
	fresh, err := pair.Seed()
	if err != nil {
		return nil, fmt.Errorf("export %s: %w", name, err)
	}
	if mkErr := os.MkdirAll(a.dir, dirMode); mkErr != nil {
		return nil, fmt.Errorf("make %s: %w", a.dir, mkErr)
	}
	if writeErr := os.WriteFile(path, fresh, keyMode); writeErr != nil {
		return nil, fmt.Errorf("write %s: %w", path, writeErr)
	}
	return pair, nil
}

// permissions builds user permissions from the subjects a connection may
// publish on and the subjects it may subscribe to.
func permissions(pub, sub []string) jwt.UserPermissionLimits {
	var perms jwt.UserPermissionLimits
	perms.Pub.Allow = append(jwt.StringList{}, pub...)
	perms.Sub.Allow = append(jwt.StringList{}, sub...)
	return perms
}

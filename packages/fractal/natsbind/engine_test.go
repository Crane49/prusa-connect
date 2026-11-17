package natsbind

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// pathEngine is an Engine that implements PathSetter, as the engine in
// sdk/fractal/node does. It records every path SetPath receives.
type pathEngine struct {
	mu    sync.Mutex
	taken []fractal.NodePath
	err   error
}

// Deliver does nothing.
func (e *pathEngine) Deliver(context.Context, fractalnode.Inbound) {}

// ChildConnected does nothing.
func (e *pathEngine) ChildConnected(context.Context, fractal.NodeID) {}

// ChildLost does nothing.
func (e *pathEngine) ChildLost(context.Context, fractal.NodeID) {}

// ParentConnected does nothing.
func (e *pathEngine) ParentConnected(context.Context) {}

// ParentLost does nothing.
func (e *pathEngine) ParentLost(context.Context) {}

// ChildRevoked does nothing.
func (e *pathEngine) ChildRevoked(context.Context, fractal.NodeID) {}

// ForgetAncestors does nothing.
func (e *pathEngine) ForgetAncestors(context.Context) {}

// SetPath records the path and returns e.err.
func (e *pathEngine) SetPath(_ context.Context, path fractal.NodePath) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.taken = append(e.taken, path)
	return e.err
}

// paths returns the paths SetPath received, in order.
func (e *pathEngine) paths() []fractal.NodePath {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]fractal.NodePath(nil), e.taken...)
}

// TestTheEngineIsGivenTheGrantedPath checks that tellPath calls SetPath with
// the Node Path the node above assigned, also after SetPath failed (fractal.md,
// After the Join Exchange).
func TestTheEngineIsGivenTheGrantedPath(t *testing.T) {
	t.Parallel()
	engine := &pathEngine{}
	b := &Binding{cfg: Config{}.withDefaults(), engine: engine, log: Config{}.withDefaults().Logger}
	b.tellPath(t.Context(), "hq/brno/printer-1")
	if got := engine.paths(); len(got) != 1 || got[0] != "hq/brno/printer-1" {
		t.Fatalf("SetPath received %v, want only hq/brno/printer-1", got)
	}
	engine.err = errors.New("the engine refuses")
	b.tellPath(t.Context(), "hq/brno/printer-2")
	if got := len(engine.paths()); got != 2 {
		t.Errorf("SetPath received %d paths, want 2", got)
	}
}

// TestTheDomainOfTheNodeAbove checks parentDomain, the JetStream domain of the
// node above where pullRPC creates its consumer. A root node and a node with
// no Node Path have none.
func TestTheDomainOfTheNodeAbove(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		path fractal.NodePath
		want string
		ok   bool
	}{
		{"a node under another", "hq/brno/printer-1", "hq_brno", true},
		{"a node at the root", "hq", "", false},
		{"a node with no path", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			b := &Binding{path: c.path}
			got, ok := b.parentDomain()
			if ok != c.ok {
				t.Fatalf("parentDomain returned ok %v, want %v", ok, c.ok)
			}
			if got != c.want {
				t.Errorf("the domain is %q, want %q", got, c.want)
			}
		})
	}
}

// TestAnAccountSeedThatIsNotOneIsRefused checks that accountPub returns
// ErrNotASeed for a seed file with invalid content. It must not create a new
// key for that account.
func TestAnAccountSeedThatIsNotOneIsRefused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	held, err := newAccounts(dir)
	if err != nil {
		t.Fatalf("build the accounts: %v", err)
	}
	path := filepath.Join(dir, keyDir, "account-link-printer-1.nk")
	if writeErr := os.WriteFile(path, []byte("not a seed"), keyMode); writeErr != nil {
		t.Fatalf("write the seed: %v", writeErr)
	}
	if _, pubErr := held.accountPub(linkAccount("printer-1")); !errors.Is(pubErr, ErrNotASeed) {
		t.Errorf("accountPub returned %v, want ErrNotASeed", pubErr)
	}
}

// TestTheAccountsAreMintedOnceAndKept checks that an account has the same
// public key after a restart. The credentials the node issued before the
// restart then stay valid.
func TestTheAccountsAreMintedOnceAndKept(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	first, err := newAccounts(dir)
	if err != nil {
		t.Fatalf("build the accounts: %v", err)
	}
	one, err := first.accountPub(accountLocal)
	if err != nil {
		t.Fatalf("read the account key: %v", err)
	}
	again, err := newAccounts(dir)
	if err != nil {
		t.Fatalf("build the accounts again: %v", err)
	}
	two, err := again.accountPub(accountLocal)
	if err != nil {
		t.Fatalf("read the account key: %v", err)
	}
	if one != two {
		t.Errorf("the account key after the restart is %q, want %q", two, one)
	}
}

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The three types one command publishes, spelled out here rather than imported
// for the reason printerSampled is: a caller outside this process reads them
// off the wire, and a constant that moved with the code would assert nothing.
// historyFlush is the feed's own write cadence. A poll taken inside it is a
// poll asking about a batch that is still in memory.
const historyFlush = 2 * time.Second

const (
	commandRequested  = "com.prusa.connect.device.command.requested"
	commandSettled    = "com.prusa.connect.device.command.settled"
	commandAuthorized = "com.prusa.connect.device.command.authorized"
)

// historyResources declares one SQLite history instance and is appended to the
// e2e configuration's resources block.
func historyResources(dir string) string {
	return strings.Join([]string{
		"  history:",
		"    past:",
		"      type: sqlite",
		"      enabled: true",
		"      config:",
		"        path: " + yamlPath(dir+"/history.db"),
	}, "\n")
}

// commandEnvelope is one command frame as a reader off the wire sees it.
type commandEnvelope struct {
	Type        string          `json:"type"`
	Subject     string          `json:"subject"`
	Class       string          `json:"evclass"`
	CausationID string          `json:"causationid"`
	TTL         *uint32         `json:"ttl"`
	AuthType    string          `json:"authtype"`
	AuthID      string          `json:"authid"`
	Data        json.RawMessage `json:"data"`
}

// TestACommandPublishesItsThreeDurableFrames is ADR-20260901-atx end to end: one POST
// becomes three frames on the printer's origin joined by one causation id, the
// caller is handed the id, and a second request under the same key is answered
// with the first invocation's id rather than dispatched again.
func TestACommandPublishesItsThreeDurableFrames(t *testing.T) {
	t.Parallel()

	server := startServer(t)
	server.awaitReady(t)

	ctx, cancel := context.WithTimeout(t.Context(), settleBudget)
	defer cancel()

	frames, errs := server.subscribe(ctx, t)

	_, listed := server.attachPrinter(t, "MK4")

	answer := server.do(t, http.MethodPost, "/api/v1/devices/"+listed.ID+"/commands",
		fmt.Sprintf(`{"verb":%q,"key":"e2e-key-1"}`, sendInfo), nil)
	if answer.status != http.StatusOK {
		t.Fatalf("POST a command answered %d: %s", answer.status, answer.body)
	}

	result := answer.decode(t)

	commandID, named := result["command_id"].(string)
	if !named || commandID == "" {
		t.Fatalf("the response carries no command_id: %s", answer.body)
	}

	t.Logf("observed: the command was answered %s", answer.body)

	seen := make(map[string]commandEnvelope, 3)

	for range 3 {
		got := awaitFrame(ctx, t, frames, errs, func(f frame) bool {
			return strings.HasPrefix(f.event, "com.prusa.connect.device.command.") &&
				strings.Contains(f.data, commandID)
		})

		var envelope commandEnvelope
		if err := json.Unmarshal([]byte(got.data), &envelope); err != nil {
			t.Fatalf("a command frame is not an envelope: %v", err)
		}

		seen[envelope.Type] = envelope

		t.Logf("observed frame: %s", got.data)
	}

	for _, want := range []string{commandRequested, commandSettled, commandAuthorized} {
		if _, found := seen[want]; !found {
			t.Fatalf("the command published no %s frame", want)
		}
	}

	if class := seen[commandRequested].Class; class != "command" {
		t.Errorf("requested is class %q, want command", class)
	}

	// The TTL is the remaining seconds of the caller's own deadline, and this
	// caller set none: there is no server-side command deadline anywhere in
	// this tree, so the honest frame carries no expiry rather than an invented
	// one. What must hold either way is that settled carries none.
	t.Logf("observed: requested carries TTL %v for a caller with no deadline",
		seen[commandRequested].TTL)

	if class := seen[commandSettled].Class; class != "state" {
		t.Errorf("settled is class %q, want state", class)
	}

	if seen[commandSettled].TTL != nil {
		t.Errorf("settled carries a TTL, and the record of what happened never expires")
	}

	if class := seen[commandAuthorized].Class; class != "audit" {
		t.Errorf("authorized is class %q, want audit", class)
	}

	for _, kind := range []string{commandSettled, commandAuthorized} {
		if got := seen[kind].CausationID; got != commandID {
			t.Errorf("%s carries causation id %q, want %q", kind, got, commandID)
		}
	}

	// The principal is on the audit frame and on nothing else.
	if seen[commandAuthorized].AuthID == "" {
		t.Errorf("the audit frame names no principal: %s", seen[commandAuthorized].Data)
	}

	for _, kind := range []string{commandRequested, commandSettled} {
		body := string(seen[kind].Data)
		if strings.Contains(body, "auth_id") || strings.Contains(body, "operator") {
			t.Errorf("%s carries a principal, and only the audit frame may: %s", kind, body)
		}
	}

	// A second request under the same key, while the first has settled, is a
	// fresh invocation: the map holds an invocation and not a history.
	second := server.do(t, http.MethodPost, "/api/v1/devices/"+listed.ID+"/commands",
		fmt.Sprintf(`{"verb":%q,"key":"e2e-key-1"}`, sendInfo), nil)

	again := second.decode(t)
	if again["command_id"] == commandID {
		t.Errorf("a settled key was answered with the first invocation's id: %s", second.body)
	}

	t.Logf("observed: three frames, joined by %s, and a settled key answered %s",
		commandID, second.body)
}

// TestACommandIsReadableFromTheStoredPast is the other transport: the same
// three frames reach a configured history provider, and the audit frame is
// served to a caller who holds audit sight of the printer.
func TestACommandIsReadableFromTheStoredPast(t *testing.T) {
	t.Parallel()

	server := startServerWith(t, historyResources(t.TempDir()))
	server.awaitReady(t)

	_, listed := server.attachPrinter(t, "MK4")

	// The provider is a principal of its own type and holds exactly what it was
	// granted. ADR-20260901-b2z rule 34 makes this grant at bootstrap and confers
	// observe and view_audit on the fleet; until that action exists the grant
	// that carries audit sight is manager, and the vocabulary caps a history
	// provider at viewer on the org deliberately, so it is written per device
	// here. That difference is the seam, and it is real: with view_audit
	// defined, the fleet-wide org grant carries audit sight without making a
	// store a manager of anything.
	grant := server.do(t, http.MethodPost, "/api/v1/grants/device/"+listed.ID,
		`{"relation":"manager","subject":"history:past"}`, nil)
	if grant.status != http.StatusOK && grant.status != http.StatusCreated {
		t.Fatalf("granting the provider answered %d: %s", grant.status, grant.body)
	}

	// The command is sent inside the wait, not before it, and that is the point
	// rather than a convenience. The provider is an ordinary subscriber: it is
	// delivered what is published while it holds sight of the origin, and a
	// frame published in the window between the grant and the provider's next
	// re-authorization reaches it never, because nothing replays a frame a
	// subscriber was not given. So the case keeps issuing commands until one of
	// them lands whole in the store, which is also what an operator granting a
	// store mid-flight actually sees.
	var (
		commandID string
		stored    map[string]any
	)

	eventually(t, "a command reached the store whole", func() bool {
		answer := server.do(t, http.MethodPost, "/api/v1/devices/"+listed.ID+"/commands",
			fmt.Sprintf(`{"verb":%q}`, sendInfo), nil)
		if answer.status != http.StatusOK {
			t.Fatalf("POST a command answered %d: %s", answer.status, answer.body)
		}

		commandID, _ = answer.decode(t)["command_id"].(string)

		// One flush interval, so a poll is not asking about a batch the feed
		// has not written yet.
		time.Sleep(historyFlush)

		page := server.get(t, "/api/v1/history/facts?origins=device:"+listed.ID+"&types="+
			commandRequested+","+commandSettled+","+commandAuthorized)
		if page.status != http.StatusOK {
			return false
		}

		stored = nil
		if err := json.Unmarshal(page.body, &stored); err != nil {
			return false
		}

		return strings.Contains(string(page.body), commandID) &&
			strings.Count(string(page.body), `"command_id"`) >= 3
	})

	body, _ := json.Marshal(stored)
	if !strings.Contains(string(body), commandID) {
		t.Fatalf("the stored past holds no row for %s: %s", commandID, body)
	}

	if !strings.Contains(string(body), commandAuthorized) {
		t.Fatalf("the audit frame was refused to a caller who holds audit sight: %s", body)
	}

	t.Logf("observed: the printer queried alone answered %s", body)

	// The same caller, the same rows, one more origin. Leaving the origins out
	// covers everything this caller may read, which here is the printer and the
	// storage instance, and the caller holds audit sight of the printer alone.
	// Classes is one list over a plural query and it fails closed, so the audit
	// row is withheld from a query that names both while the other two are
	// still served. This is F2's shape against the running binary.
	mixed := server.get(t, "/api/v1/history/facts?types="+
		commandRequested+","+commandSettled+","+commandAuthorized)
	if mixed.status != http.StatusOK {
		t.Fatalf("the mixed query answered %d: %s", mixed.status, mixed.body)
	}

	if strings.Contains(string(mixed.body), commandAuthorized) {
		t.Fatalf("a query covering an origin with no audit sight served the audit row: %s",
			mixed.body)
	}

	for _, kind := range []string{commandRequested, commandSettled} {
		if !strings.Contains(string(mixed.body), kind) {
			t.Fatalf("the mixed query withheld %s as well as the audit row: %s", kind, mixed.body)
		}
	}

	t.Logf("observed: the mixed-grant query answered %s", mixed.body)
}

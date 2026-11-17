package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
)

// The permissions lane. It runs the shipping binary with the local password
// login on, makes a second person through the routes an operator has, and
// measures what that person reaches before and after one grant.
//
// It is here and not in internal/authz because the question is not what the
// graph answers. It is what a caller holding a session cookie gets back from
// the HTTP surface, which is the only place the account store, the session, the
// projection and the device plane are all in the same process.
//
// Every case names its status codes. A refusal that changes from 403 to 404, or
// from 404 to 200, is the failure this lane exists to catch.

// localAuth is the core block that turns the password login on. Without it the
// deployment authenticates nobody and serves every request as core.principal,
// so there is no second person to test.
const localAuth = "  auth:\n    local: true"

// The two accounts this lane makes, and the password they take. The password is
// a literal because the account store's floor is eight bytes and nothing here
// is a secret.
const (
	adminName = "livetest-admin"
	bobName   = "livetest-bob"
	password  = "livetest-password"
)

// person is one logged-in caller: the cookie the login set, and the subject the
// graph names them by.
type person struct {
	cookie  string
	subject string
	id      string
}

// headers is what a request from this person carries.
func (p person) headers() map[string]string {
	return map[string]string{"Cookie": p.cookie}
}

// TestASecondPersonReachesOnlyWhatTheyWereGranted is the whole lane: the first
// administrator, a second person holding nothing, one grant, and the negative
// space either side of it.
//
// The subtests run in order and are not parallel: each one stands on the grant
// the one before it wrote, so the sequence is the assertion.
//
//nolint:tparallel // the subtests are steps of one sequence, not independent cases
func TestASecondPersonReachesOnlyWhatTheyWereGranted(t *testing.T) {
	t.Parallel()

	srv := startServerCore(t, localAuth, "")
	srv.awaitReady(t)

	admin := srv.setupFirstAdmin(t)

	core := srv.attachAs(t, admin, "CoreOne", "LIVE-CORE-1")
	other := srv.attachAs(t, admin, "MK4S", "LIVE-MK4S-2")

	bob := srv.createPerson(t, admin, bobName, "")

	t.Run("holding nothing, the device is not there", func(t *testing.T) {
		if got := srv.as(t, bob, http.MethodGet, "/api/v1/devices/"+core, ""); got.status != http.StatusNotFound {
			t.Fatalf("reading a device with no grant answered %d, want 404: %s", got.status, got.body)
		}

		got := srv.as(t, bob, http.MethodPost, "/api/v1/devices/"+core+"/commands",
			`{"verb":"core.command.beep"}`)
		if got.status != http.StatusNotFound {
			t.Fatalf("commanding a device with no grant answered %d, want 404: %s", got.status, got.body)
		}
	})

	srv.grant(t, admin, core, "operator", bob.subject)

	t.Run("after the grant the same two calls pass", func(t *testing.T) {
		if got := srv.as(t, bob, http.MethodGet, "/api/v1/devices/"+core, ""); got.status != http.StatusOK {
			t.Fatalf("reading the granted device answered %d, want 200: %s", got.status, got.body)
		}

		got := srv.as(t, bob, http.MethodPost, "/api/v1/devices/"+core+"/commands",
			`{"verb":"core.command.beep"}`)
		if got.status != http.StatusOK {
			t.Fatalf("commanding the granted device answered %d, want 200: %s", got.status, got.body)
		}
	})

	t.Run("the grant reaches one device and not its neighbour", func(t *testing.T) {
		got := srv.as(t, bob, http.MethodPost, "/api/v1/devices/"+other+"/commands",
			`{"verb":"core.command.beep"}`)
		if got.status != http.StatusNotFound {
			t.Fatalf("commanding the ungranted device answered %d, want 404: %s", got.status, got.body)
		}
	})

	t.Run("operate does not reach a dangerous verb", func(t *testing.T) {
		got := srv.as(t, bob, http.MethodPost, "/api/v1/devices/"+core+"/commands",
			`{"verb":"core.command.send_gcode","args":{"gcode":"M300"}}`)
		if got.status != http.StatusForbidden {
			t.Fatalf("raw gcode without a row answered %d, want 403: %s", got.status, got.body)
		}
	})

	t.Run("the grantee cannot grant themselves more", func(t *testing.T) {
		cases := []struct {
			what   string
			method string
			path   string
			body   string
		}{
			{"a manager tuple on the device they operate", http.MethodPost,
				"/api/v1/grants/device/" + core, `{"relation":"manager","subject":"` + bob.subject + `"}`},
			{"an admin role on the org", http.MethodPost,
				"/api/v1/grants/org/local", `{"relation":"admin","subject":"` + bob.subject + `"}`},
			{"a dangerous-verb row for themselves", http.MethodPut,
				"/api/v1/grants/device/" + core + "/verbs",
				`{"verb":"core.command.send_gcode","subject":"` + bob.subject + `"}`},
			{"an account carrying the admin role", http.MethodPost,
				"/api/v1/auth/accounts",
				`{"username":"livetest-mallory","password":"` + password + `","role":"admin"}`},
			{"the administrator's password", http.MethodPost,
				"/api/v1/auth/accounts/" + admin.id + "/password", `{"new_password":"` + password + `"}`},
		}

		for _, one := range cases {
			got := srv.as(t, bob, one.method, one.path, one.body)
			if got.status != http.StatusForbidden {
				t.Errorf("%s answered %d, want 403: %s", one.what, got.status, got.body)
			}
		}
	})

	t.Run("a named row reaches one dangerous verb and no other", func(t *testing.T) {
		// The row is written by a caller who administers the device and already
		// reaches the verb. Nothing confers a dangerous verb by class, role or
		// level, so the administrator takes the device's owner tuple first:
		// ownership implies every dangerous verb of the kind, computed
		// (ADR-20260901-b2z rule 19). This is the only route to a first row that exists
		// today, and LIVETEST-03-permissions.md on the archive/finish-v1-original tag says so.
		srv.grant(t, admin, other, "owner", admin.subject)
		srv.grant(t, admin, other, "operator", bob.subject)

		wrote := srv.as(t, admin, http.MethodPut, "/api/v1/grants/device/"+other+"/verbs",
			`{"verb":"core.command.send_gcode","subject":"`+bob.subject+`"}`)
		if wrote.status != http.StatusCreated {
			t.Fatalf("writing the verb row answered %d, want 201: %s", wrote.status, wrote.body)
		}

		got := srv.as(t, bob, http.MethodPost, "/api/v1/devices/"+other+"/commands",
			`{"verb":"core.command.send_gcode","args":{"gcode":"M300"}}`)
		if got.status != http.StatusOK {
			t.Fatalf("raw gcode under its own row answered %d, want 200: %s", got.status, got.body)
		}

		// The neighbouring dangerous verb, which no row names.
		got = srv.as(t, bob, http.MethodPost, "/api/v1/devices/"+other+"/commands",
			`{"verb":"core.command.force_gcode","args":{"gcode":"M300"}}`)
		if got.status != http.StatusForbidden {
			t.Fatalf("the adjacent dangerous verb answered %d, want 403: %s", got.status, got.body)
		}

		// And the same row does not travel to the other machine.
		got = srv.as(t, bob, http.MethodPost, "/api/v1/devices/"+core+"/commands",
			`{"verb":"core.command.send_gcode","args":{"gcode":"M300"}}`)
		if got.status != http.StatusForbidden {
			t.Fatalf("raw gcode on the device with no row answered %d, want 403: %s", got.status, got.body)
		}
	})

	t.Run("an unknown verb answers the same whatever the caller holds", func(t *testing.T) {
		const unknown = `{"verb":"acme.io.shelf.command.calibrate"}`

		seen := srv.as(t, bob, http.MethodPost, "/api/v1/devices/"+core+"/commands", unknown)
		unseen := srv.as(t, bob, http.MethodPost, "/api/v1/devices/pnothinghere000000000000/commands", unknown)

		if seen.status != http.StatusBadRequest || unseen.status != http.StatusBadRequest {
			t.Fatalf("the unknown verb answered %d and %d, want 400 and 400", seen.status, unseen.status)
		}

		if faultCode(t, seen.body) != faultCode(t, unseen.body) {
			t.Fatalf("the unknown verb named %s on a device the caller sees and %s on one that does not exist",
				faultCode(t, seen.body), faultCode(t, unseen.body))
		}
	})

	t.Run("a marking is not a field a client may send", func(t *testing.T) {
		got := srv.as(t, bob, http.MethodPost, "/api/v1/devices/"+core+"/commands",
			`{"verb":"core.command.beep","markings":["opaque_payload"]}`)
		if got.status != http.StatusUnprocessableEntity {
			t.Fatalf("a marking on the command body answered %d, want 422: %s", got.status, got.body)
		}
	})
}

// setupFirstAdmin runs the first run with the setup code from the log, which
// creates the administrator, and logs them in. The account id is
// core.principal, the subject the graph's first grant was written for, so the
// account it makes is the account that holds the org.
func (s *server) setupFirstAdmin(t *testing.T) person {
	t.Helper()

	made := s.do(t, http.MethodPost, "/api/v1/setup",
		`{"mode":"required","code":"`+s.setupCode(t)+`","account":{"username":"`+adminName+
			`","password":"`+password+`"}}`, nil)
	if made.status != http.StatusOK {
		t.Fatalf("the first run answered %d, want 200: %s", made.status, made.body)
	}

	return s.login(t, adminName, "operator")
}

// createPerson adds an account and logs it in. An empty role makes a person who
// holds nothing, which is what the before half of this lane needs.
func (s *server) createPerson(t *testing.T, admin person, name string, role string) person {
	t.Helper()

	body := `{"username":"` + name + `","password":"` + password + `"}`
	if role != "" {
		body = `{"username":"` + name + `","password":"` + password + `","role":"` + role + `"}`
	}

	made := s.do(t, http.MethodPost, "/api/v1/auth/accounts", body, admin.headers())
	if made.status != http.StatusCreated {
		t.Fatalf("creating %s answered %d, want 201: %s", name, made.status, made.body)
	}

	var account struct {
		ID string `json:"id"`
	}

	if err := json.Unmarshal(made.body, &account); err != nil {
		t.Fatalf("parse the created account: %v", err)
	}

	return s.login(t, name, account.ID)
}

// login turns a username and password into the cookie every later request
// carries.
func (s *server) login(t *testing.T, name string, id string) person {
	t.Helper()

	got := s.do(t, http.MethodPost, "/api/v1/auth/login",
		`{"username":"`+name+`","password":"`+password+`"}`, nil)
	if got.status != http.StatusOK {
		t.Fatalf("logging %s in answered %d, want 200: %s", name, got.status, got.body)
	}

	raw := got.header.Get("Set-Cookie")
	if raw == "" {
		t.Fatalf("the login of %s set no cookie", name)
	}

	return person{
		cookie:  strings.SplitN(raw, ";", 2)[0],
		subject: "user:" + id,
		id:      id,
	}
}

// as is one request carrying one person's credential.
func (s *server) as(t *testing.T, who person, method, path, body string) response {
	t.Helper()

	return s.do(t, method, path, body, who.headers())
}

// grant writes one tuple on one device, as the administrator.
func (s *server) grant(t *testing.T, admin person, device, relation, subject string) {
	t.Helper()

	got := s.as(t, admin, http.MethodPost, "/api/v1/grants/device/"+device,
		`{"relation":"`+relation+`","subject":"`+subject+`"}`)
	if got.status != http.StatusCreated && got.status != http.StatusOK {
		t.Fatalf("granting %s on %s answered %d: %s", relation, device, got.status, got.body)
	}
}

// attachAs registers a simulated printer and returns the id the API gave it. It
// is attachPrinter with a credential, because a listing takes one here.
func (s *server) attachAs(t *testing.T, who person, model, serial string) string {
	t.Helper()

	printer, err := simulator.New(simulator.Config{Model: model, Serial: serial, BaseURL: s.baseURL})
	if err != nil {
		t.Fatalf("new %s: %v", model, err)
	}

	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))

	if err = printer.Register(ctx); err != nil {
		cancel()
		t.Fatalf("register a %s: %v", model, err)
	}

	running := make(chan error, 1)
	go func() { running <- printer.Run(ctx) }()

	t.Cleanup(func() {
		cancel()
		<-running
	})

	var id string

	eventually(t, "the printer "+serial+" appeared on /api/v1/devices", func() bool {
		listed := s.as(t, who, http.MethodGet, "/api/v1/devices", "")
		if listed.status != http.StatusOK {
			t.Fatalf("/api/v1/devices answered %d: %s", listed.status, listed.body)
		}

		var body struct {
			Items []printerView `json:"items"`
		}

		if err = json.Unmarshal(listed.body, &body); err != nil {
			t.Fatalf("parse the listing: %v", err)
		}

		for _, item := range body.Items {
			if item.Serial == serial && item.Connected {
				id = item.ID

				return true
			}
		}

		return false
	})

	return id
}

// faultCode reads the registered code out of a problem document, so two
// refusals are compared by what they say rather than by their whole bodies,
// which carry a trace id that differs every time.
func faultCode(t *testing.T, body []byte) string {
	t.Helper()

	var problem struct {
		Code string `json:"code"`
	}

	if err := json.Unmarshal(body, &problem); err != nil {
		t.Fatalf("parse the refusal %s: %v", body, err)
	}

	return problem.Code
}

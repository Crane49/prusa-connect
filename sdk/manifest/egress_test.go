package manifest_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// egressModule is a manifest with one url option and the egress list given.
func egressModule(egress string) string {
	return `{"name":"s","kind":"module","license":"MIT",` +
		`"options":[{"key":"printer_url","type":"url","summary":"The server jobs go to."},` +
		`{"key":"retries","type":"int","default":"3","summary":"How often a job is sent again."}],` +
		`"egress":` + egress + `}`
}

func TestAModuleWithNoEgressConnectsNowhere(t *testing.T) {
	t.Parallel()

	held, err := manifest.Parse("module.yaml", []byte(`{"name":"s","kind":"module","license":"MIT"}`), decode)
	if err != nil {
		t.Fatal(err)
	}

	if len(held.Egress) != 0 {
		t.Errorf("a manifest with no egress read as %+v", held.Egress)
	}
}

func TestTheEgressEntriesAreRead(t *testing.T) {
	t.Parallel()

	held, err := manifest.Parse("module.yaml", []byte(egressModule(`[
	  {"host":"materials.prusa3d.com","why":"Downloads the material catalogue","sends":"The catalogue version it has"},
	  {"host":"*.example.net","ports":[443,8443],"from":"ui","why":"Shows the maps","sends":"Nothing but the request"},
	  {"host":"192.0.2.10","ports":[80],"why":"Reads a sensor","sends":"Nothing"},
	  {"option":"printer_url","why":"Sends jobs to the server the operator picks","sends":"G-code and the job name"}
	]`)), decode)
	if err != nil {
		t.Fatal(err)
	}

	if len(held.Egress) != 4 {
		t.Fatalf("read %d entries, want 4", len(held.Egress))
	}

	first := held.Egress[0]
	if first.Host != "materials.prusa3d.com" || !slices.Equal(first.Ports, []int{443}) ||
		first.From != manifest.EgressFromProcess {
		t.Errorf("the first entry read as %+v, want port 443 from the process", first)
	}

	if second := held.Egress[1]; !slices.Equal(second.Ports, []int{443, 8443}) || second.From != manifest.EgressFromUI {
		t.Errorf("the second entry read as %+v", second)
	}

	last := held.Egress[3]
	if last.Option != "printer_url" || last.Host != "" {
		t.Errorf("the option entry read as %+v", last)
	}

	if !strings.Contains(last.Line(), "the host in the setting printer_url port 443 from the process") {
		t.Errorf("the install line is %q", last.Line())
	}
}

func TestAnEgressEntryIsValidated(t *testing.T) {
	t.Parallel()

	const reason = `"why":"w","sends":"s"`

	cases := map[string]struct {
		egress  string
		refuses string
	}{
		"not a list":         {`{"host":"a.example"}`, "egress is a map"},
		"neither target":     {`[{` + reason + `}]`, "names neither, expected exactly one of host and option"},
		"both targets":       {`[{"host":"a.example","option":"printer_url",` + reason + `}]`, "names both"},
		"unknown key":        {`[{"host":"a.example","path":"/x",` + reason + `}]`, "path"},
		"bad host":           {`[{"host":"a b.example",` + reason + `}]`, `host is "a b.example"`},
		"deep wildcard":      {`[{"host":"*.*.example",` + reason + `}]`, "expected a host name, *.domain"},
		"wildcard on a TLD":  {`[{"host":"*.com",` + reason + `}]`, "expected a host name, *.domain"},
		"undeclared option":  {`[{"option":"nowhere",` + reason + `}]`, "which options does not declare"},
		"option not text":    {`[{"option":"retries",` + reason + `}]`, "which is a int option"},
		"port zero":          {`[{"host":"a.example","ports":[0],` + reason + `}]`, "ports[0] is 0"},
		"port too high":      {`[{"host":"a.example","ports":[70000],` + reason + `}]`, "expected 1 to 65535"},
		"empty ports":        {`[{"host":"a.example","ports":[],` + reason + `}]`, "expected a list of ports"},
		"unknown from":       {`[{"host":"a.example","from":"server",` + reason + `}]`, `from is "server"`},
		"why with an escape": {`[{"host":"a.example","why":"w\u001b[2J","sends":"s"}]`, "why holds a control"},
		"sends reversed":     {`[{"host":"a.example","why":"w","sends":"s\u202e"}]`, "sends holds a control"},
		"no why":             {`[{"host":"a.example","sends":"s"}]`, "why is empty"},
		"no sends":           {`[{"host":"a.example","why":"w"}]`, "sends is empty"},
		"why too long": {
			`[{"host":"a.example","why":"` + strings.Repeat("w", 201) + `","sends":"s"}]`,
			"why is 201 characters",
		},
		"sends too long": {
			`[{"host":"a.example","why":"w","sends":"` + strings.Repeat("s", 301) + `"}]`,
			"sends is 301 characters",
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := manifest.Parse("module.yaml", []byte(egressModule(testCase.egress)), decode)
			if err == nil {
				t.Fatalf("%s was accepted", name)
			}

			if !strings.Contains(err.Error(), testCase.refuses) {
				t.Errorf("the refusal is %q and does not name %q", err.Error(), testCase.refuses)
			}
		})
	}
}

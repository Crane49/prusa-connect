// Command printer-listener prints the port a printer dials on this server and
// whether that listener uses TLS. scripts/generate_printer_config.sh writes the
// printer's settings file from its output.
//
// A connector may declare a listener of its own under
// core.connectors.<name>.config.listener, and then its printers dial that
// listener. Without one they dial the API listener, core.api.
//
// The output is three lines: listener=<config path>, port=<number>, tls=<0|1>.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	exitOK      = 0
	exitFailure = 1
)

// listener is the part of a listener block this command reads.
type listener struct {
	Addr string `yaml:"addr"`
	TLS  struct {
		Certificate string `yaml:"certificate"`
	} `yaml:"tls"`
}

// document is the part of the configuration file this command reads.
type document struct {
	Core struct {
		API        listener `yaml:"api"`
		Connectors map[string]struct {
			Config struct {
				Listener listener `yaml:"listener"`
			} `yaml:"config"`
		} `yaml:"connectors"`
	} `yaml:"core"`
}

// answer is what the command prints.
type answer struct {
	// Listener is the configuration path of the listener a printer dials.
	Listener string
	// Port is the port in that listener's addr.
	Port string
	// TLS is true when that listener has a certificate.
	TLS bool
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("printer-listener", flag.ContinueOnError)
	flags.SetOutput(stderr)

	configPath := flags.String("config", "config/config.yaml", "the operator's configuration file")
	connector := flags.String("connector", "xbuddy", "the connector instance whose printers dial in")

	if err := flags.Parse(args); err != nil {
		return exitFailure
	}

	content, err := os.ReadFile(*configPath)
	if err != nil {
		fmt.Fprintln(stderr, "printer-listener: "+err.Error())

		return exitFailure
	}

	found, err := resolve(*configPath, content, *connector)
	if err != nil {
		fmt.Fprintln(stderr, "printer-listener: "+err.Error())

		return exitFailure
	}

	tls := "0"
	if found.TLS {
		tls = "1"
	}

	fmt.Fprintf(stdout, "listener=%s\nport=%s\ntls=%s\n", found.Listener, found.Port, tls)

	return exitOK
}

// resolve finds the listener the named connector's printers dial. It returns
// an error when no listener has an addr, or when the addr has no port: a
// settings file with a guessed port gives a printer that never connects.
func resolve(name string, content []byte, connector string) (answer, error) {
	var doc document
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return answer{}, fmt.Errorf("parse %s: %w", name, err)
	}

	path := "core.connectors." + connector + ".config.listener"
	chosen := doc.Core.Connectors[connector].Config.Listener

	if chosen.Addr == "" {
		path = "core.api"
		chosen = doc.Core.API
	}

	if chosen.Addr == "" {
		return answer{}, errors.New("neither core.connectors." + connector +
			".config.listener.addr nor core.api.addr is set in " + name)
	}

	colon := strings.LastIndex(chosen.Addr, ":")
	if colon < 0 || colon == len(chosen.Addr)-1 {
		return answer{}, fmt.Errorf("%s.addr is %q and has no port", path, chosen.Addr)
	}

	return answer{
		Listener: path,
		Port:     chosen.Addr[colon+1:],
		TLS:      chosen.TLS.Certificate != "",
	}, nil
}

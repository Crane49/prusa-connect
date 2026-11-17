package arch_test

import (
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// A test run on a developer machine stays on loopback. On Windows every new
// binary that joins a multicast group, or binds an address another machine
// can reach, makes the firewall ask the person at the keyboard to allow it,
// and a test binary is new on every build. The gates below read the source,
// so they cover these shapes and nothing else:
//
//   - a core a Go test starts in process, through app.Options with a ConfigPath
//   - a core a Go test starts as a binary, through exec.Command with --config
//   - a test that opens the real mDNS socket through discovery.NewLink
//   - a Go test that listens on the wildcard address through package net
//   - pion WebRTC settings built anywhere, whose own mDNS must be off
//   - a script under scripts/ that passes -addr or --addr to a server
//
// A test that exists to use the real network calls one of networkOptIns
// first. Each of them ends in internal/discovery/networktest, which skips the
// test unless PCL_NETWORK_TESTS is 1. CI sets it on its Linux runners and
// scripts/ci-local.sh sets it on Linux.

// networkOptIns are the calls that mark a test as one that uses the real
// network. The two starts are internal/app's, and each calls
// networktest.Require before it starts a core.
//
//nolint:gochecknoglobals // a fixed list, read-only.
var networkOptIns = []string{"networktest.Require", "startOnNetwork", "launchOnNetwork"}

// loopbackHarnessCommands are the commands, outside test files, that start a
// core for a measurement rather than for an operator.
//
//nolint:gochecknoglobals // a fixed list, read-only.
var loopbackHarnessCommands = []string{"cmd/fleetload/"}

// TestEveryCoreATestStartsStaysOffTheNetwork holds the two ways a Go test
// starts a core. In process, app.Options carries LoopbackOnly: true, which
// switches the mDNS announcement and listener off and keeps the camera relay
// on loopback. As a binary, the configuration it writes switches
// core.discovery off.
func TestEveryCoreATestStartsStaysOffTheNetwork(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	var violations []string

	literals, binaries := 0, 0

	walkGoFiles(t, root, func(rel string, isTest bool) {
		harness := slices.ContainsFunc(loopbackHarnessCommands, func(dir string) bool {
			return strings.HasPrefix(rel, dir)
		})
		if !isTest && !harness {
			return
		}

		file := parseGoFile(t, root, rel)
		inApp := file.Name.Name == "app"

		eachFunc(file, func(fn *ast.FuncDecl) {
			optedIn := callsAny(fn, networkOptIns)

			ast.Inspect(fn, func(node ast.Node) bool {
				lit, ok := node.(*ast.CompositeLit)
				if !ok || !isAppOptions(lit.Type, inApp) || !hasKey(lit, "ConfigPath") {
					return true
				}

				literals++

				if !setsTrue(lit, "LoopbackOnly") && !optedIn {
					violations = append(violations, fmt.Sprintf(
						"%s: %s starts a core with app.Options and no LoopbackOnly: true",
						rel, fn.Name.Name))
				}

				return true
			})
		})

		if !isTest || !runsBinaryWithConfig(file) {
			return
		}

		binaries++

		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}

		if !strings.Contains(string(content), "  discovery:") || !strings.Contains(string(content), "enabled: false") {
			violations = append(violations, fmt.Sprintf(
				"%s: starts the binary on a configuration that does not switch core.discovery off", rel))
		}
	})

	requireScanned(t, "app.Options literals that start a core", literals, 8)
	requireScanned(t, "test files that start the binary on a configuration", binaries, 3)

	reportLoopback(t, violations)
}

// TestEveryTestOnTheRealNetworkOptsIn holds the tests that open a socket
// another machine can reach: the real mDNS socket, and a listener on the
// wildcard address. Each one calls one of networkOptIns, so a developer run
// skips it.
func TestEveryTestOnTheRealNetworkOptsIn(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	var violations []string

	judged := 0

	walkGoFiles(t, root, func(rel string, isTest bool) {
		if !isTest {
			return
		}

		file := parseGoFile(t, root, rel)

		eachFunc(file, func(fn *ast.FuncDecl) {
			judged++

			if callsAny(fn, networkOptIns) {
				return
			}

			if opensRealMdnsSocket(fn) {
				violations = append(violations, fmt.Sprintf(
					"%s: %s opens the real mDNS socket without opting in", rel, fn.Name.Name))
			}

			if listensOnWildcard(fn) {
				violations = append(violations, fmt.Sprintf(
					"%s: %s listens on every interface without opting in", rel, fn.Name.Name))
			}
		})
	})

	requireScanned(t, "test functions", judged, 1000)

	reportLoopback(t, violations)
}

// TestEveryWebRTCSettingEngineSwitchesMdnsOff holds pion's own mDNS. Unless it
// is told otherwise, a pion peer connection asks for .local candidates over
// mDNS, and that opens a socket on port 5353 of every address, whatever
// interface filter the settings carry. The server publishes plain addresses,
// so no settings built here need it.
func TestEveryWebRTCSettingEngineSwitchesMdnsOff(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	var violations []string

	judged := 0

	walkGoFiles(t, root, func(rel string, _ bool) {
		file := parseGoFile(t, root, rel)

		eachFunc(file, func(fn *ast.FuncDecl) {
			builds := false

			ast.Inspect(fn.Body, func(node ast.Node) bool {
				lit, ok := node.(*ast.CompositeLit)
				if !ok {
					return true
				}

				if sel, isSel := lit.Type.(*ast.SelectorExpr); isSel && sel.Sel.Name == "SettingEngine" {
					builds = true
				}

				return true
			})

			if !builds {
				return
			}

			judged++

			if !callsAny(fn, []string{"SetICEMulticastDNSMode"}) {
				violations = append(violations, fmt.Sprintf(
					"%s: %s builds a pion SettingEngine and leaves its mDNS on", rel, fn.Name.Name))
			}
		})
	})

	requireScanned(t, "functions that build a pion SettingEngine", judged, 2)

	reportLoopback(t, violations)
}

// wildcardScripts are the scripts allowed to name a wildcard address, each
// with the reason.
//
//nolint:gochecknoglobals // a fixed list, read-only.
var wildcardScripts = map[string]string{
	"scripts/check-container-privileged-port.sh": "the server runs inside a container, " +
		"where the wildcard is the container's own interface",
}

// scriptAddr is one address a script hands a server.
//
//nolint:gochecknoglobals // a compiled pattern, read-only.
var scriptAddr = regexp.MustCompile(`--?addr[ =]+"?([^"\s]+)`)

// scriptAssignment is one shell variable assignment.
//
//nolint:gochecknoglobals // a compiled pattern, read-only.
var scriptAssignment = regexp.MustCompile(`(?m)^\s*([A-Z_][A-Z0-9_]*)=["']?([^"'\s]*)`)

// scriptVariable is a variable reference at the start of a value.
//
//nolint:gochecknoglobals // a compiled pattern, read-only.
var scriptVariable = regexp.MustCompile(`^\$\{?([A-Z_][A-Z0-9_]*)\}?`)

// TestEveryScriptServesOnLoopback holds the check scripts and ci-local.sh.
// Each address a script hands a server with -addr or --addr is loopback, read
// through one shell variable when the script names one. The demo those scripts
// start stays off the local network when its address is loopback.
func TestEveryScriptServesOnLoopback(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	scripts, err := filepath.Glob(filepath.Join(root, "scripts", "*.sh"))
	if err != nil {
		t.Fatalf("list the scripts: %v", err)
	}

	var violations []string

	judged, seen := 0, map[string]bool{}

	for _, path := range scripts {
		rel := "scripts/" + filepath.Base(path)

		content, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read %s: %v", rel, readErr)
		}

		text := string(content)

		variables := map[string]string{}
		for _, match := range scriptAssignment.FindAllStringSubmatch(text, -1) {
			variables[match[1]] = match[2]
		}

		for _, match := range scriptAddr.FindAllStringSubmatch(text, -1) {
			judged++

			addr := match[1]
			if name := scriptVariable.FindStringSubmatch(addr); name != nil {
				if value, ok := variables[name[1]]; ok {
					addr = value + addr[len(name[0]):]
				}
			}

			if isLoopbackAddr(addr) {
				continue
			}

			if _, allowed := wildcardScripts[rel]; allowed {
				seen[rel] = true

				continue
			}

			violations = append(violations, fmt.Sprintf("%s: serves on %s, which is not loopback", rel, match[1]))
		}
	}

	requireScanned(t, "server addresses in scripts", judged, 4)

	for rel := range wildcardScripts {
		if !seen[rel] {
			t.Errorf("%s is allowed a wildcard address and names none. Remove it from wildcardScripts.", rel)
		}
	}

	reportLoopback(t, violations)
}

// reportLoopback fails on every place that would leave loopback on a
// developer run.
func reportLoopback(t *testing.T, violations []string) {
	t.Helper()

	if len(violations) == 0 {
		return
	}

	slices.Sort(violations)
	t.Errorf("%d place(s) leave loopback on a developer run. Keep the socket on loopback, or call one "+
		"of %v first if the test exists to use the network:\n\t%s",
		len(violations), networkOptIns, strings.Join(violations, "\n\t"))
}

// isLoopbackAddr reports whether a host:port names the loopback interface.
func isLoopbackAddr(addr string) bool {
	for _, prefix := range []string{"127.0.0.1:", "localhost:", "[::1]:"} {
		if strings.HasPrefix(addr, prefix) {
			return true
		}
	}

	return false
}

// eachFunc calls back for every top-level function of a file.
func eachFunc(file *ast.File, visit func(*ast.FuncDecl)) {
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
			visit(fn)
		}
	}
}

// callsAny reports whether fn calls a function by one of the names. A name
// with a dot is matched with its package, and one without is matched alone.
func callsAny(fn *ast.FuncDecl, names []string) bool {
	found := false

	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return !found
		}

		if slices.Contains(names, calledName(call)) || slices.Contains(names, qualifiedName(call)) {
			found = true
		}

		return !found
	})

	return found
}

// qualifiedName is a call made through a package, as package.Name, or empty.
func qualifiedName(call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}

	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}

	return pkg.Name + "." + sel.Sel.Name
}

// calledName is the name a call is made by, without its package or receiver.
func calledName(call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		return fun.Sel.Name
	default:
		return ""
	}
}

// isAppOptions reports whether a literal's type is internal/app's Options.
func isAppOptions(expr ast.Expr, inApp bool) bool {
	switch typ := expr.(type) {
	case *ast.Ident:
		return inApp && typ.Name == "Options"
	case *ast.SelectorExpr:
		pkg, ok := typ.X.(*ast.Ident)

		return ok && pkg.Name == "app" && typ.Sel.Name == "Options"
	default:
		return false
	}
}

// hasKey reports whether a literal sets a field.
func hasKey(lit *ast.CompositeLit, key string) bool {
	return keyValue(lit, key) != nil
}

// setsTrue reports whether a literal sets a field to the constant true.
func setsTrue(lit *ast.CompositeLit, key string) bool {
	value, ok := keyValue(lit, key).(*ast.Ident)

	return ok && value.Name == "true"
}

// keyValue is the value a literal gives a field, or nil.
func keyValue(lit *ast.CompositeLit, key string) ast.Expr {
	for _, elt := range lit.Elts {
		pair, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}

		if name, isIdent := pair.Key.(*ast.Ident); isIdent && name.Name == key {
			return pair.Value
		}
	}

	return nil
}

// runsBinaryWithConfig reports whether a file starts a command with a
// --config argument, which is how a test starts the server binary.
func runsBinaryWithConfig(file *ast.File) bool {
	found := false

	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return !found
		}

		if name := calledName(call); name != "Command" && name != "CommandContext" {
			return !found
		}

		for _, arg := range call.Args {
			if lit, isLit := arg.(*ast.BasicLit); isLit && lit.Kind == token.STRING && lit.Value == `"--config"` {
				found = true
			}
		}

		return !found
	})

	return found
}

// opensRealMdnsSocket reports whether fn builds a real link and uses it: an
// announcer or listener switched on, or a direct join. A function that
// replaces the link's open with a fake opens nothing.
func opensRealMdnsSocket(fn *ast.FuncDecl) bool {
	builds, uses, faked := false, false, false

	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.CallExpr:
			switch calledName(typed) {
			case "NewLink":
				builds = true
			case "join":
				uses = true
			}
		case *ast.KeyValueExpr:
			key, isKey := typed.Key.(*ast.Ident)
			value, isValue := typed.Value.(*ast.Ident)

			if isKey && isValue && key.Name == "Enabled" && value.Name == "true" {
				uses = true
			}
		case *ast.AssignStmt:
			for _, lhs := range typed.Lhs {
				if sel, isSel := lhs.(*ast.SelectorExpr); isSel && sel.Sel.Name == "open" {
					faked = true
				}
			}
		}

		return true
	})

	return builds && uses && !faked
}

// wildcardHosts are the ways a listen address names every interface.
//
//nolint:gochecknoglobals // a fixed list, read-only.
var wildcardHosts = []string{`":`, `"0.0.0.0`, `"[::]`}

// listensOnWildcard reports whether fn calls a listen function of package net
// on every interface: an address string with no host or a wildcard one, or a
// UDP or TCP address whose IP is the unspecified one.
func listensOnWildcard(fn *ast.FuncDecl) bool {
	found := false

	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !strings.HasPrefix(calledName(call), "Listen") {
			return !found
		}

		for _, arg := range call.Args {
			if isWildcardArg(arg) {
				found = true
			}
		}

		return !found
	})

	return found
}

// isWildcardArg reports whether one argument of a listen call names every
// interface.
func isWildcardArg(arg ast.Expr) bool {
	if unary, ok := arg.(*ast.UnaryExpr); ok {
		arg = unary.X
	}

	switch typed := arg.(type) {
	case *ast.BasicLit:
		return typed.Kind == token.STRING && slices.ContainsFunc(wildcardHosts, func(prefix string) bool {
			return strings.HasPrefix(typed.Value, prefix)
		})
	case *ast.CompositeLit:
		sel, ok := typed.Type.(*ast.SelectorExpr)
		if !ok || (sel.Sel.Name != "UDPAddr" && sel.Sel.Name != "TCPAddr") {
			return false
		}

		ip := keyValue(typed, "IP")
		if ip == nil {
			return true
		}

		unspecified, isSel := ip.(*ast.SelectorExpr)

		return isSel && (unspecified.Sel.Name == "IPv4zero" || unspecified.Sel.Name == "IPv6unspecified")
	default:
		return false
	}
}

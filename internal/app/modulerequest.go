package app

import (
	"encoding/json"
	"flag"
	"fmt"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// moduleRequest is one `module add` line: what to install, where its bytes are,
// and whether the operator approved what it asks for.
type moduleRequest struct {
	source  string
	from    string
	approve bool
	// settings is the path of a JSON file with the settings of the module. It
	// is empty when the operator gave none, and the module then runs on the
	// defaults its manifest declares. The settings go into the same
	// machine-owned record as the rest of the declaration: a settings block an
	// operator wrote in their own file would hide that record instead of
	// adding to it.
	settings string
	// given is the settings themselves, for a caller that sent them rather than
	// naming a file. It is the API's half of the same key, and it is read only
	// when settings names no file.
	given json.RawMessage
	// recordsMiB is the room the operator grants the module's records in place
	// of what its manifest asks for, and zero to grant what it asks.
	recordsMiB int
	// memoryMiB is the memory budget the operator sets for the module's
	// process in place of what its manifest recommends, and zero to keep the
	// one they set before, or to follow the recommendation.
	memoryMiB int
	// approveListen grants the ports the manifest lists. It is an approval
	// apart from approve, because a port is reachable from the network.
	approveListen bool
	// listen moves a granted port: the port to grant under each name the
	// manifest lists, in place of the one it asks for.
	listen map[string]int
	// database is where the operator puts the module's own database, and nil
	// for no choice: a SQLite file on a first install, what the declaration
	// grants on an update.
	database *databaseChoice
	// shown is what the operator read about the release's requests before
	// approving it, keyed by who showed it: the marketplace's listing and the
	// request's expect. An approved install whose manifest asks for more is
	// refused. Empty is the command, where the operator reads the manifest.
	shown map[string]*install.ModuleAsks
	// channel is the marketplace channel of the release, and empty for a
	// release that did not come from a marketplace.
	channel string
	// pulled is true for a module an install adds because another module
	// requires it.
	pulled bool
	// address is where the release came from, a URL or a path, and digest the
	// sha256 the source stated for it. The lock file records both.
	address string
	digest  string
	// constraint is the constraint to record for the module in place of
	// the one the source names, as an update and an install from the lock
	// keep the operator's.
	constraint string
}

// declaredAs is what the operator decided about the module being installed:
// whether they approved what its manifest asked for, the settings they wrote
// for it, and what the manifest asked for before anything was decided.
type declaredAs struct {
	approve  bool
	settings json.RawMessage
	asked    install.ModuleAsks
	channel  string
	// memory is the operator's memory settings for the module's process.
	memory operatorMemory
	// database is the module's own database the install grants, and nil for
	// none.
	database *install.Database
	// left is what the install does to the databases left behind.
	left resource.LeftChange
	// keep moves the release already installed aside instead of deleting
	// it, so the update can be rolled back when the new release does not
	// start.
	keep bool
	// path is the module path, address and digest where the release came
	// from, and pulled whether an install added it for another module. The
	// declaration records them for the lock.
	path       string
	address    string
	digest     string
	pulled     bool
	constraint string
}

// parseAround reads flags written on either side of the words that are not
// flags, and returns those words.
//
// Go's flag package stops at the first argument that is not a flag, so
// `module add github.com/acme/spools@1.0.0 -from ./release` would leave -from
// unread and the install would refuse for a reason the operator can see is
// wrong. The module path comes first in every example an author copies, so
// flags are read after it too.
func parseAround(flags *flag.FlagSet, args []string) ([]string, error) {
	var operands []string

	rest := args

	for len(rest) > 0 {
		if err := flags.Parse(rest); err != nil {
			return nil, err //nolint:wrapcheck // the caller renders it as a usage error.
		}

		if rest = flags.Args(); len(rest) == 0 {
			break
		}

		operands = append(operands, rest[0])
		rest = rest[1:]
	}

	return operands, nil
}

// describeOption renders one declared setting as a line an operator can act on:
// the key, what it holds, and what it is worth when nobody writes it.
//
// A required key has no default, so the line tells the operator to write it.
// The instance does not start until they do.
func describeOption(option extension.Option) string {
	value := "default " + option.Default

	switch {
	case option.Required:
		value = "required, write it under this instance's config block"
	case option.Default == "":
		value = "no default"
	case option.Type == extension.OptionSecret:
		value = "default set"
	}

	return fmt.Sprintf("%s (%s, %s). %s", option.Key, option.Type, value, option.Summary)
}

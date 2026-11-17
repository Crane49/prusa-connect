package app

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"

	"github.com/prusa3d/prusa-connect/internal/app/devicekinds"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// installDeviceKinds puts the compiled-in device-kind declarations into the
// verb table.
//
// A refused declaration does not stop the boot. A declaration failing
// validation refuses that kind, its verbs classify nowhere, and the refusal is
// named. A build that will not start is a build nobody can read the refusal
// from, and the refusal is already safe: a verb the table does not classify
// reaches no device.
//
// The log carries the inventory, in the one place a person running the server
// sees it without asking: which kinds this binary carries, what was refused
// and why, and every disagreement with the first-seen record that is being
// held inert.
func installDeviceKinds(
	ctx context.Context, logger *slog.Logger, graph *authz.Store,
) (map[string]authz.DeviceKind, error) {
	compiled := devicekinds.Declarations()

	report, err := graph.InstallKindDeclarations(ctx, compiled.Declarations...)
	if err != nil {
		return nil, fmt.Errorf("install the compiled-in device kind declarations: %w", err)
	}

	for _, kind := range slices.Sorted(maps.Keys(compiled.Refused)) {
		reason := compiled.Refused[kind]
		logger.ErrorContext(ctx, "a compiled-in device kind declaration was refused by the host",
			"kind", kind, "reason", reason)
		refuse(kind, reason)
	}

	for _, kind := range slices.Sorted(maps.Keys(report.Refused)) {
		logger.ErrorContext(ctx, "a compiled-in device kind declaration was refused",
			"kind", kind, "reason", report.Refused[kind])
		refuse(kind.String(), report.Refused[kind])
	}

	for _, held := range report.Held {
		logger.WarnContext(ctx, "a declared verb disagrees with what this deployment first saw, "+
			"so the recorded class and markings stand until an operator accepts the change",
			"disagreement", held)
	}

	logger.InfoContext(ctx, "device kinds registered", "kinds", authz.Kinds())

	return compiled.Drivers, nil
}

// refuse writes one refusal back into the extension catalogue, so that the two
// inventories a person can read say the same thing.
//
// The catalogue records what registered and the verb table records what the
// host accepted. Without this, a declaration refused between the two would
// leave /api/v1/vocabularies/extensions advertising a device class whose
// commands nothing dispatches, with the refusal only in one line of the
// start-up log. The catalogue carries the reason and the entry stays listed
// with it, so a reader of either inventory reaches the same conclusion.
//
// The refusals arrive from two places, which is why this is a function and not
// a line: devicekinds refuses a declaration the host cannot derive at all, and
// the graph refuses one that disagrees with what this deployment first saw. A
// reader does not care which layer said no.
func refuse(qualified, reason string) {
	extension.Global().Refuse(extension.KindDeviceKind, qualified, reason)
}

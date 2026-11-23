package resourcehost

import (
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// Advisory HTTP statuses for the codes below. The class is 5xx: the operator's
// declaration was read and accepted, and the missing or wrong thing is on the
// machine.
const (
	statusUnavailable = 503
	statusInternal    = 500
)

// The ways an instance fails before it runs. Each code names a different
// action for the operator, so each way has its own code.
//
// They are in the internal domain with the supervision plane's own RESOURCE_
// codes, because an operator reads all of them in the conditions of one
// instance.
//
// A code registered fault.RetryNever keeps the instance in failed and stops
// the retries. A wrong checksum, a contract this host does not serve and an
// artefact that is not the recorded module are not fixed by a retry. The
// supervisor treats every other error as recoverable, so without these codes
// a required instance with a bad checksum would be retried without end.
var (
	moduleDataUnavailable = fault.Register(
		fault.DomainInternal(),
		"RESOURCE_MODULE_DATA_UNAVAILABLE",
		statusUnavailable,
		fault.Document(
			"The module was granted a data directory and the server could not create it, so the "+
				"module was not started",
			"Give the server write access to the directory beside its modules directory, or move "+
				"core.modules to a disk it can write, then restart the instance.",
			fault.RetryAllowed,
			fault.ShowDetail,
			"resource",
			"path",
		),
	)

	pluginMissing = fault.Register(
		fault.DomainInternal(),
		"RESOURCE_PLUGIN_MISSING",
		statusUnavailable,
		fault.Document(
			"The instance names an artefact that is not where the declaration says, so there is "+
				"nothing to launch or serve",
			"Install the extension where the instance says it is, then restart the instance.",
			fault.RetryAllowed,
			fault.ShowDetail,
			"resource", "path",
		),
	)

	moduleDataOwned = fault.Register(
		fault.DomainInternal(),
		"RESOURCE_MODULE_DATA_OWNED",
		statusInternal,
		fault.Document(
			"The module's data directory was made for another module that was installed under the same "+
				"name, so the module was not started",
			"Remove the folder the error names, or move it away if its data is still needed, then restart "+
				"the instance.",
			fault.RetryNever,
			fault.ShowDetail,
			"resource", "path",
		),
	)

	pluginChecksum = fault.Register(
		fault.DomainInternal(),
		"RESOURCE_PLUGIN_CHECKSUM",
		statusInternal,
		fault.Document(
			"The artefact is not the bytes its recorded checksum covers, so it was not launched or served",
			"Reinstall the extension, or record the checksum of the file that is now there.",
			fault.RetryNever,
			fault.ShowDetail,
			"resource", "path",
		),
	)

	pluginContract = fault.Register(
		fault.DomainInternal(),
		"RESOURCE_PLUGIN_CONTRACT",
		statusInternal,
		fault.Document(
			"The extension implements a plugin contract major this host does not serve",
			"Install a build of the extension for this host, or run a host that serves the major it needs.",
			fault.RetryNever,
			fault.ShowDetail,
			"resource", "declared", "serves",
		),
	)

	pluginUndocumented = fault.Register(
		fault.DomainInternal(),
		"RESOURCE_PLUGIN_UNDOCUMENTED",
		statusInternal,
		fault.Document(
			"The module serves HTTP routes and ships no OpenAPI fragment for them, so it was not mounted",
			"Attach the module's OpenAPI 3.1 fragment to its plugin.info answer with routesapi.Describe, "+
				"then restart the instance.",
			fault.RetryNever,
			fault.ShowDetail,
			"resource", "path",
		),
	)

	pluginSettings = fault.Register(
		fault.DomainInternal(),
		"RESOURCE_PLUGIN_SETTINGS",
		statusInternal,
		fault.Document(
			"The instance's config block is not what the module reads, so the module was not started",
			"Read the keys the module declares at GET /api/v1/resources, fix the config block of this "+
				"instance, and reload the configuration.",
			fault.RetryNever,
			fault.ShowDetail,
			"resource",
		),
	)

	pluginUnconfigurable = fault.Register(
		fault.DomainInternal(),
		"RESOURCE_PLUGIN_UNCONFIGURABLE",
		statusInternal,
		fault.Document(
			"The instance carries a config block and the module answers no plugin.configure, so it "+
				"was not started",
			"Remove the config block from this instance, or install a release of the module that "+
				"reads settings.",
			fault.RetryNever,
			fault.ShowDetail,
			"resource",
		),
	)

	manifestDisagrees = fault.Register(
		fault.DomainInternal(),
		"RESOURCE_MANIFEST_DISAGREES",
		statusInternal,
		fault.Document(
			"The artefact that answered is not the module the install recorded, so it was not run",
			"Reinstall the module from the release you want, so the declaration and the artefact "+
				"name the same module, release and plugin contract.",
			fault.RetryNever,
			fault.ShowDetail,
			"resource", "path",
		),
	)

	manifestInvalid = fault.Register(
		fault.DomainInternal(),
		"RESOURCE_MANIFEST_INVALID",
		statusInternal,
		fault.Document(
			"The module ships a module.yaml the host will not read, so nothing of it was served",
			"Fix the manifest in the module's bundle, record the digest of the bundle as it is now, "+
				"and restart the instance.",
			fault.RetryNever,
			fault.ShowDetail,
			"resource", "path", "reason",
		),
	)

	// A type name this build has no implementation for is a misspelling or a
	// build without that adapter. A retry fixes neither, so the instance stays
	// in failed and the message lists the types this build has. Every other
	// instance still starts.
	typeUnknown = fault.Register(
		fault.DomainInternal(),
		"RESOURCE_UNKNOWN_TYPE",
		statusInternal,
		fault.Document(
			"The instance names a type this build of the server does not have, so it was not started",
			"Write one of the types the error lists in the instance's type key, or run a build of the "+
				"server that has this type, then reload the configuration.",
			fault.RetryNever,
			fault.ShowDetail,
			"resource", "kind", "type",
		),
	)

	// The ways a declared requirement is not met. They are separate codes
	// because they ask the operator for different things, and because a retry
	// helps with some and not with others.
	//
	// A contract is met when a module that provides it is installed or
	// switched on, or when the provider serves again. The supervisor
	// therefore keeps retrying and the instance starts by itself.
	dependencyMissing = fault.Register(
		fault.DomainInternal(),
		"RESOURCE_DEPENDENCY_MISSING",
		statusUnavailable,
		fault.Document(
			"The module requires a contract that no serving module provides, so it was not started",
			"Install and switch on a module that provides the contract named here, or start the one that "+
				"does. This instance starts by itself once one is serving.",
			fault.RetryAllowed,
			fault.ShowDetail,
			"resource", "requires",
		),
	)

	// A module that keeps its data in the record store needs a record backend
	// that serves. The operator can declare one, and a restarting one serves
	// again, so the supervisor keeps retrying.
	recordsUnserved = fault.Register(
		fault.DomainInternal(),
		"RESOURCE_RECORDS_UNSERVED",
		statusUnavailable,
		fault.Document(
			"The module keeps its data in the record store and no record backend is serving, "+
				"so it was not started",
			"Declare a record backend under resources.db, such as one of type sqlite, or start the one "+
				"that is declared. This instance starts by itself once one is serving.",
			fault.RetryAllowed,
			fault.ShowDetail,
			"resource", "requires",
		),
	)

	// The running build fixes the capabilities and the core version. No
	// change to the configuration adds either, so the instance stays in failed
	// and is not retried.
	dependencyUnserved = fault.Register(
		fault.DomainInternal(),
		"RESOURCE_DEPENDENCY_UNSERVED",
		statusInternal,
		fault.Document(
			"The module requires something this build of the server does not offer, so it was not started",
			"Run a build of the server that serves the capability or falls in the core range named here, "+
				"or install a release of the module built for this server.",
			fault.RetryNever,
			fault.ShowDetail,
			"resource", "requires", "serves",
		),
	)

	// A module that marked a request header required does not start without
	// the grant. The operator writes the grant, so the host does not retry by
	// itself: the changed declaration starts the instance again.
	headerNotGranted = fault.Register(
		fault.DomainInternal(),
		"RESOURCE_HEADER_NOT_GRANTED",
		statusInternal,
		fault.Document(
			"The module cannot work without a request header this server did not grant it, so it was not started",
			"Grant the header named here in service.request_headers of the instance, or install the module "+
				"again with approve.",
			fault.RetryNever,
			fault.ShowDetail,
			"resource", "header",
		),
	)

	// Two modules that each wait for the other never start. Neither is wrong
	// alone, so both are named and both are refused.
	dependencyCycle = fault.Register(
		fault.DomainInternal(),
		"RESOURCE_DEPENDENCY_CYCLE",
		statusInternal,
		fault.Document(
			"The instance requires a contract whose provider requires it back, so nothing in the ring could start",
			"Remove one requirement in the ring named here, or take one of its instances out of the "+
				"configuration.",
			fault.RetryNever,
			fault.ShowDetail,
			"resource", "cycle",
		),
	)

	// The three ways a root mount an operator wrote for a module is refused.
	// Each names a different edit to the file: rewrite the prefix, pick one the
	// server does not serve, or pick one no other instance holds. A retry
	// fixes none of them, and no process is launched for any of them.
	mountInvalid = fault.Register(
		fault.DomainInternal(),
		"RESOURCE_MOUNT_INVALID",
		statusInternal,
		fault.Document(
			"The module's service.mount is not a root prefix this server can route, so the instance was not started",
			"Write an absolute path with no trailing slash, no dot segment and no character a path segment "+
				"cannot carry, such as /graphql or /app/api, then reload the configuration.",
			fault.RetryNever,
			fault.ShowDetail,
			"resource", "mount", "reason",
		),
	)

	mountReserved = fault.Register(
		fault.DomainInternal(),
		"RESOURCE_MOUNT_RESERVED",
		statusInternal,
		fault.Document(
			"The module's service.mount is a path this server serves itself, so the instance was not started",
			"Choose a prefix outside /api, the OpenAPI document, the reference, /metrics, /dev and every "+
				"path a connector mounted, then reload the configuration.",
			fault.RetryNever,
			fault.ShowDetail,
			"resource", "mount", "reason",
		),
	)

	mountClaimed = fault.Register(
		fault.DomainInternal(),
		"RESOURCE_MOUNT_CLAIMED",
		statusInternal,
		fault.Document(
			"Another running instance already serves the module's service.mount, so the instance was not started",
			"Give this instance a different prefix and reload the configuration, or free the prefix by "+
				"removing or disabling the instance holding it and then switch this instance off and on "+
				"so it starts again.",
			fault.RetryNever,
			fault.ShowDetail,
			"resource", "mount", "held_by",
		),
	)
)

// CodePluginMissing is the result of an instance whose artefact is not on this
// machine.
func CodePluginMissing() fault.Code { return pluginMissing.Code() }

// CodePluginChecksum is the result of an artefact whose bytes are not the ones
// that were installed. Nothing is executed after it.
func CodePluginChecksum() fault.Code { return pluginChecksum.Code() }

// CodePluginContract is the result of an extension built against a contract
// major this host does not serve.
func CodePluginContract() fault.Code { return pluginContract.Code() }

// CodePluginUndocumented is the result of a module that serves routes and
// ships no OpenAPI fragment. It is not mounted.
func CodePluginUndocumented() fault.Code { return pluginUndocumented.Code() }

// CodeManifestInvalid is the result of a bundle carrying a module.yaml this
// host will not read. Nothing of the bundle is served after it.
func CodeManifestInvalid() fault.Code { return manifestInvalid.Code() }

// CodeUnknownType is the result of an instance whose type this build has no
// implementation for. The message names the types it has.
func CodeUnknownType() fault.Code { return typeUnknown.Code() }

// CodeMountInvalid is the result of a service.mount the router cannot route:
// not absolute, ending in a slash, or containing a character a path segment
// cannot carry.
func CodeMountInvalid() fault.Code { return mountInvalid.Code() }

// CodeMountReserved is the result of a service.mount on a path this server
// serves itself.
func CodeMountReserved() fault.Code { return mountReserved.Code() }

// CodeMountClaimed is the result of a service.mount another running instance
// already holds.
func CodeMountClaimed() fault.Code { return mountClaimed.Code() }

package httpfault

import (
	"net/http"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The param names the marketplace codes interpolate, from the published set:
// the digest the request or the marketplace declared, the one the bytes were
// found to have, and the module path as the name an install gave it.
const (
	paramNameDeclared = "declared"
	paramNameFound    = "found"
	paramNameName     = "name"
	paramNameType     = "type"
)

// The codes an install from a marketplace, or from any fetched release,
// answers. Each names what the operator can do next.
var (
	// 400, because the request named a digest and the bytes it led to are
	// not those. Nothing was unpacked, so a retry after the marketplace or the
	// URL is fixed starts from nothing.
	moduleDigestMismatch = fault.Register(
		fault.DomainValidation(), "MODULE_DIGEST_MISMATCH", http.StatusBadRequest, fault.Document(
			"The downloaded release is not the one the request or the marketplace names",
			"The bytes fetched from the release's address hash to another SHA-256 than the one "+
				"stated, so this server refused them before unpacking anything. Ask the marketplace "+
				"for the module again, or check that the address and the digest belong together.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameSource, paramNameDeclared, paramNameFound,
		))

	// 400, because the request asked the marketplace for something it does
	// not offer: an unknown module, a version it does not list, or one built
	// for another platform or another core.
	moduleNotOffered = fault.Register(
		fault.DomainValidation(), "MODULE_NOT_OFFERED", http.StatusBadRequest, fault.Document(
			"The marketplace offers no such release for this server",
			"Read the module index again and install a version a row offers, or name the release's "+
				"address in from. The marketplace lists only releases built for this server's platform "+
				"and accepting its version.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameName, paramNameVersion,
		))

	// 409, because the request is sound and this server was told not to act on
	// it: a person switched the marketplace off, and switching it on again is
	// what makes the same request work.
	marketplaceDisabled = fault.Register(
		fault.DomainResource(), "MARKETPLACE_DISABLED", http.StatusConflict, fault.Document(
			"The marketplace is switched off on this server",
			"Switch the marketplace on again in the Extensions settings, or install the release from a "+
				"path or an address in from. Modules already installed from it keep running.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameSource,
		))
	// 409 on the model of MARKETPLACE_DISABLED: the request is sound, and the
	// operator's configuration told this server not to act on it. Only the
	// operator's file switches modules on again, so the remedy names it.
	modulesDisabled = fault.Register(
		fault.DomainResource(), "MODULES_DISABLED", http.StatusConflict, fault.Document(
			"This server installs no module and runs none it installed",
			"The operator set core.modules_enabled to false in the configuration file, so this server "+
				"installs no module and starts none it installed. Modules declared in that file still "+
				"run, and an installed module can still be removed. To install or start modules, set "+
				"core.modules_enabled to true, or delete the line, and restart the server.",
			fault.RetryNever,
			fault.ShowDetail,
		))
	// 409 like MODULES_DISABLED, with its own code because the remedy differs:
	// safe mode ends with a normal restart, and no line of the file changes.
	safeMode = fault.Register(
		fault.DomainResource(), "SAFE_MODE", http.StatusConflict, fault.Document(
			"This server runs in safe mode, so it runs no module",
			"Safe mode starts no module, installed or declared in the configuration file, and installs "+
				"none. A module can still be removed and its settings changed. Restart the server normally "+
				"to start modules again.",
			fault.RetryNever,
			fault.ShowDetail,
		))
	// 502 and retryable, on the model of a camera that does not answer: this
	// server is working and the marketplace it was told to read did not answer.
	marketplaceUnreachable = fault.Register(
		fault.DomainResource(), "MARKETPLACE_UNREACHABLE", http.StatusBadGateway, fault.Document(
			"The marketplace did not answer, so this server cannot say what it offers",
			"Try again in a moment. If it keeps failing, check that core.marketplace.url is right and "+
				"that this server reaches it, or install the release from a path or an address in from.",
			fault.RetryAllowed,
			fault.ShowDetail,
			paramNameSource,
		))

	// 502 and retryable: the address answered something other than the
	// release, or this server would not connect to where it leads. The detail
	// is the same sentence whatever happened, so a caller cannot use the answer
	// to learn what is listening at an address. The log says which.
	moduleDownloadFailed = fault.Register(
		fault.DomainResource(), "MODULE_DOWNLOAD_FAILED", http.StatusBadGateway, fault.Document(
			"This server could not download the release",
			"Try again in a moment. If it keeps failing, the release's address is not one this server "+
				"downloads from: it answered with an error, or it leads to this machine or the local "+
				"network. Install the release from a path on this server instead.",
			fault.RetryAllowed,
			fault.ShowDetail,
			paramNameSource,
		))

	// 500, because the release was sound and the server could not put it in
	// its own modules directory: the operator's to fix, not the caller's.
	moduleInstallFailed = fault.Register(
		fault.DomainInternal(), "MODULE_INSTALL_FAILED", http.StatusInternalServerError, fault.Document(
			"This server could not write the release into its modules directory",
			"Check that core.modules names a directory this server can write, such as one on a "+
				"volume rather than beside a read-only configuration file, then install again.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameSource,
		))

	// 409, because the request is sound and the release is not the one the
	// operator approved: its manifest asks for something they were not shown.
	moduleRequestsChanged = fault.Register(
		fault.DomainResource(), "MODULE_REQUESTS_CHANGED", http.StatusConflict, fault.Document(
			"The release asks for more than the operator was shown",
			"Nothing was installed. Read the module index again, or install without approve to read "+
				"what the release asks for, then approve exactly that. source says what the operator "+
				"was shown, and found lists each request beyond it.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameName, paramNameSource, paramNameFound,
		))

	// 409, because another module already holds the instance name, and its
	// files and its declaration would be replaced by this release.
	moduleNameTaken = fault.Register(
		fault.DomainResource(), "MODULE_NAME_TAKEN", http.StatusConflict, fault.Document(
			"Another module is installed under the same name",
			"Remove the module that holds the name first, or install a release with another name. "+
				"type is the type declared under the name now, and found is the type of this release.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameResource, paramNameType, paramNameFound,
		))

	// 502, because the request is sound and the database server refused or
	// did not answer. The reason is the database server's own words, which
	// carry no credential.
	moduleDatabaseNotMade = fault.Register(
		fault.DomainResource(), "MODULE_DATABASE_NOT_MADE", http.StatusBadGateway, fault.Document(
			"This server could not make the module's database on its Postgres server",
			"Nothing was installed. reason says what the database server answered. Give the role in "+
				"persistence.postgres.module_admin_dsn the rights CREATEDB and CREATEROLE, or drop a role or a "+
				"database of the name found that belongs to something else. Or install with the database mode "+
				"sqlite, or postgres with a dsn.",
			fault.RetryAllowed,
			fault.ShowDetail,
			paramNameName, paramNameFound, paramNameReason,
		))
)

// moduleUpdateRolledBack is an update whose release did not start. 409,
// because the request was sound and what it asked for conflicts with what
// the module does on this server: the release it replaced runs again.
//
//nolint:gochecknoglobals // a registered code, read by the constructor below.
var moduleUpdateRolledBack = fault.Register(
	fault.DomainResource(), "MODULE_UPDATE_ROLLED_BACK", http.StatusConflict, fault.Document(
		"The new release did not start, so the release it replaced runs again",
		"Nothing of the update was kept: the files, the declaration and the grants are those of the release "+
			"named declared, and a database of the module's own is as it was before, because a migration that "+
			"fails is undone whole. reason says why the new release did not start, such as a migration "+
			"that failed or a database a newer release migrated. Install a release that fixes it.",
		fault.RetryNever,
		fault.ShowDetail,
		paramNameName, paramNameVersion, paramNameDeclared, paramNameReason,
	))

// ModuleUpdateRolledBack reports an update whose release did not start and
// was taken back. version is the release that did not start and kept the one
// that runs again, which the fault names declared.
func ModuleUpdateRolledBack(module, version, kept, reason string) *fault.Fault {
	return moduleUpdateRolledBack.New("the release "+version+" of "+module+" did not start, so "+kept+
		" runs again: "+reason).With(
		fault.StringParam(paramNameName, module),
		fault.StringParam(paramNameVersion, version),
		fault.StringParam(paramNameDeclared, kept),
		fault.StringParam(paramNameReason, reason))
}

// CodeModuleUpdateRolledBack names an update that was taken back.
func CodeModuleUpdateRolledBack() fault.Code { return moduleUpdateRolledBack.Code() }

// moduleDatabaseNotPurged is a database left behind that the server could not
// drop. 502, because the request is sound and the database server refused,
// or the role or the database turned out to belong to something else.
//
//nolint:gochecknoglobals // a registered code, read by the constructor below.
var moduleDatabaseNotPurged = fault.Register(
	fault.DomainResource(), "MODULE_DATABASE_NOT_PURGED", http.StatusBadGateway, fault.Document(
		"This server could not drop a database a module left behind",
		"Nothing of that database was dropped, and it is still listed. reason says what the database server "+
			"answered. A role or a database of the name found whose comment or owner names another module "+
			"is never dropped: drop it by hand if it is yours, then purge with forget. Otherwise give the role "+
			"in persistence.postgres.module_admin_dsn the right to drop what it made, and purge again.",
		fault.RetryAllowed,
		fault.ShowDetail,
		paramNameResource, paramNameFound, paramNameReason,
	))

// ModuleDatabaseNotPurged reports a database left behind the server could not
// drop. database is the name of the role and the database.
func ModuleDatabaseNotPurged(instance, database, reason string) *fault.Fault {
	return moduleDatabaseNotPurged.New("this server could not drop the database "+database+" that "+instance+
		" left behind: "+reason).With(
		fault.StringParam(paramNameResource, instance),
		fault.StringParam(paramNameFound, database),
		fault.StringParam(paramNameReason, reason))
}

// ModuleDatabaseNotMade reports a module's database this server could not
// make on its own Postgres server. database is the name of the role and the
// database it tried to make.
func ModuleDatabaseNotMade(module, database, reason string) *fault.Fault {
	return moduleDatabaseNotMade.New("this server could not make the database "+database+" for "+module+
		": "+reason).With(
		fault.StringParam(paramNameName, module),
		fault.StringParam(paramNameFound, database),
		fault.StringParam(paramNameReason, reason))
}

// ModuleDownloadFailed reports a release this server could not download. The
// source is the address without its query, which may carry a signature.
func ModuleDownloadFailed(source string) *fault.Fault {
	return moduleDownloadFailed.New("this server could not download the release from " + source).With(
		fault.StringParam(paramNameSource, source))
}

// CodeModuleDownloadFailed names a release this server could not download.
func CodeModuleDownloadFailed() fault.Code { return moduleDownloadFailed.Code() }

// ModuleInstallFailed reports a sound release this server could not write into
// its modules directory, named by dir.
func ModuleInstallFailed(dir string) *fault.Fault {
	return moduleInstallFailed.New("this server could not write the release into " + dir).With(
		fault.StringParam(paramNameSource, dir))
}

// CodeModuleInstallFailed names a release the modules directory could not take.
func CodeModuleInstallFailed() fault.Code { return moduleInstallFailed.Code() }

// ModuleRequestsChanged reports an approved install whose manifest asks for
// more than the operator was shown. beyond is each request, in the words the
// install's requests use.
func ModuleRequestsChanged(module, shown string, beyond []string) *fault.Fault {
	return moduleRequestsChanged.New("the release of "+module+" asks for more than "+shown+": "+
		strings.Join(beyond, ", ")).With(
		fault.StringParam(paramNameName, module),
		fault.StringParam(paramNameSource, shown),
		fault.StringParam(paramNameFound, strings.Join(beyond, "; ")))
}

// CodeModuleRequestsChanged names a release that asks for more than was shown.
func CodeModuleRequestsChanged() fault.Code { return moduleRequestsChanged.Code() }

// ModuleNameTaken reports a release whose instance name another module holds.
func ModuleNameTaken(instance, held, wanted string) *fault.Fault {
	return moduleNameTaken.New(instance+" is installed as "+held+", and this release is "+wanted).With(
		fault.StringParam(paramNameResource, instance),
		fault.StringParam(paramNameType, held),
		fault.StringParam(paramNameFound, wanted))
}

// CodeModuleNameTaken names a release whose name another module holds.
func CodeModuleNameTaken() fault.Code { return moduleNameTaken.Code() }

// ModuleDigestMismatch reports a fetched release whose bytes hash to another
// digest than the one stated, before it was unpacked.
func ModuleDigestMismatch(source, expected, actual string) *fault.Fault {
	return moduleDigestMismatch.New("the release fetched from "+source+" has another digest").With(
		fault.StringParam(paramNameSource, source),
		fault.StringParam(paramNameDeclared, expected),
		fault.StringParam(paramNameFound, actual))
}

// CodeModuleDigestMismatch names a fetched release with another digest.
func CodeModuleDigestMismatch() fault.Code { return moduleDigestMismatch.Code() }

// ModuleNotOffered reports a module and version the marketplace does not
// offer this server.
func ModuleNotOffered(module, version string) *fault.Fault {
	return moduleNotOffered.New("the marketplace offers no release "+version+" of "+module+" for this server").With(
		fault.StringParam(paramNameName, module),
		fault.StringParam(paramNameVersion, version))
}

// CodeModuleNotOffered names a release the marketplace does not offer.
func CodeModuleNotOffered() fault.Code { return moduleNotOffered.Code() }

// MarketplaceUnreachable reports a marketplace that did not answer when an
// install needed its catalogue.
func MarketplaceUnreachable(source, detail string) *fault.Fault {
	return marketplaceUnreachable.New(detail).With(fault.StringParam(paramNameSource, source))
}

// MarketplaceDisabled reports an install that needs the marketplace while a
// person has switched it off.
func MarketplaceDisabled(source string) *fault.Fault {
	return marketplaceDisabled.New("the marketplace " + source + " is switched off on this server").With(
		fault.StringParam(paramNameSource, source))
}

// CodeMarketplaceDisabled names an install refused because the marketplace is off.
func CodeMarketplaceDisabled() fault.Code { return marketplaceDisabled.Code() }

// ModulesDisabled reports an install, a catalogue read, a marketplace switch,
// or a restart, a switch-on or a root mount of an installed module, while the
// operator has switched modules off.
func ModulesDisabled() *fault.Fault {
	return modulesDisabled.New("core.modules_enabled is false in the configuration file")
}

// CodeModulesDisabled names a request refused because modules are off.
func CodeModulesDisabled() fault.Code { return modulesDisabled.Code() }

// SafeMode reports a request that needs a module to run, or an install, while
// the server runs in safe mode.
func SafeMode() *fault.Fault {
	return safeMode.New("the server runs in safe mode")
}

// CodeSafeMode names a request refused because the server runs in safe mode.
func CodeSafeMode() fault.Code { return safeMode.Code() }

// ModulesHeld is the refusal for a module the server keeps from running:
// SafeMode when the server runs in safe mode, ModulesDisabled otherwise.
func ModulesHeld(safe bool) *fault.Fault {
	if safe {
		return SafeMode()
	}

	return ModulesDisabled()
}

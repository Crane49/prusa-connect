package httpfault

import (
	"net/http"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The param name the dependency refusals interpolate: who needs what. It is
// the key a missing dependency of a starting module already names it by.
const paramNameNeeds = "requires"

// paramNameConflicts is the param a file type conflict names the modules and
// the types by.
const paramNameConflicts = "conflicts"

// The refusals of a change that breaks a dependency between modules. Each is
// 409: the request is sound, and the installed modules or the index refuse it.
var (
	moduleDependencyMissing = fault.Register(
		fault.DomainResource(), "MODULE_DEPENDENCY_MISSING", http.StatusConflict, fault.Document(
			"The module needs another module that is not installed, is switched off, or has a release out of range",
			"requires names each module it needs, the range, and what this server has. Install, switch on or "+
				"update that module, then try again.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameResource, paramNameNeeds,
		))

	moduleRequiredBy = fault.Register(
		fault.DomainResource(), "MODULE_REQUIRED_BY", http.StatusConflict, fault.Document(
			"Other installed modules require this module",
			"requires names each module that requires this one and its range. Remove or switch off those "+
				"modules first, or update them to releases that work with the change.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameResource, paramNameNeeds,
		))

	moduleDependencyConflict = fault.Register(
		fault.DomainResource(), "MODULE_DEPENDENCY_CONFLICT", http.StatusConflict, fault.Document(
			"No release of a module is in the range of every module that needs it",
			"requires names who needs what. Update the module whose range is out of date, or install a "+
				"release of the new module that fits the modules already installed.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameResource, paramNameNeeds,
		))

	moduleDependencyNotOffered = fault.Register(
		fault.DomainResource(), "MODULE_DEPENDENCY_NOT_OFFERED", http.StatusConflict, fault.Document(
			"A module the install needs is in no index this server reads",
			"requires names each missing module and its range. Install it from a release file first, add it "+
				"to the index in the modules directory or to core.modules_local, or switch the marketplace on.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameResource, paramNameNeeds,
		))

	moduleFileTypeConflict = fault.Register(
		fault.DomainResource(), "MODULE_FILE_TYPE_CONFLICT", http.StatusConflict, fault.Document(
			"Another enabled module already reads or draws files of the same type",
			"conflicts names both modules, what each does with the files and the file types they share. "+
				"Switch the other module off or remove it, or install a release of this one that names other "+
				"file types.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameResource, paramNameConflicts,
		))

	modulePlanChanged = fault.Register(
		fault.DomainResource(), "MODULE_PLAN_CHANGED", http.StatusConflict, fault.Document(
			"The modules the install would add are not the ones that were approved",
			"The index or the installed modules changed since the plan was read. Read the plan again, "+
				"approve it, and install again. Nothing was written. found is the plan the server resolved now.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameResource, paramNameFound,
		))

	moduleLockNotWritten = fault.Register(
		fault.DomainResource(), "MODULE_LOCK_NOT_WRITTEN", http.StatusInternalServerError, fault.Document(
			"modules.lock.json could not be written",
			"The detail says whether the change was refused before anything was written or stands without "+
				"a lock that names it. Make the directory of the lock writable, then try again, or run module "+
				"update, which writes the lock again. resource names the module.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameResource,
		))
)

// ModuleDependencyMissing refuses a change that leaves the module itself
// without a module it requires.
func ModuleDependencyMissing(instance, needs string) *fault.Fault {
	return moduleDependencyMissing.New(needs).With(
		fault.StringParam(paramNameResource, instance), fault.StringParam(paramNameNeeds, needs))
}

// CodeModuleDependencyMissing names a change refused for the module's own
// dependency.
func CodeModuleDependencyMissing() fault.Code { return moduleDependencyMissing.Code() }

// ModuleFileTypeConflict refuses a change that would leave two enabled
// modules reading or drawing files of one type.
func ModuleFileTypeConflict(instance, conflicts string) *fault.Fault {
	return moduleFileTypeConflict.New(conflicts).With(
		fault.StringParam(paramNameResource, instance), fault.StringParam(paramNameConflicts, conflicts))
}

// CodeModuleFileTypeConflict names a change refused for a file type another
// module claims.
func CodeModuleFileTypeConflict() fault.Code { return moduleFileTypeConflict.Code() }

// ModuleRequiredBy refuses a change other installed modules depend on.
func ModuleRequiredBy(instance, needs string) *fault.Fault {
	return moduleRequiredBy.New(instance+" is required: "+needs).With(
		fault.StringParam(paramNameResource, instance), fault.StringParam(paramNameNeeds, needs))
}

// CodeModuleRequiredBy names a change refused for another module's
// dependency.
func CodeModuleRequiredBy() fault.Code { return moduleRequiredBy.Code() }

// ModuleDependencyConflict refuses an install no set of releases satisfies.
func ModuleDependencyConflict(module, needs string) *fault.Fault {
	return moduleDependencyConflict.New(needs).With(
		fault.StringParam(paramNameResource, module), fault.StringParam(paramNameNeeds, needs))
}

// CodeModuleDependencyConflict names an install no set of releases satisfies.
func CodeModuleDependencyConflict() fault.Code { return moduleDependencyConflict.Code() }

// ModuleDependencyNotOffered refuses an install that needs a module no index
// offers.
func ModuleDependencyNotOffered(module, needs string) *fault.Fault {
	return moduleDependencyNotOffered.New("no index offers "+needs).With(
		fault.StringParam(paramNameResource, module), fault.StringParam(paramNameNeeds, needs))
}

// CodeModuleDependencyNotOffered names an install that needs a module no
// index offers.
func CodeModuleDependencyNotOffered() fault.Code { return moduleDependencyNotOffered.Code() }

// ModulePlanChanged refuses an install whose plan differs from the approved
// one.
func ModulePlanChanged(module, found string) *fault.Fault {
	return modulePlanChanged.New("the install of "+module+" now adds "+found).With(
		fault.StringParam(paramNameResource, module), fault.StringParam(paramNameFound, found))
}

// CodeModulePlanChanged names an install whose plan moved.
func CodeModulePlanChanged() fault.Code { return modulePlanChanged.Code() }

// ModuleLockNotWritten answers a change of the installed modules whose lock
// could not be written. stands says whether the change was made: before it
// nothing was written, after it the change stands and the answer says so
// rather than reporting a success the lock does not record.
func ModuleLockNotWritten(module string, stands bool, cause error) *fault.Fault {
	outcome := "nothing was changed"
	if stands {
		outcome = "the change stands and the lock does not name it"
	}

	return moduleLockNotWritten.New(module + ": " + outcome + ": " + cause.Error()).
		With(fault.StringParam(paramNameResource, module))
}

// CodeModuleLockNotWritten names a change whose lock was not written.
func CodeModuleLockNotWritten() fault.Code { return moduleLockNotWritten.Code() }

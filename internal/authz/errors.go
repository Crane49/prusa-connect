package authz

import (
	"net/http"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The param names these codes interpolate. A message bundle indexes them, so
// they are part of the published contract for the code that declares them.
const (
	paramNameType     = "type"
	paramNameRelation = "relation"
	paramNameSubject  = "subject"
	paramNameObject   = "object"
	paramNameAction   = "action"
	paramNameID       = "id"
	paramNameName     = "name"
	paramNameKind     = "kind"
	// paramNameVerb and paramNameCommandID are what a refused command carries.
	// They are declared on the code because an undeclared param does not reach
	// a caller. The command id is minted before the authorization check, so a
	// caller who was told no can read back the record of what they asked for.
	paramNameVerb      = "verb"
	paramNameCommandID = "command_id"
)

// The codes this package returns. Registration documents them, and the
// published error reference is generated from the registry.
var (
	unknownType = fault.Register(fault.DomainAuth(), "UNKNOWN_TYPE", http.StatusBadRequest, fault.Document(
		"The authorization graph has no such type",
		"Use one of the types the vocabulary defines. It is closed and cannot be extended at runtime.",
		fault.RetryNever,
		fault.ShowDetail,
		paramNameType,
	))

	unknownRelation = fault.Register(fault.DomainAuth(), "UNKNOWN_RELATION", http.StatusBadRequest, fault.Document(
		"The type does not have that relation",
		"Use a relation the vocabulary defines on this type.",
		fault.RetryNever,
		fault.ShowDetail,
		paramNameType, paramNameRelation,
	))

	invalidSubject = fault.Register(fault.DomainAuth(), "INVALID_SUBJECT", http.StatusBadRequest, fault.Document(
		"The relation does not accept that kind of subject",
		"Grant to a subject shape the relation accepts. A service account cannot hold an org role or a group membership.",
		fault.RetryNever,
		fault.ShowDetail,
		paramNameType,
		paramNameRelation,
		paramNameSubject,
	))

	invalidID = fault.Register(fault.DomainAuth(), "INVALID_ID", http.StatusBadRequest, fault.Document(
		"The identifier is not a DNS-1123 label",
		"Use lowercase letters, digits and hyphens, at most 63 bytes, starting and ending with an alphanumeric.",
		fault.RetryNever,
		fault.ShowDetail,
		paramNameID,
	))

	declarationRefused = fault.Register(
		fault.DomainAuth(), "DECLARATION_REFUSED", http.StatusBadRequest, fault.Document(
			"The extension may not declare a permission on that object",
			"Declare on a resource kind this extension contributes, which is "+
				"resource_kind:<namespace>/<name>. A permission declared on a core object type is "+
				"held by every administrator of every object of that type.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameObject, paramNameType, paramNameName,
		))

	declaredGrantRefused = fault.Register(
		fault.DomainAuth(), "DECLARED_GRANT_REFUSED", http.StatusBadRequest, fault.Document(
			"The declared permission cannot be granted that way",
			"Grant a declared permission on a device whose kind an installed adapter declared it "+
				"for. An org, a group and a device group span device kinds, a retired name is never "+
				"reused, and a condition may not qualify a read action.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameObject, paramNameType, paramNameName, paramNameAction, paramNameKind,
		))

	unknownAction = fault.Register(fault.DomainAuth(), "UNKNOWN_ACTION", http.StatusBadRequest, fault.Document(
		"The authorization graph has no such action",
		"Check one of the actions the vocabulary defines. Roles are not checkable.",
		fault.RetryNever,
		fault.ShowDetail,
		paramNameAction,
	))

	unsupportedObject = fault.Register(
		fault.DomainAuth(), "UNSUPPORTED_OBJECT", http.StatusBadRequest, fault.Document(
			"Actions are not defined on that object type",
			"Check an action on a device. Other object types carry structure and grants, not actions.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameObject, paramNameAction,
		))

	accessDenied = fault.Register(fault.DomainAuth(), "ACCESS_DENIED", http.StatusForbidden, fault.Document(
		"The caller may not perform that action on that object",
		"Ask an administrator for a grant on the device or on a group that contains it.",
		fault.RetryNever,
		fault.ShowDetail,
		paramNameObject, paramNameAction, paramNameVerb, paramNameCommandID,
	))

	invalidScope = fault.Register(fault.DomainAuth(), "INVALID_SCOPE", http.StatusBadRequest, fault.Document(
		"The scope is not one this server knows",
		"Name at least one of the scopes read, operate and administer.",
		fault.RetryNever,
		fault.ShowDetail,
		paramNameName,
	))

	invalidDeviceName = fault.Register(
		fault.DomainAuth(), "INVALID_DEVICE_NAME", http.StatusBadRequest, fault.Document(
			"The device name is empty or too long",
			"Give the device a name of at most 256 bytes.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameName,
		))

	deviceKindConflict = fault.Register(
		fault.DomainAuth(), "DEVICE_KIND_CONFLICT", http.StatusConflict, fault.Document(
			"That identifier already names a device of another kind",
			"Give the device an identifier no other device uses. The directory is one identifier "+
				"space, so a camera registered over a printer of the same identifier would carry "+
				"that printer's grants.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameID, paramNameKind,
		))

	lastAdministrator = fault.Register(
		fault.DomainAuth(), "LAST_ADMINISTRATOR", http.StatusConflict, fault.Document(
			"The last administrator cannot give the admin role away",
			"Give another account the admin role first, then change this one. A server with no "+
				"administrator has nobody who can add people or change what they may do.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameObject, paramNameSubject,
		))

	invalidStoredTuple = fault.Register(
		fault.DomainAuth(), "INVALID_STORED_TUPLE", http.StatusInternalServerError, fault.Document(
			"The authorization graph holds a tuple outside the vocabulary",
			fault.NoAction,
			fault.RetryNever,
			fault.RedactDetail,
		))

	missingEpoch = fault.Register(
		fault.DomainAuth(), "MISSING_EPOCH", http.StatusInternalServerError, fault.Document(
			"The permissions epoch row is missing",
			fault.NoAction,
			fault.RetryNever,
			fault.RedactDetail,
		))

	graphUnavailable = fault.Register(
		fault.DomainAuth(), "GRAPH_UNAVAILABLE", http.StatusInternalServerError, fault.Document(
			"The authorization graph could not be read",
			fault.NoAction,
			fault.RetryAllowed,
			fault.RedactDetail,
		))
)

// CodeUnknownType names a type outside the closed set.
func CodeUnknownType() fault.Code { return unknownType.Code() }

// CodeUnknownRelation names a relation the object type does not have.
func CodeUnknownRelation() fault.Code { return unknownRelation.Code() }

// CodeInvalidSubject names a subject shape the relation does not accept.
func CodeInvalidSubject() fault.Code { return invalidSubject.Code() }

// CodeInvalidID names an identifier that is not a DNS-1123 label.
func CodeInvalidID() fault.Code { return invalidID.Code() }

// CodeDeclaredGrantRefused names a declared grant this surface will not write:
// an object type that spans device kinds, a name no installed adapter declares
// for the device's kind, a retired name, a mapping that moved, or a condition on
// a read action.
func CodeDeclaredGrantRefused() fault.Code { return declaredGrantRefused.Code() }

// CodeUnknownAction names an action outside the closed set.
func CodeUnknownAction() fault.Code { return unknownAction.Code() }

// CodeUnsupportedObject names a check on an object type that has no actions.
func CodeUnsupportedObject() fault.Code { return unsupportedObject.Code() }

// CodeAccessDenied names a refused check. It is what Require returns, and the
// code a transport turns into its own answer: 404 rather than 403 where the
// existence of the object is itself worth hiding.
func CodeAccessDenied() fault.Code { return accessDenied.Code() }

// CodeInvalidDeviceName names a directory name that is empty or oversized.
func CodeInvalidDeviceName() fault.Code { return invalidDeviceName.Code() }

// CodeDeviceKindConflict names a registration that would change what an
// identifier names. The directory is one identifier space, so the two devices
// would be one row and one set of grants.
func CodeDeviceKindConflict() fault.Code { return deviceKindConflict.Code() }

// CodeLastAdministrator names a role change that would leave this server with
// nobody holding the admin role on its org. Nobody could then add a person or
// change what one may do, and no path outside a restart would put an
// administrator back.
func CodeLastAdministrator() fault.Code { return lastAdministrator.Code() }

// CodeInvalidStoredTuple names a stored row that is not a tuple this
// vocabulary allows. It is what a restored backup or a hand-edited table
// produces, and it stops the graph being read rather than resolving the row to
// whatever it happens to resemble.
func CodeInvalidStoredTuple() fault.Code { return invalidStoredTuple.Code() }

// CodeMissingEpoch names an authz_epoch table with no seed row, which would
// otherwise let permission changes land without invalidating anything.
func CodeMissingEpoch() fault.Code { return missingEpoch.Code() }

// CodeGraphUnavailable names a failure to read the graph. The detail stays in
// the log line.
func CodeGraphUnavailable() fault.Code { return graphUnavailable.Code() }

func paramType(t Type) fault.Param { return fault.StringParam(paramNameType, t.String()) }

func paramRelation(r Relation) fault.Param { return fault.StringParam(paramNameRelation, r.String()) }

func paramSubject(s Subject) fault.Param { return fault.StringParam(paramNameSubject, s.String()) }

func paramObject(o Object) fault.Param { return fault.StringParam(paramNameObject, o.String()) }

// paramInstance names one configured instance in a refusal. It renders the same
// "kind:id" a Ref does, so the object a resource refusal names reads like the
// object every other refusal names.
func paramInstance(instance ref.Ref) fault.Param {
	return fault.StringParam(paramNameObject, instance.String())
}

func paramAction(a Action) fault.Param { return fault.StringParam(paramNameAction, a.String()) }

func paramID(id string) fault.Param { return fault.StringParam(paramNameID, id) }

func paramName(name string) fault.Param { return fault.StringParam(paramNameName, name) }

// paramDeviceKind names the device kind a directory row carries or a verb was
// looked up under. It is the same param name: a reader asking "which kind"
// wants one answer, and no message carries both.
func paramDeviceKind(kind ref.DeviceKind) fault.Param {
	return fault.StringParam(paramNameKind, kind.String())
}

// storeFailure keeps a driver error out of every client rendering while the
// cause stays reachable through the log line keyed by trace id.
func storeFailure(what string, err error) error {
	return graphUnavailable.New(what).Wrap(err)
}

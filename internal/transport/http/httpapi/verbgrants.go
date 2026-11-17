package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/prusa3d/prusa-connect/internal/authz"
)

// The dangerous tier's surface, with the acceptance routes and the
// grants-by-subject routes beside it.
//
// A dangerous verb is conferred by no class, no role and no level. Reaching one
// takes a row naming the subject, the device and the verb, and this is where
// that row is written. It is the only place a verb name appears in a grant:
// everywhere else a grant names a class, and a class is an ordinary tuple.
const (
	deviceVerbsPath     = "/api/v1/grants/device/{object_id}/verbs"
	kindAcceptancesPath = "/api/v1/grants/kinds/{kind}/acceptances"
	subjectGrantsPath   = "/api/v1/grants/subject/{subject}"
	subjectDirectPath   = "/api/v1/grants/subject/{subject}/direct"
)

// VerbGrantStore is the dangerous tier and the first-seen record as this
// surface reads and writes them. *authz.Store satisfies it.
type VerbGrantStore interface {
	// VerbGrants returns the rows written on one device, for a caller who may
	// administer it. It reads the written rows and never the projection, so a
	// row that confers nothing because this build does not classify its verb
	// is still listed: an operator deletes it or waits for the verb to come
	// back.
	VerbGrants(ctx context.Context, who authz.Subject, object authz.Object) ([]authz.VerbGrant, error)

	// WriteVerbGrant stores one row and reports whether the graph did not
	// already hold it.
	WriteVerbGrant(ctx context.Context, who authz.Subject, grant authz.VerbGrant) (bool, error)

	// WithdrawVerbGrant removes one row. Removing one that is not there is not
	// an error.
	WithdrawVerbGrant(ctx context.Context, who authz.Subject, grant authz.VerbGrant) error

	// AcceptDeclarationChange records an operator accepting a change the
	// first-seen record is holding, or a module rename it refused.
	AcceptDeclarationChange(
		ctx context.Context, who authz.Subject,
		module string, kind authz.DeviceKind, verb string, to string,
	) error

	// Acceptances is every acceptance recorded for one kind, with the
	// first-seen record each was written against. It is the read side of the
	// route above: an acceptance changes what happens at the next start and
	// nothing before it, so without this there is no way to see what has been
	// accepted until the server is restarted.
	Acceptances(
		ctx context.Context, who authz.Subject, kind authz.DeviceKind,
	) ([]authz.AcceptedChange, error)

	// VerbsOfSubject is every dangerous verb one subject reaches, by device,
	// read from the projection the point check reads.
	VerbsOfSubject(ctx context.Context, who authz.Subject) (map[string][]string, error)

	// SubjectDevices is what one subject holds, device by device, narrowed to
	// the devices the caller may administer.
	SubjectDevices(ctx context.Context, who, subject authz.Subject) ([]authz.DeviceRow, error)

	// SubjectGrants is the tuples written on devices for one subject, narrowed
	// to the devices the caller may administer.
	SubjectGrants(ctx context.Context, who, subject authz.Subject) ([]authz.Tuple, error)
}

// VerbGrant is one row of the dangerous tier on the wire.
type VerbGrant struct {
	Device  string `json:"device"  doc:"The device the row is written on, as device:id."`
	Verb    string `json:"verb"    doc:"The verb this row confers, by name."`
	Subject string `json:"subject" doc:"Who holds it: type:id for a principal, or type:id#relation for a userset."`
}

// DeviceVerbRef is the device the rows are written on, as the path names it. It
// is exported because Huma binds parameters by reflection and cannot write into
// an unexported embedded field.
type DeviceVerbRef struct {
	ObjectID string `path:"object_id" doc:"The device's identifier, a DNS-1123 label." maxLength:"63" example:"9f2a4c18-6d3b-4e07-b5a1-8c0f2d7e4b93"`
}

type listVerbGrantsInput struct {
	DeviceVerbRef
}

type listVerbGrantsOutput struct {
	Body struct {
		Items []VerbGrant `json:"items"`
	}
}

type verbGrantBody struct {
	Verb    string `json:"verb"    doc:"The verb to confer, by name, as the authorization vocabulary spells it." maxLength:"160" minLength:"1"`
	Subject string `json:"subject" doc:"Who to confer it on, as type:id or type:id#relation."                    maxLength:"191" minLength:"1"`
}

type writeVerbGrantInput struct {
	DeviceVerbRef

	Body verbGrantBody
}

type verbGrantOutput struct {
	Status int
	Body   VerbGrant
}

type withdrawVerbGrantInput struct {
	DeviceVerbRef

	Verb    string `query:"verb"    required:"true" doc:"The verb to stop conferring."                           example:"core.command.send_gcode"`
	Subject string `query:"subject" required:"true" doc:"Who to remove it from, as type:id or type:id#relation." example:"user:alice"`
}

type withdrawVerbGrantOutput struct {
	Status int
}

type acceptanceBody struct {
	Module string `json:"module"         doc:"The Go module path that declared the kind, as the inventory renders it."                                                                                 maxLength:"191" minLength:"1"`
	Verb   string `json:"verb,omitempty" doc:"The verb whose change is being accepted. Leave it out to accept a module rename, which moves the kind's whole first-seen record onto the new path."      maxLength:"160"`
	From   string `json:"from,omitempty" doc:"What the record holds today: the recorded class, or the old module path for a rename. It is read back in the answer and is not what decides the change."`
	To     string `json:"to"             doc:"What is being accepted: the class this build declares, or the new module path for a rename."                                                             maxLength:"191" minLength:"1"`
}

type acceptanceInput struct {
	Kind string `path:"kind" doc:"The device kind, qualified as the inventory renders it." maxLength:"128" example:"acme.io/filament-shelf"`

	Body acceptanceBody
}

type acceptanceOutput struct {
	Status int
	Body   struct {
		Kind   string `json:"kind"           doc:"The device kind the acceptance was recorded against."`
		Module string `json:"module"         doc:"The module path the kind's record now names."`
		Verb   string `json:"verb,omitempty" doc:"The verb whose class was accepted. Absent when the acceptance was a module rename."`
		To     string `json:"to"             doc:"What was accepted: the class the record now holds, or the module path it was moved to."`
	}
}

// AcceptedChange is one recorded acceptance on the wire.
type AcceptedChange struct {
	Kind     string `json:"kind"               doc:"The device kind the acceptance is about."`
	Module   string `json:"module"             doc:"The module path the kind's first-seen record is under."`
	Verb     string `json:"verb,omitempty"     doc:"The verb whose class change was accepted. Absent for a module rename."`
	To       string `json:"to"                 doc:"What was accepted: the class for a verb, or the module path the kind moved from for a rename."`
	Class    string `json:"class,omitempty"    doc:"The class the first-seen record holds for that verb today."`
	Markings string `json:"markings,omitempty" doc:"The markings the first-seen record holds for that verb today, as name:marker pairs."`
}

type listAcceptancesInput struct {
	Kind string `path:"kind" doc:"The device kind, qualified as the inventory renders it." maxLength:"128" example:"acme.io/filament-shelf"`
}

type listAcceptancesOutput struct {
	Body struct {
		Items []AcceptedChange `json:"items"`
	}
}

type subjectGrantsInput struct {
	Subject string `path:"subject" doc:"The subject, in tuple spelling: user:alice, service_account:ci, group:eng#member." maxLength:"191" example:"user:alice"`
}

type subjectDirectOutput struct {
	Body struct {
		Subject string  `json:"subject" doc:"The subject the grants are written for, as it was asked for."`
		Items   []Grant `json:"items"   doc:"Every grant written on a device for this subject, on the devices the caller administers. Empty when there is none."`
	}
}

type subjectGrantsOutput struct {
	Body struct {
		Subject string        `json:"subject" doc:"The subject the holdings belong to, as it was asked for."`
		Devices []SubjectHold `json:"devices" doc:"One entry per device the subject holds anything on. Empty when it holds nothing."`
	}
}

// SubjectHold is what one subject holds on one device: the classes, and the
// dangerous verbs their classes do not reach.
//
// It is rendered per kind. "Operator on a shelf" means nothing unless the
// answer says which of the shelf's verbs the class reaches, so the verbs are
// listed and the reader does not have to look them up in a table.
type SubjectHold struct {
	Device  string   `json:"device"  doc:"The device id the holding is on."`
	Kind    string   `json:"kind"    doc:"The device kind, which decides which verbs the actions reach."`
	Actions []string `json:"actions" doc:"The actions the subject holds on the device through its grants and the groups it is in."`
	// Verbs is every verb of this device's kind the actions above reach, and
	// nothing else. A dangerous verb is in it only when a row or ownership put
	// it there.
	Verbs []string `json:"verbs" doc:"Every verb of this device's kind the actions reach. A dangerous verb is here only when a grant or ownership put it here."`
}

// registerVerbGrants publishes the dangerous tier, the acceptance route and the
// grants-by-subject route.
func registerVerbGrants(api huma.API, store VerbGrantStore) {
	registerListVerbGrants(api, store)
	registerWriteVerbGrant(api, store)
	registerWithdrawVerbGrant(api, store)
	registerAcceptance(api, store)
	registerListAcceptances(api, store)
	registerSubjectGrants(api, store)
	registerSubjectDirectGrants(api, store)
}

func registerListVerbGrants(api huma.API, store VerbGrantStore) {
	register(api, huma.Operation{
		OperationID: "list-device-verb-grants",
		Method:      http.MethodGet,
		Path:        deviceVerbsPath,
		Summary:     "The dangerous-verb rows written on one device",
		Description: "A dangerous verb is one no class, role or level confers: raw gcode, and " +
			"anything a declaration or an operator marked irreversible or opaque-payload. Reaching " +
			"one takes a row naming the subject, the device and the verb, and these are those rows. " +
			"Reading them takes the same grant reading the tuples takes. A row whose verb this build " +
			"no longer classifies is listed with held false: it confers nothing, and it is kept so " +
			"somebody can see it rather than find it later.",
		Tags:   []string{"grants"},
		Errors: grantErrors,
	}, func(ctx context.Context, in *listVerbGrantsInput) (*listVerbGrantsOutput, error) {
		who, err := caller(ctx)
		if err != nil {
			return nil, err
		}

		object, err := authz.NewObject(authz.TypeDevice, in.ObjectID)
		if err != nil {
			return nil, err
		}

		held, err := store.VerbGrants(ctx, who, object)
		if err != nil {
			return nil, err
		}

		out := &listVerbGrantsOutput{}
		out.Body.Items = make([]VerbGrant, 0, len(held))

		for _, row := range held {
			out.Body.Items = append(out.Body.Items, verbGrantOf(row))
		}

		return out, nil
	})
}

func registerWriteVerbGrant(api huma.API, store VerbGrantStore) {
	register(api, huma.Operation{
		OperationID: "write-device-verb-grant",
		Method:      http.MethodPut,
		Path:        deviceVerbsPath,
		Summary:     "Confer one dangerous verb on one subject",
		Description: "Writes the row that reaches a dangerous verb. Two rules decide it, and they " +
			"are the rules a tuple grant is under: the caller administers the device, and the caller " +
			"already reaches that verb there. There is no level to fall back on, because no level " +
			"confers a dangerous verb, so what makes a first grant possible is ownership: the owner " +
			"of a machine reaches every dangerous verb of its kind, computed and not stored, and is " +
			"therefore who hands them out. " +
			"A verb the device's kind does not declare is refused, and so is a verb nothing marked " +
			"dangerous: that one is already conferred by its class, and a second path to one " +
			"permission is a second revocation story for it.",
		Tags:          []string{"grants"},
		DefaultStatus: http.StatusCreated,
		Errors:        grantErrors,
	}, func(ctx context.Context, in *writeVerbGrantInput) (*verbGrantOutput, error) {
		who, err := caller(ctx)
		if err != nil {
			return nil, err
		}

		grant, err := verbGrantFrom(in.ObjectID, in.Body.Verb, in.Body.Subject)
		if err != nil {
			return nil, err
		}

		wrote, err := store.WriteVerbGrant(ctx, who, grant)
		if err != nil {
			return nil, err
		}

		status := http.StatusCreated
		if !wrote {
			status = http.StatusOK
		}

		return &verbGrantOutput{Status: status, Body: verbGrantOf(grant)}, nil
	})
}

func registerWithdrawVerbGrant(api huma.API, store VerbGrantStore) {
	register(api, huma.Operation{
		OperationID: "withdraw-device-verb-grant",
		Method:      http.MethodDelete,
		Path:        deviceVerbsPath,
		Summary:     "Stop conferring one dangerous verb",
		Description: "Removes the row. It takes the grant that administers the device and answers " +
			"204 whether or not the row was there, so a client that retries a withdrawal is told the " +
			"same thing twice.",
		Tags:          []string{"grants"},
		DefaultStatus: http.StatusNoContent,
		Errors:        grantErrors,
	}, func(ctx context.Context, in *withdrawVerbGrantInput) (*withdrawVerbGrantOutput, error) {
		who, err := caller(ctx)
		if err != nil {
			return nil, err
		}

		grant, err := verbGrantFrom(in.ObjectID, in.Verb, in.Subject)
		if err != nil {
			return nil, err
		}

		if err = store.WithdrawVerbGrant(ctx, who, grant); err != nil {
			return nil, err
		}

		return &withdrawVerbGrantOutput{Status: http.StatusNoContent}, nil
	})
}

func registerAcceptance(api huma.API, store VerbGrantStore) {
	register(api, huma.Operation{
		OperationID: "accept-declaration-change",
		Method:      http.MethodPost,
		Path:        kindAcceptancesPath,
		Summary:     "Accept a change to a declared kind that the host is holding",
		Description: "This deployment records the class and the markings every declared verb was " +
			"first seen with. A rebuild that raises a class or adds a marking applies on its own, " +
			"because reach only narrows. One that lowers a class or removes a marking does not: the " +
			"verb keeps what the record holds, the disagreement is named in the authorization " +
			"vocabulary, and it applies from the next start once a row here accepts it by name. " +
			"That turns the one genuinely silent supply-chain move, a rebuild pulling a version that " +
			"quietly moves a verb down a class under grants written against the old one, into a " +
			"visible difference that does nothing until a person moves it. " +
			"Leaving verb out accepts a module rename instead: the kind's whole first-seen record " +
			"moves onto the new module path, carrying every class and marking unchanged, so what " +
			"moves is the publisher's name and never a pin.",
		Tags:          []string{"grants"},
		DefaultStatus: http.StatusCreated,
		Errors:        grantErrors,
	}, func(ctx context.Context, in *acceptanceInput) (*acceptanceOutput, error) {
		who, err := caller(ctx)
		if err != nil {
			return nil, err
		}

		if err = store.AcceptDeclarationChange(
			ctx, who, in.Body.Module, authz.DeviceKind(in.Kind), in.Body.Verb, in.Body.To,
		); err != nil {
			return nil, err
		}

		out := &acceptanceOutput{Status: http.StatusCreated}
		out.Body.Kind = in.Kind
		out.Body.Module = in.Body.Module
		out.Body.Verb = in.Body.Verb
		out.Body.To = in.Body.To

		return out, nil
	})
}

// registerListAcceptances is the read side of the route above.
//
// An acceptance takes effect at the next start, so without this route nothing
// would show what was accepted until the server restarts. The rows are read
// back beside the first-seen record each was written against, so a person can
// check an acceptance after the 201.
func registerListAcceptances(api huma.API, store VerbGrantStore) {
	register(api, huma.Operation{
		OperationID: "list-declaration-acceptances",
		Method:      http.MethodGet,
		Path:        kindAcceptancesPath,
		Summary:     "What has been accepted for one declared kind",
		Description: "Every acceptance recorded against this kind, with the class and markings the " +
			"first-seen record holds for that verb today. An acceptance applies from the next " +
			"start, so a row here whose class already matches the record has been applied and a row " +
			"whose class does not is waiting for a restart. A verb this deployment has no record " +
			"for cannot be accepted at all, so nothing here is a row that will never do anything.",
		Tags:   []string{"grants"},
		Errors: grantErrors,
	}, func(ctx context.Context, in *listAcceptancesInput) (*listAcceptancesOutput, error) {
		who, err := caller(ctx)
		if err != nil {
			return nil, err
		}

		recorded, err := store.Acceptances(ctx, who, authz.DeviceKind(in.Kind))
		if err != nil {
			return nil, err
		}

		out := &listAcceptancesOutput{}
		out.Body.Items = make([]AcceptedChange, 0, len(recorded))

		for _, change := range recorded {
			out.Body.Items = append(out.Body.Items, AcceptedChange{
				Kind:     change.Kind.String(),
				Module:   change.Module,
				Verb:     change.Verb,
				To:       change.To,
				Class:    change.Class,
				Markings: change.Markings,
			})
		}

		return out, nil
	})
}

func registerSubjectGrants(api huma.API, store VerbGrantStore) {
	register(api, huma.Operation{
		OperationID: "list-subject-grants",
		Method:      http.MethodGet,
		Path:        subjectGrantsPath,
		Summary:     "What one subject holds, by device",
		Description: "Answers the question an administrator actually asks, which is what a person " +
			"can do rather than which tuples exist. It is read from the same projection the point " +
			"check reads, so it cannot disagree with what the caller would be allowed to do. " +
			"Each device carries its kind, the classes the subject holds there, and every verb of " +
			"that kind those classes reach, plus any dangerous verb a row or ownership conferred. " +
			"That is what makes an answer readable: operator on a shelf means nothing until the " +
			"answer says which of the shelf's verbs it reaches.",
		Tags:   []string{"grants"},
		Errors: grantErrors,
	}, func(ctx context.Context, in *subjectGrantsInput) (*subjectGrantsOutput, error) {
		who, err := caller(ctx)
		if err != nil {
			return nil, err
		}

		subject, err := authz.ParseSubject(in.Subject)
		if err != nil {
			return nil, err
		}

		rows, err := store.SubjectDevices(ctx, who, subject)
		if err != nil {
			return nil, err
		}

		verbs, err := store.VerbsOfSubject(ctx, subject)
		if err != nil {
			return nil, err
		}

		out := &subjectGrantsOutput{}
		out.Body.Subject = subject.String()
		out.Body.Devices = make([]SubjectHold, 0, len(rows))

		for _, row := range rows {
			out.Body.Devices = append(out.Body.Devices, holdOf(row, verbs[row.ID]))
		}

		return out, nil
	})
}

func registerSubjectDirectGrants(api huma.API, store VerbGrantStore) {
	register(api, huma.Operation{
		OperationID: "list-subject-direct-grants",
		Method:      http.MethodGet,
		Path:        subjectDirectPath,
		Summary:     "The grants written for one subject, device by device",
		Description: "Every tuple written on a printer for this subject, a block included, on the printers " +
			"the caller administers. It is the per-subject read of list-grants, and it answers a " +
			"different question than list-subject-grants: these are the grants as written, so a page " +
			"can show and change them, and not the access they add up to. An org role is not here, " +
			"because it is written on the org. A caller who administers no printer gets an empty list.",
		Tags:   []string{"grants"},
		Errors: grantErrors,
	}, func(ctx context.Context, in *subjectGrantsInput) (*subjectDirectOutput, error) {
		who, err := caller(ctx)
		if err != nil {
			return nil, err
		}

		subject, err := authz.ParseSubject(in.Subject)
		if err != nil {
			return nil, err
		}

		held, err := store.SubjectGrants(ctx, who, subject)
		if err != nil {
			return nil, err
		}

		out := &subjectDirectOutput{}
		out.Body.Subject = subject.String()
		out.Body.Items = grantsOf(held)

		return out, nil
	})
}

// holdOf renders one device row per kind: the classes, and every verb of that
// kind they reach, with the dangerous ones a row conferred.
func holdOf(row authz.DeviceRow, conferred []string) SubjectHold {
	held := authz.MaskOf(row.Actions)

	out := SubjectHold{
		Device:  row.ID,
		Kind:    row.Kind.Or(authz.DeviceKindPrinter).String(),
		Actions: authz.ActionNames(row.Actions),
		Verbs:   []string{},
	}

	dangerous := map[string]bool{}
	for _, verb := range conferred {
		dangerous[verb] = true
	}

	out.Verbs = append(out.Verbs, authz.VerbsReached(row.Kind, held, dangerous)...)

	return out
}

// verbGrantFrom builds one row from the path and the body, through the
// constructor, so a row this surface cannot spell is refused here, before the
// statement.
func verbGrantFrom(device, verb, subject string) (authz.VerbGrant, error) {
	object, err := authz.NewObject(authz.TypeDevice, device)
	if err != nil {
		return authz.VerbGrant{}, err
	}

	who, err := authz.ParseSubject(subject)
	if err != nil {
		return authz.VerbGrant{}, err
	}

	return authz.NewVerbGrant(object, verb, who)
}

// verbGrantOf renders one row.
func verbGrantOf(grant authz.VerbGrant) VerbGrant {
	return VerbGrant{
		Device:  grant.Object().String(),
		Verb:    grant.Verb(),
		Subject: grant.Subject().String(),
	}
}

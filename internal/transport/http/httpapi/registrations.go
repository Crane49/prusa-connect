package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/admission"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

// The registration paths. A registration is addressed by the code the device is
// polling with, which is also the code an operator reads off its screen.
const (
	// registrationObject is what a refusal names this thing, in the one place
	// the two callers both read.
	registrationObject = "registration"

	registrationsPath      = "/api/v1/registrations"
	registrationPath       = "/api/v1/registrations/{code}"
	approveRegistrationURL = "/api/v1/registrations/{code}/approve"
)

// DeviceRegistrations is the devices asking to be let onto this server, as this
// surface reads and decides them. It is declared here because this is where it
// is consumed, and the composition root satisfies it with every connector that
// holds a registration table.
//
// No method takes a principal. Who may decide an admission is a question for the
// authorization graph, and it is asked here, on the resource kind a connector
// belongs to, so a second transport asking it gets the same answer.
type DeviceRegistrations interface {
	// Pending returns every device waiting for a decision, over every connector
	// that has a registration table, in a stable order.
	Pending(ctx context.Context) ([]Registration, error)

	// Approve lets one device through, so its next poll is answered with the
	// credential it then dials with. A code no connector holds is
	// admission.ErrUnknownCode.
	Approve(ctx context.Context, code string) (Registration, error)

	// Reject forgets one registration and every credential it held. A code no
	// connector holds is admission.ErrUnknownCode.
	Reject(ctx context.Context, code string) error
}

// Registration is one device waiting to be let onto this server.
//
// It carries no credential. The token a connector issues is what the machine
// presents on its own wire afterwards, and a surface an operator reads must not
// be a second way to obtain one.
type Registration struct {
	Code string `json:"code" doc:"What the device is polling with, and what an operator reads off its screen. It is the id every operation on this surface takes."`

	Connector string `json:"connector" doc:"The connector instance holding this registration. A deployment with several connectors reads it to know which wire the device is on."`

	Fingerprint string `json:"fingerprint" doc:"The identity the device keeps across reconnections. Once the registration is approved, this is what the connector's allowlist is keyed by."`

	// The three below are the machine's own spelling and each is absent for a
	// dialect whose handshake does not carry it. They are what an operator
	// matches against the machine in front of them before approving it.
	Serial   string `json:"serial,omitempty"   doc:"The serial number the device reported, in its own spelling."`
	Model    string `json:"model,omitempty"    doc:"The model the device reported, in its own spelling."`
	Firmware string `json:"firmware,omitempty" doc:"The firmware version the device reported, in its own spelling."`

	Approved bool `json:"approved" doc:"Whether anything has let this device through: an operator, or a connector configured to approve on arrival. The listing carries only registrations waiting for a decision, so it is false on every row there."`

	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	DeviceID string `json:"device_id,omitempty" doc:"The device this approval bound the machine to, which is the id every device operation takes. It is on the answer to an approval, so a page that just let a machine through opens its device page without matching serial numbers against the fleet. It is absent on a row waiting for a decision, because no device exists until somebody decides, and absent on an approval that bound none: a registration carrying no fingerprint has nothing to bind an id to, and such a machine is given one at its first handshake instead."`
}

type listRegistrationsOutput struct {
	Body struct {
		Items []Registration `json:"items"`
	}
}

// RegistrationRef is the registration a path names. It is exported because Huma
// binds parameters by reflection and cannot write into an unexported embedded
// field.
type RegistrationRef struct {
	Code string `path:"code" doc:"The registration code, as the listing reports it and as the device displays it." maxLength:"64" minLength:"1" example:"K7PQ2XR4MN"`
}

type approveRegistrationInput struct {
	RegistrationRef
}

type approveRegistrationOutput struct {
	Body Registration
}

type rejectRegistrationInput struct {
	RegistrationRef
}

type rejectRegistrationOutput struct {
	Status int
}

// registrationErrors are the refusals every operation on this surface answers
// with. Deciding admission is one permission, so the listing and the two
// decisions refuse the same caller in the same words.
var registrationErrors = []int{
	http.StatusUnauthorized,
	http.StatusForbidden,
}

// codedRegistrationErrors adds the refusal the two operations that name a code
// have. A code no connector holds and a code somebody already rejected are the
// same 404, so nothing here tells a caller which codes this server has seen.
var codedRegistrationErrors = append([]int{http.StatusNotFound}, registrationErrors...)

// registerRegistrations publishes the operator's half of admission.
//
// The three operations are registered whatever this process was wired with. The
// document is a contract, so a deployment whose connectors hold no registration
// table publishes the same paths and answers an empty listing. A client never
// meets the difference as an unrouted path.
func registerRegistrations(api huma.API, cfg Config) {
	registerListRegistrations(api, cfg)
	registerApproveRegistration(api, cfg)
	registerRejectRegistration(api, cfg)
}

func registerListRegistrations(api huma.API, cfg Config) {
	register(api, huma.Operation{
		OperationID: "list-pending-registrations",
		Method:      http.MethodGet,
		Path:        registrationsPath,
		Summary:     "The devices waiting to be let onto this server",
		Description: "Every device that has introduced itself over a connector's own wire and is " +
			"waiting for somebody to decide. Each row carries the code the device is displaying and " +
			"what the machine said it is, so an operator can match a row against the machine in " +
			"front of them before approving it.\n" +
			"\n" +
			"A connector configured to approve on arrival puts nothing here, because nothing waits. " +
			"So does a deployment whose connectors hold no registration table at all: the answer is " +
			"an empty list rather than a refusal.\n" +
			"\n" +
			"No row carries a credential. The token a connector issues is what the machine presents " +
			"on its own wire, and this listing is not a second way to obtain one. It takes " +
			"admit_device on the driver resource kind.",
		Tags:   []string{"registrations"},
		Errors: registrationErrors,
	}, func(ctx context.Context, _ *struct{}) (*listRegistrationsOutput, error) {
		if err := requireRegistrationAdmin(ctx, cfg); err != nil {
			return nil, err
		}

		out := &listRegistrationsOutput{}
		out.Body.Items = []Registration{}

		if cfg.Registrations == nil {
			return out, nil
		}

		waiting, err := cfg.Registrations.Pending(ctx)
		if err != nil {
			return nil, err
		}

		out.Body.Items = append(out.Body.Items, waiting...)

		return out, nil
	})
}

func registerApproveRegistration(api huma.API, cfg Config) {
	register(api, huma.Operation{
		OperationID: "approve-registration",
		Method:      http.MethodPost,
		Path:        approveRegistrationURL,
		Summary:     "Let one device onto this server",
		Description: "Approves the registration, so the device's next poll is answered with the " +
			"credential it then dials with. The device appears on `/api/v1/devices` once it has " +
			"dialled, which is its own next step and not this one: approving decides admission and " +
			"does not open a connection.\n" +
			"\n" +
			"Approving twice is the same answer as approving once. A code no connector holds is a " +
			"404, and so is one that was already rejected. It takes admit_device on the driver " +
			"resource kind.",
		Tags:   []string{"registrations"},
		Errors: codedRegistrationErrors,
	}, func(ctx context.Context, in *approveRegistrationInput) (*approveRegistrationOutput, error) {
		if err := requireRegistrationAdmin(ctx, cfg); err != nil {
			return nil, err
		}

		if cfg.Registrations == nil {
			return nil, unknownRegistration(in.Code)
		}

		approved, err := cfg.Registrations.Approve(ctx, in.Code)
		if err != nil {
			return nil, registrationFault(err, in.Code)
		}

		return &approveRegistrationOutput{Body: approved}, nil
	})
}

func registerRejectRegistration(api huma.API, cfg Config) {
	register(api, huma.Operation{
		OperationID: "reject-registration",
		Method:      http.MethodDelete,
		Path:        registrationPath,
		Summary:     "Turn one device away",
		Description: "Forgets the registration and every credential it held, so a device that was " +
			"already approved stops being accepted on the connector's wire.\n" +
			"\n" +
			"It is not a permanent refusal. The device registers again at its next boot and is given " +
			"a new code, so rejecting a machine somebody plugged in by mistake costs that machine one " +
			"handshake. To keep a device off this server for good, take the port away from it.\n" +
			"\n" +
			"A code no connector holds is a 404, and so is one that was already rejected. It takes " +
			"admit_device on the driver resource kind.",
		Tags:   []string{"registrations"},
		Errors: codedRegistrationErrors,
	}, func(ctx context.Context, in *rejectRegistrationInput) (*rejectRegistrationOutput, error) {
		if err := requireRegistrationAdmin(ctx, cfg); err != nil {
			return nil, err
		}

		if cfg.Registrations == nil {
			return nil, unknownRegistration(in.Code)
		}

		if err := cfg.Registrations.Reject(ctx, in.Code); err != nil {
			return nil, registrationFault(err, in.Code)
		}

		return &rejectRegistrationOutput{Status: http.StatusNoContent}, nil
	})
}

// requireRegistrationAdmin refuses a caller who may not decide who gets onto
// this server.
//
// The registration table belongs to a connector, and a connector is an instance
// of the driver resource kind, so the object is that kind. The action is
// admit_device, which the resource role layer confers at manager, so an org
// admin holds it from the start. It is separate from configure_resource, so a
// site can let somebody decide which machines join the fleet without giving
// them the settings that say what this server dials.
func requireRegistrationAdmin(ctx context.Context, cfg Config) error {
	const what = "which devices are let onto this server"

	who, object, err := kindDecision(ctx, ref.KindDriver)
	if err != nil {
		return err
	}

	allowed, err := cfg.Grants.Can(ctx, who, authz.ActionAdmitDevice, object)

	return decided(allowed, err, object, authz.ActionAdmitDevice, what)
}

// unknownRegistration is the refusal for a code nothing holds. A deployment
// whose connectors keep no registration table answers it in the same bytes as
// one whose table does not carry the code.
func unknownRegistration(code string) error {
	return httpfault.ResourceNotFound(httpfault.Reference(registrationObject, code))
}

// registrationFault renders what a connector said. A code nothing holds is the
// one refusal a caller can act on. It is decided here and not in each dialect,
// so two connectors cannot report it as two different statuses.
func registrationFault(err error, code string) error {
	if errors.Is(err, admission.ErrUnknownCode) {
		return unknownRegistration(code)
	}

	return httpfault.Unexpected("decide a registration").Wrap(err)
}

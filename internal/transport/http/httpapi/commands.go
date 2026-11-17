package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// Where a command is read back from.
//
// A command's three transitions are readable one frame at a time under
// /history/facts, but a client reading them there has to fold a record the
// store has already folded. These two paths answer the folded row: what was
// asked of one machine, and what happened to this one id.
const (
	deviceCommandHistoryPath = devicePath + "/commands"
	commandPath              = "/api/v1/commands/{command_id}"
)

// HistoryCommand is one invocation as the store folded it.
//
// The three audit members are absent for a caller who does not hold view_audit
// on the device. The event stream and the fact query narrow the same way. What
// a command was and how it ended is not audit. Who authorized it is audit.
type HistoryCommand struct {
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	ID string `json:"command_id" doc:"The invocation's id, minted by this server before the command was authorized. It is the value a caller was handed when it sent the command, and the value every one of the command's durable frames carries."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Origin string `json:"origin" doc:"The device the command was issued against, as kind:id."`
	Verb   string `json:"verb"   doc:"The capability key that was invoked."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Key string `json:"key,omitempty" doc:"The caller's idempotency key, absent when none was sent."`

	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Requested time.Time `json:"requested_at,omitzero" doc:"When the instruction was published. Absent for an invocation this server stored an outcome for and never an instruction, which a replay can produce."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Authorized time.Time `json:"authorized_at,omitzero" doc:"When the authorization was recorded. Absent for a caller who does not hold view_audit on the device, and for a command that was never authorized."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Settled time.Time `json:"settled_at,omitzero" doc:"When the outcome was published. Absent while the invocation is open."`

	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Status string `json:"status,omitempty" doc:"How the invocation ended: succeeded, failed, refused, timed_out, abandoned, unauthorized, or server_restarted. Absent while it is open. It is an open set: a later build may add a word, so show an unknown one rather than refusing the record."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	OK bool `json:"ok" doc:"Whether the device accepted the command. Meaningless when outcome_unknown is set."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	OutcomeUnknown bool `json:"outcome_unknown,omitempty" doc:"True when this server never observed how the command ended. A command whose caller went away, one whose device lost its connection after the command was sent, and one this server found open after a restart may all have been executed by the machine: the record says it does not know rather than guessing."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	MachineReason string `json:"machine_reason,omitempty" doc:"The device's own word for a refusal, passed through as it spelled it."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	FaultCode string `json:"fault_code,omitempty" doc:"The registered error code of a failure, absent when the command did not fail with one."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	ElapsedMS int64 `json:"elapsed_ms" doc:"How long the invocation took, in milliseconds."`

	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	AuthType string `json:"auth_type,omitempty" doc:"The type of the principal that authorized the command. Present only for a caller holding view_audit on the device."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	AuthID string `json:"auth_id,omitempty" doc:"The id of the principal that authorized the command. Present only for a caller holding view_audit on the device."`
}

// HistoryCommandPage is one page of folded commands, newest requested first.
type HistoryCommandPage struct {
	Items []HistoryCommand `json:"items"`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Cursor string `json:"cursor" doc:"Opaque text the provider minted, to be sent back as the cursor parameter for the next page. Empty when this page is the last one."`
}

// HistoryCommandQuery asks for folded command rows.
type HistoryCommandQuery struct {
	// Origins is what the caller asked for and never what they may read. The
	// implementation reduces it against the authorization graph.
	Origins []event.Origin
	// IDs narrows to these command ids, and empty is every id in the window.
	IDs []string
	// Open narrows to invocations with no outcome.
	Open   bool
	From   time.Time
	To     time.Time
	Limit  int
	Cursor string
}

// CommandHistory is the folded command record as this transport reads it. The
// composition root satisfies it with whatever history provider the deployment
// configured, and a deployment with none answers 501.
type CommandHistory interface {
	// Commands answers one page of folded commands, narrowed to what this
	// principal may read and with the audit members stripped where they may not
	// see them.
	Commands(ctx context.Context, principal string, query HistoryCommandQuery) (HistoryCommandPage, error)
}

// deviceCommandsInput is one device's command record.
type deviceCommandsInput struct {
	HistoryWindowParams

	ID string `path:"device_id" doc:"The device this server minted an id for when it dialled in." example:"9f2a4c18-6d3b-4e07-b5a1-8c0f2d7e4b93"`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Open bool `query:"open" doc:"Narrow to invocations this server has no outcome for. A command in flight is open; so is one a restart found and closed, until the closing frame has been stored." example:"true"`
}

// commandInput is one invocation by its id.
type commandInput struct {
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	ID string `path:"command_id" maxLength:"64" doc:"The invocation's id, as the server answered it when the command was sent." example:"01K3Q7W0YQ4S2C4H2S8W9YQ7V1"`
}

type commandsOutput struct {
	Body HistoryCommandPage
}

type commandOutput struct {
	Body HistoryCommand
}

// The prose the two command operations share.
const commandIntro = "Reads the folded command record, which is what a history " +
	"provider made of the three durable frames every invocation publishes: the " +
	"instruction, the audit record of who authorized it, and the outcome. A " +
	"deployment that configures no provider keeps no record and answers 501.\n\n"

const commandNarrowing = "\n\nWho authorized a command is audit, and the three " +
	"audit members are absent for a caller who does not hold view_audit on the " +
	"device. What the command was and how it ended is not audit and is answered " +
	"to anybody who may read the device. An invocation that was refused before it " +
	"reached the machine is a record like any other: the id is minted before the " +
	"authorization check, so a caller who was told no can read back what they asked for."

// registerCommands publishes the two read paths over the folded command record.
func registerCommands(api huma.API, cfg Config) {
	registerDeviceCommandHistory(api, cfg)
	registerCommand(api, cfg)
}

func registerDeviceCommandHistory(api huma.API, cfg Config) {
	register(api, huma.Operation{
		OperationID: "list-device-commands",
		Method:      http.MethodGet,
		Path:        deviceCommandHistoryPath,
		Summary:     "Read one device's command record",
		Description: commandIntro + "Every command this server stored for one device, newest " +
			"requested first. A device this server has never met and one the caller may not read " +
			"are the same refusal, so this path never says which devices exist." + commandNarrowing,
		Tags:   []string{"history"},
		Errors: []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusNotImplemented},
	}, func(ctx context.Context, in *deviceCommandsInput) (*commandsOutput, error) {
		record, err := resolveCommands(cfg)
		if err != nil {
			return nil, err
		}

		if _, err = cfg.Devices.Get(ctx, principalFrom(ctx), in.ID); err != nil {
			return nil, err
		}

		origin, err := event.NewOrigin(event.KindDevice, in.ID)
		if err != nil {
			return nil, httpfault.RequestInvalid("path.device_id",
				"it is not an id this server could have minted", err.Error())
		}

		page, err := record.Commands(ctx, principalFrom(ctx), HistoryCommandQuery{
			Origins: []event.Origin{origin},
			Open:    in.Open,
			From:    in.From,
			To:      in.To,
			Limit:   in.Limit,
			Cursor:  in.Cursor,
		})
		if err != nil {
			return nil, err
		}

		return &commandsOutput{Body: emptyCommands(page)}, nil
	})
}

func registerCommand(api huma.API, cfg Config) {
	register(api, huma.Operation{
		OperationID: "get-command",
		Method:      http.MethodGet,
		Path:        commandPath,
		Summary:     "Read one command",
		Description: commandIntro + "One invocation by the id this server minted for it. An id " +
			"belonging to a device the caller may not read is answered exactly as an id nothing " +
			"was ever stored under, so this path discloses no command the caller could not have " +
			"listed." + commandNarrowing,
		Tags:   []string{"history"},
		Errors: []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusNotImplemented},
	}, func(ctx context.Context, in *commandInput) (*commandOutput, error) {
		record, err := resolveCommands(cfg)
		if err != nil {
			return nil, err
		}

		page, err := record.Commands(ctx, principalFrom(ctx),
			HistoryCommandQuery{IDs: []string{in.ID}, Limit: 1})
		if err != nil {
			return nil, err
		}

		if len(page.Items) == 0 {
			return nil, httpfault.ResourceNotFound(httpfault.Reference("command", in.ID))
		}

		return &commandOutput{Body: page.Items[0]}, nil
	})
}

// resolveCommands is the command record this request reads, or the refusal
// when there is none. The history provider answers both the history and the
// command record, and a build that wired none refuses both the same way.
func resolveCommands(cfg Config) (CommandHistory, error) {
	past, err := resolveHistory(cfg)
	if err != nil {
		return nil, err
	}

	record, ok := past.(CommandHistory)
	if !ok {
		return nil, httpfault.HistoryNotConfigured()
	}

	return record, nil
}

// emptyCommands renders a page with no rows as an empty array and never as
// null, so a client can iterate the member without a null check.
func emptyCommands(page HistoryCommandPage) HistoryCommandPage {
	if page.Items == nil {
		page.Items = []HistoryCommand{}
	}

	return page
}

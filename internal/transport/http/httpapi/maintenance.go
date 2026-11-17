package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/prusa3d/prusa-connect/internal/betaportal"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/internal/upgrade"
)

const (
	serverVersionPath = "/api/v1/server/version"
	upgradePath       = "/api/v1/server/upgrade"
	upgradeReleases   = upgradePath + "/releases"
	upgradeHistory    = upgradePath + "/history"
	upgradeStaged     = upgradePath + "/staged"
	upgradeBundle     = upgradeStaged + "/bundle"
	upgradeApply      = upgradePath + "/apply"
	backupsPath       = "/api/v1/server/backups"
	restartPath       = "/api/v1/server/restart"
)

type Maintenance interface {
	ServerVersion(context.Context) (ServerVersion, error)
	UpgradeStatus(context.Context) (upgrade.Journal, error)
	UpgradeHistory(context.Context) ([]upgrade.Journal, error)
	Releases(context.Context) ([]upgrade.Release, error)
	Stage(context.Context, MaintenanceStage) (upgrade.Journal, error)
	// StageBundle stages a signed offline bundle read from body as it arrives.
	StageBundle(ctx context.Context, body io.Reader, principal string) (upgrade.Journal, error)
	Prepare(context.Context) (upgrade.Journal, error)
	Apply(context.Context, MaintenanceApply) (upgrade.Journal, error)
	Cancel(context.Context) error
	Backups(context.Context) ([]upgrade.BackupManifest, error)
	CreateBackup(context.Context, string, bool) (upgrade.BackupManifest, error)
	RestoreBackup(context.Context, string) error
	DeleteBackup(context.Context, string) error
	// Restart stops the server and starts it again, in safe mode when the
	// request says so. It returns once the restart is requested.
	Restart(context.Context, MaintenanceRestart) error
}

// MaintenanceRestart is a request to restart the server.
type MaintenanceRestart struct {
	// Mode is RestartNormal or RestartSafe.
	Mode      string
	Principal string
}

// The two modes a restart asks for.
const (
	RestartNormal = "normal"
	RestartSafe   = "safe"
)

type restartInput struct {
	Body struct {
		Mode string `json:"mode" enum:"normal,safe" doc:"safe starts the server with every module off, once; normal starts it as configured, and leaves safe mode when the server is in it."`
	}
}

type restartOutput struct {
	Status int
	Body   struct {
		Mode string `json:"mode" enum:"normal,safe" doc:"The mode the server restarts in."`
	}
}

type ServerVersion struct {
	Version           string   `json:"version"                  doc:"The semantic version reported by this core binary."`
	BuildProfile      string   `json:"build_profile"            doc:"The feature profile linked into this binary."`
	BuildVariant      string   `json:"build_variant"            doc:"The full, minimal or development feature set."`
	GOOS              string   `json:"goos"                     doc:"The operating system this binary targets."`
	GOARCH            string   `json:"goarch"                   doc:"The processor architecture this binary targets."`
	Contract          string   `json:"contract"                 doc:"The module wire contract this binary serves."`
	Capabilities      []string `json:"capabilities,omitempty"   doc:"The host capabilities this build serves to extensions."`
	HostSchemaVersion int      `json:"host_schema_version"      doc:"The highest host schema version this build understands."`
	HostSchemaCompat  int      `json:"host_schema_compat"       doc:"The oldest host schema version that may open data migrated by this build."`
	ManagedExternally bool     `json:"managed_externally"       doc:"Whether another system owns replacement of the executable."`
	ManagedReason     string   `json:"managed_reason,omitempty" doc:"Why another system owns replacement of the executable."`
	// The field name keeps the request member it governs, postgres_backup_ack.
	PostgresAckRequired bool `json:"postgres_backup_ack_required,omitempty" doc:"Whether applying an update needs postgres_backup_ack, because a PostgreSQL database is outside the backup the update takes."`
}

type MaintenanceStage struct {
	Version   string
	Name      string
	Binary    []byte
	Sums      []byte
	Signature []byte
	Principal string
}

type MaintenanceApply struct {
	Schedule      string
	ScheduledFor  *time.Time
	ForcePrinting bool
	PostgresAck   bool
	// ApproveExtensions approves what the prepared journal's extension
	// reviews name: new asks and versions older than the installed ones.
	ApproveExtensions bool
	Principal         string
}

type maintenanceOutput[T any] struct {
	Status int
	Body   T
}

// noContentOutput answers 204 with no body. A Body field, even an empty one,
// declares a JSON response that a client then tries to parse.
type noContentOutput struct {
	Status int
}

type stageInput struct {
	Body struct {
		Version   string `json:"version,omitempty" minLength:"1" doc:"The release version being staged."`
		Name      string `json:"name,omitempty" doc:"The binary's name in SHA256SUMS; omitted for a server download."`
		Binary    []byte `json:"binary,omitempty" doc:"The release binary, base64 encoded by JSON; omitted for a server download."`
		Sums      []byte `json:"sha256sums,omitempty" doc:"The signed SHA256SUMS bytes, base64 encoded by JSON; omitted for a server download."`
		Signature []byte `json:"signature,omitempty" doc:"The detached signature bytes, base64 encoded by JSON; omitted for a server download."`
	}
}

type applyInput struct {
	Body struct {
		Schedule          string     `json:"schedule" enum:"now,when-idle,at" doc:"When the prepared upgrade may start."`
		ScheduledFor      *time.Time `json:"scheduled_for,omitempty" doc:"The instant for an at schedule."`
		ForcePrinting     bool       `json:"force_while_printing,omitempty" doc:"Allow a restart while a printer reports active work."`
		PostgresAck       bool       `json:"postgres_backup_ack,omitempty" doc:"Confirm the configured external or omitted PostgreSQL backup."`
		ApproveExtensions bool       `json:"approve_extensions,omitempty" doc:"Approve what extension_reviews in the prepared upgrade name: new asks and versions older than the installed ones. Applying refuses without it when a review needs approval."`
	}
}

type backupInput struct {
	ToVersion   string `query:"to_version"          example:"1.3.0" doc:"The release this manual backup precedes, when known."`
	PostgresAck bool   `query:"postgres_backup_ack" example:"false" doc:"Confirm that PostgreSQL is protected externally when pg_dump is not configured."`
}

type backupRef struct {
	ID string `path:"id" minLength:"1" example:"6f1c0a2d5b8e4711" doc:"The backup identifier."`
}

//nolint:gocognit // the branches are independent route handlers kept beside their contracts
func registerMaintenance(api huma.API, cfg Config) {
	register(api, huma.Operation{
		OperationID: "get-server-version",
		Method:      http.MethodGet,
		Path:        serverVersionPath,
		Summary:     "The running core version",
		Tags:        []string{"system"},
		Description: "Returns build and schema compatibility metadata. An organization administrator uses it to select a compatible release.",
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, func(ctx context.Context, _ *struct{}) (*maintenanceOutput[ServerVersion], error) {
		if err := requireMaintenance(ctx, cfg); err != nil {
			return nil, err
		}

		version, err := cfg.Maintenance.ServerVersion(ctx)
		if err != nil {
			return nil, maintenanceFailure("read the server version", err)
		}

		return &maintenanceOutput[ServerVersion]{Status: http.StatusOK, Body: version}, nil
	})

	register(api, huma.Operation{
		OperationID: "get-server-upgrade",
		Method:      http.MethodGet,
		Path:        upgradePath,
		Summary:     "The current upgrade transaction",
		Tags:        []string{"system"},
		Description: "Returns the durable state of the active transaction. It returns not found when no transaction exists." + upgradesOffNote,
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, func(ctx context.Context, _ *struct{}) (*maintenanceOutput[upgrade.Journal], error) {
		if err := requireUpgrades(ctx, cfg); err != nil {
			return nil, err
		}

		status, err := cfg.Maintenance.UpgradeStatus(ctx)
		if err != nil {
			return nil, maintenanceFailure("read the upgrade", err)
		}

		return &maintenanceOutput[upgrade.Journal]{Status: http.StatusOK, Body: status}, nil
	})

	register(api, huma.Operation{
		OperationID: "list-server-upgrade-history",
		Method:      http.MethodGet,
		Path:        upgradeHistory,
		Summary:     "Completed core upgrade attempts",
		Tags:        []string{"system"},
		Description: "Returns durable terminal upgrade records in chronological order. Use the backup identifier to inspect a retained rollback snapshot." + upgradesOffNote,
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, func(ctx context.Context, _ *struct{}) (*maintenanceOutput[[]upgrade.Journal], error) {
		if err := requireUpgrades(ctx, cfg); err != nil {
			return nil, err
		}

		history, err := cfg.Maintenance.UpgradeHistory(ctx)
		if err != nil {
			return nil, maintenanceFailure("read upgrade history", err)
		}
		if history == nil {
			history = []upgrade.Journal{}
		}

		return &maintenanceOutput[[]upgrade.Journal]{Status: http.StatusOK, Body: history}, nil
	})

	register(api, huma.Operation{
		OperationID: "list-server-upgrade-releases",
		Method:      http.MethodGet,
		Path:        upgradeReleases,
		Summary:     "Core releases available to this server",
		Tags:        []string{"system"},
		Description: "Returns signed releases compatible with this build. Select a release version and pass it to the staging operation." + upgradesOffNote,
		Errors: []int{
			http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
			http.StatusTooManyRequests, http.StatusBadGateway,
		},
	}, func(ctx context.Context, _ *struct{}) (*maintenanceOutput[[]upgrade.Release], error) {
		if err := requireUpgrades(ctx, cfg); err != nil {
			return nil, err
		}

		releases, err := cfg.Maintenance.Releases(ctx)
		if err != nil {
			return nil, maintenanceFailure("list releases", err)
		}
		if releases == nil {
			releases = []upgrade.Release{}
		}

		return &maintenanceOutput[[]upgrade.Release]{Status: http.StatusOK, Body: releases}, nil
	})

	register(api, huma.Operation{
		OperationID:   "stage-server-upgrade",
		Method:        http.MethodPost,
		Path:          upgradeStaged,
		Summary:       "Stage and verify a core release",
		Tags:          []string{"system"},
		Description:   "Downloads the release a version names from the update server, or takes a release binary with its SHA256SUMS and signature in the body, and verifies its signed identity. The body carries the files base64 encoded, so it suits a small scripted upload; an offline bundle goes up with stage-server-upgrade-bundle, which streams it. Prepare the returned transaction before applying it." + upgradesOffNote,
		DefaultStatus: http.StatusCreated,
		Errors: []int{
			http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
			http.StatusConflict, http.StatusUnprocessableEntity, http.StatusTooManyRequests,
			http.StatusBadGateway,
		},
	}, func(ctx context.Context, input *stageInput) (*maintenanceOutput[upgrade.Journal], error) {
		if err := requireUpgrades(ctx, cfg); err != nil {
			return nil, err
		}

		journal, err := cfg.Maintenance.Stage(ctx, MaintenanceStage{
			Version: input.Body.Version, Name: input.Body.Name, Binary: input.Body.Binary,
			Sums: input.Body.Sums, Signature: input.Body.Signature, Principal: principalFrom(ctx),
		})
		if err != nil {
			return nil, maintenanceFailure("stage the upgrade", err)
		}

		return &maintenanceOutput[upgrade.Journal]{Status: http.StatusCreated, Body: journal}, nil
	})

	register(api, huma.Operation{
		OperationID:   "stage-server-upgrade-bundle",
		Method:        http.MethodPost,
		Path:          upgradeBundle,
		Summary:       "Stage and verify a signed offline bundle",
		Tags:          []string{"system"},
		Description:   "Takes a signed offline update bundle as the request body and verifies it as it arrives, so nothing here holds the file. The bundle's signed manifest names the core version. Prepare the returned transaction before applying it." + upgradesOffNote,
		DefaultStatus: http.StatusCreated,
		RequestBody:   objectRequestBody(),
		Metadata:      streams(),
		Errors: []int{
			http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
			http.StatusConflict, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity,
		},
	}, func(ctx context.Context, _ *struct{}) (*maintenanceOutput[upgrade.Journal], error) {
		if err := requireUpgrades(ctx, cfg); err != nil {
			return nil, err
		}

		body, err := uploadBody(ctx)
		if err != nil {
			return nil, err
		}

		journal, err := cfg.Maintenance.StageBundle(ctx, body, principalFrom(ctx))
		if err != nil {
			return nil, maintenanceFailure("stage the offline bundle", err)
		}

		return &maintenanceOutput[upgrade.Journal]{Status: http.StatusCreated, Body: journal}, nil
	})

	register(api, huma.Operation{
		OperationID: "prepare-server-upgrade",
		Method:      http.MethodPost,
		Path:        upgradePath,
		Summary:     "Preflight and rehearse the staged release",
		Tags:        []string{"system"},
		Description: "Checks platform, capacity, activity, configuration and migrations. Resolve any reported conflict before retrying or applying the prepared transaction." + upgradesOffNote,
		Errors: []int{
			http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
			http.StatusConflict, http.StatusUnprocessableEntity,
		},
	}, func(ctx context.Context, _ *struct{}) (*maintenanceOutput[upgrade.Journal], error) {
		if err := requireUpgrades(ctx, cfg); err != nil {
			return nil, err
		}

		journal, err := cfg.Maintenance.Prepare(ctx)
		if err != nil {
			return nil, maintenanceFailure("prepare the upgrade", err)
		}

		return &maintenanceOutput[upgrade.Journal]{Status: http.StatusOK, Body: journal}, nil
	})

	register(api, huma.Operation{
		OperationID:   "apply-server-upgrade",
		Method:        http.MethodPost,
		Path:          upgradeApply,
		Summary:       "Apply or schedule the prepared release",
		Tags:          []string{"system"},
		Description:   "Authorizes immediate or scheduled replacement after a successful rehearsal. Active work and unconfirmed PostgreSQL protection refuse unsafe requests." + upgradesOffNote,
		DefaultStatus: http.StatusAccepted,
		Errors: []int{
			http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
			http.StatusConflict, http.StatusUnprocessableEntity,
		},
	}, func(ctx context.Context, input *applyInput) (*maintenanceOutput[upgrade.Journal], error) {
		if err := requireUpgrades(ctx, cfg); err != nil {
			return nil, err
		}

		journal, err := cfg.Maintenance.Apply(ctx, MaintenanceApply{
			Schedule: input.Body.Schedule, ScheduledFor: input.Body.ScheduledFor,
			ForcePrinting: input.Body.ForcePrinting, PostgresAck: input.Body.PostgresAck,
			ApproveExtensions: input.Body.ApproveExtensions, Principal: principalFrom(ctx),
		})
		if err != nil {
			return nil, maintenanceFailure("apply the upgrade", err)
		}

		return &maintenanceOutput[upgrade.Journal]{Status: http.StatusAccepted, Body: journal}, nil
	})

	register(api, huma.Operation{
		OperationID:   "cancel-server-upgrade",
		Method:        http.MethodDelete,
		Path:          upgradePath,
		Summary:       "Cancel a staged or scheduled release",
		Tags:          []string{"system"},
		Description:   "Removes a transaction before draining begins. A transaction already changing server state returns a conflict." + upgradesOffNote,
		DefaultStatus: http.StatusNoContent,
		Errors: []int{
			http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
			http.StatusConflict, http.StatusUnprocessableEntity,
		},
	}, func(ctx context.Context, _ *struct{}) (*noContentOutput, error) {
		if err := requireUpgrades(ctx, cfg); err != nil {
			return nil, err
		}
		if err := cfg.Maintenance.Cancel(ctx); err != nil {
			return nil, maintenanceFailure("cancel the upgrade", err)
		}

		return &noContentOutput{Status: http.StatusNoContent}, nil
	})

	registerBackupOperations(api, cfg)
}

func registerBackupOperations(api huma.API, cfg Config) {
	register(api, huma.Operation{
		OperationID: "list-server-backups",
		Method:      http.MethodGet,
		Path:        backupsPath,
		Summary:     "Protected core backups",
		Tags:        []string{"system"},
		Description: "Returns verified backup manifests retained by this server. Use an identifier to restore or delete one snapshot." + upgradesOffNote,
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, func(ctx context.Context, _ *struct{}) (*maintenanceOutput[[]upgrade.BackupManifest], error) {
		if err := requireUpgrades(ctx, cfg); err != nil {
			return nil, err
		}

		backups, err := cfg.Maintenance.Backups(ctx)
		if err != nil {
			return nil, maintenanceFailure("list backups", err)
		}
		if backups == nil {
			backups = []upgrade.BackupManifest{}
		}

		return &maintenanceOutput[[]upgrade.BackupManifest]{Status: http.StatusOK, Body: backups}, nil
	})

	register(api, huma.Operation{
		OperationID:   "create-server-backup",
		Method:        http.MethodPost,
		Path:          backupsPath,
		Summary:       "Create a protected core backup",
		Tags:          []string{"system"},
		Description:   "Creates and verifies a consistent server snapshot. External PostgreSQL protection requires explicit operator confirmation." + upgradesOffNote,
		DefaultStatus: http.StatusCreated,
		Errors: []int{
			http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
			http.StatusConflict, http.StatusUnprocessableEntity,
		},
	}, func(ctx context.Context, input *backupInput) (*maintenanceOutput[upgrade.BackupManifest], error) {
		if err := requireUpgrades(ctx, cfg); err != nil {
			return nil, err
		}

		backup, err := cfg.Maintenance.CreateBackup(ctx, input.ToVersion, input.PostgresAck)
		if err != nil {
			return nil, maintenanceFailure("create a backup", err)
		}

		return &maintenanceOutput[upgrade.BackupManifest]{Status: http.StatusCreated, Body: backup}, nil
	})

	registerRestart(api, cfg)

	register(api, huma.Operation{
		OperationID:   "restore-server-backup",
		Method:        http.MethodPost,
		Path:          backupsPath + "/{id}/restore",
		Summary:       "Restore a protected core backup",
		Tags:          []string{"system"},
		Description:   "Schedules offline restoration after database shutdown. The server restarts with the matching retained executable when the backup predates an update." + upgradesOffNote,
		DefaultStatus: http.StatusAccepted,
		Errors: []int{
			http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
			http.StatusConflict, http.StatusUnprocessableEntity,
		},
	}, func(ctx context.Context, input *backupRef) (*maintenanceOutput[struct{}], error) {
		if err := requireUpgrades(ctx, cfg); err != nil {
			return nil, err
		}
		if err := cfg.Maintenance.RestoreBackup(ctx, input.ID); err != nil {
			return nil, maintenanceFailure("restore the backup", err)
		}

		return &maintenanceOutput[struct{}]{Status: http.StatusAccepted}, nil
	})

	register(api, huma.Operation{
		OperationID:   "delete-server-backup",
		Method:        http.MethodDelete,
		Path:          backupsPath + "/{id}",
		Summary:       "Delete a protected core backup",
		Tags:          []string{"system"},
		Description:   "Deletes one retained snapshot. A snapshot required by an active upgrade returns a conflict." + upgradesOffNote,
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict},
	}, func(ctx context.Context, input *backupRef) (*noContentOutput, error) {
		if err := requireUpgrades(ctx, cfg); err != nil {
			return nil, err
		}
		if err := cfg.Maintenance.DeleteBackup(ctx, input.ID); err != nil {
			return nil, maintenanceFailure("delete the backup", err)
		}

		return &noContentOutput{Status: http.StatusNoContent}, nil
	})
}

func registerRestart(api huma.API, cfg Config) {
	register(api, huma.Operation{
		OperationID: "restart-server",
		Method:      http.MethodPost,
		Path:        restartPath,
		Summary:     "Restart the server, normally or in safe mode",
		Tags:        []string{"system"},
		Description: "Restarts the server, for an organization administrator. Every printer stream, " +
			"upload and module stops, and the page waits for the server to come back. safe starts " +
			"the next run once with every module off, the ones declared in config.yaml too, and a " +
			"normal restart from there starts them again. A scheduled or running upgrade answers 409. " +
			"The marker that asks for safe mode names nobody.",
		DefaultStatus: http.StatusAccepted,
		Errors: []int{
			http.StatusUnauthorized, http.StatusForbidden, http.StatusConflict, http.StatusInternalServerError,
		},
	}, func(ctx context.Context, input *restartInput) (*restartOutput, error) {
		if err := requireMaintenance(ctx, cfg); err != nil {
			return nil, err
		}

		principal := principalFrom(ctx)
		if err := cfg.Maintenance.Restart(
			ctx,
			MaintenanceRestart{Mode: input.Body.Mode, Principal: principal},
		); err != nil {
			if errors.Is(err, upgrade.ErrUpgradeBusy) {
				return nil, maintenanceFailure("restart the server", err)
			}

			return nil, httpfault.Unexpected("restart the server").Wrap(err)
		}

		out := &restartOutput{Status: http.StatusAccepted}
		out.Body.Mode = input.Body.Mode

		return out, nil
	})
}

func requireMaintenance(ctx context.Context, cfg Config) error {
	if cfg.Maintenance == nil {
		return httpfault.ResourceNotFound(httpfault.Reference("server", "maintenance"))
	}

	return requireOrgAdmin(ctx, cfg, "server maintenance")
}

// upgradesOffNote ends the description of every operation requireUpgrades
// guards, and of the pairing operations, whose portal a server with upgrades
// off does not build. A client reads there why it may answer not found.
const upgradesOffNote = " It returns not found while core.upgrade.enabled is off."

// requireUpgrades is requireMaintenance for an operation that prepares an
// upgrade or touches a backup, which a server with core.upgrade.enabled off
// does not serve.
func requireUpgrades(ctx context.Context, cfg Config) error {
	if cfg.UpgradesOff {
		return httpfault.ResourceNotFound(httpfault.Reference("server", "upgrades"))
	}

	return requireMaintenance(ctx, cfg)
}

// upgradesServed is what get-ui-settings reports as upgrades.
func upgradesServed(cfg Config) bool {
	return cfg.Maintenance != nil && !cfg.UpgradesOff
}

func maintenanceFailure(action string, err error) error {
	if limited, ok := errors.AsType[*upgrade.LimitedError](err); ok {
		raised := httpfault.UpgradeServerLimited(limited.RetryAfter)
		wait, _ := raised.Params().Get(httpfault.UpgradeRetryAfterParam)

		return huma.ErrorWithHeaders(raised, http.Header{"Retry-After": []string{wait.String()}})
	}

	// A bundle cut short by the upload bound is too large, whatever check
	// noticed the missing bytes.
	if tooLarge, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return httpfault.BodyTooLarge(tooLarge.Limit)
	}

	switch {
	case errors.Is(err, upgrade.ErrNoJournal), errors.Is(err, os.ErrNotExist):
		return httpfault.ResourceNotFound(httpfault.Reference("server-maintenance", action))
	case errors.Is(err, upgrade.ErrJournal),
		errors.Is(err, upgrade.ErrUpgradeBusy),
		errors.Is(err, upgrade.ErrPreflight),
		errors.Is(err, upgrade.ErrPostgresUnconfirmed),
		errors.Is(err, upgrade.ErrBackupIntegrity):
		return httpfault.UpgradeConflict(err)
	case errors.Is(err, upgrade.ErrPlatformMismatch),
		errors.Is(err, upgrade.ErrBundle),
		errors.Is(err, upgrade.ErrBundleCompatibility),
		errors.Is(err, upgrade.ErrSignature),
		errors.Is(err, upgrade.ErrChecksum),
		errors.Is(err, upgrade.ErrVersionPolicy),
		errors.Is(err, upgrade.ErrBackupInvalid):
		return httpfault.UpgradeRefused(err)
	case errors.Is(err, betaportal.ErrAccessSuspended):
		return httpfault.BetaPortalAccessSuspended()
	case errors.Is(err, upgrade.ErrMachineRefused):
		return httpfault.BetaPortalPairingNeeded()
	case errors.Is(err, upgrade.ErrMachineRelease), errors.Is(err, upgrade.ErrDownloadSize):
		return httpfault.BetaPortalUnavailable()
	}

	return httpfault.Unexpected(action).Wrap(err)
}

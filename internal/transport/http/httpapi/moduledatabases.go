package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

// The databases modules left behind. A removal, and an update that moved a
// module's database, leave the old database with its data. These two
// operations list them and purge one. databases is a literal segment, and a
// namespace is at least two DNS labels, so no module claims it.
const (
	moduleDatabasesPath = modulesPathPrefix + "/databases"
	moduleDatabasePath  = moduleDatabasesPath + "/{kind}/{name}"
)

// ModuleDatabaseLeft is one database of a module's own that no declaration
// grants.
type ModuleDatabaseLeft struct {
	Instance    string    `json:"instance"              doc:"The instance that was granted the database, as kind:name. Installing the same module under this name again takes the database back."`
	Type        string    `json:"type"                  doc:"The module's type. Only a module of this type takes the database back."`
	Engine      string    `json:"engine"                doc:"sqlite for a file in the module's data directory, postgres for a database on a Postgres server."                                                                                                                            enum:"sqlite,postgres"`
	Where       string    `json:"where"                 doc:"Where the database is: the path of the SQLite file, or host:port/name for Postgres. The role and its password are never answered."`
	Provisioned bool      `json:"provisioned,omitempty" doc:"Whether this server made the database and its role on its own Postgres server."`
	LeftAt      time.Time `json:"left_at"               doc:"When the grant was given up."`
	Why         string    `json:"why"                   doc:"removed when the module was removed. moved when an update put the module's database somewhere else. not_granted when an update granted no database, because it was installed without approve or the release asks for none." enum:"removed,moved,not_granted"`
	Purge       string    `json:"purge"                 doc:"What a purge does to it. drop drops the database and the role this server made. delete deletes the SQLite file. forget forgets the record and leaves a database the operator named alone, because it is theirs."            enum:"drop,delete,forget"`
}

// ModuleDatabasePurged is what a purge did to one database.
type ModuleDatabasePurged struct {
	Instance string `json:"instance" doc:"The instance that was granted the database, as kind:name."`
	Engine   string `json:"engine"   doc:"sqlite or postgres."                                                        enum:"sqlite,postgres"`
	Where    string `json:"where"    doc:"Where the database was, as list-module-databases names it."`
	Done     string `json:"done"     doc:"dropped, deleted or forgotten. A forgotten database is still where it was." enum:"dropped,deleted,forgotten"`
}

type moduleDatabasesOutput struct {
	Body struct {
		Items []ModuleDatabaseLeft `json:"items" doc:"Every database a module left behind that this caller administers, oldest first. Empty when there is none."`
	}
}

type purgeModuleDatabaseInput struct {
	Kind    string `path:"kind" doc:"The resource kind of the instance that was granted the database."                                                                          maxLength:"64"  example:"sink"`
	Name    string `path:"name" doc:"The name of that instance."                                                                                                                maxLength:"253" example:"notes"`
	Confirm string `            doc:"The instance again, as kind:name. A purge cannot be undone, so a request that does not repeat the instance is refused and purges nothing." maxLength:"320" example:"sink:notes" query:"confirm" required:"true"`
	Forget  bool   `            doc:"Forget the record of each database and leave the database itself alone, such as one you dropped by hand."                                                  example:"false"      query:"forget"`
}

type purgeModuleDatabaseOutput struct {
	Body struct {
		Items []ModuleDatabasePurged `json:"items" doc:"What the purge did to each database left behind under the instance."`
	}
}

func registerModuleDatabases(api huma.API, cfg Config) {
	register(api, huma.Operation{
		OperationID: "list-module-databases",
		Method:      http.MethodGet,
		Path:        moduleDatabasesPath,
		Summary:     "The databases modules left behind",
		Description: "Lists each database of a module's own that no declaration grants any more: one a removed " +
			"module left, and one an update moved away from. The database is still where it was, with the " +
			"module's data. Installing the same module under the same name again takes it back, and " +
			"purge-module-database deletes it. It takes configure_resource on the resource kind of the " +
			"instance, and a database of a kind the caller does not administer is not listed.",
		Tags:   []string{"resources"},
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotImplemented},
	}, func(ctx context.Context, _ *struct{}) (*moduleDatabasesOutput, error) {
		if err := requireModuleAdmin(ctx, cfg); err != nil {
			return nil, err
		}

		left, err := cfg.ModuleInstall.LeftDatabases(ctx, principalFrom(ctx))
		if err != nil {
			return nil, err
		}

		out := &moduleDatabasesOutput{}
		out.Body.Items = left

		if out.Body.Items == nil {
			out.Body.Items = []ModuleDatabaseLeft{}
		}

		return out, nil
	})

	register(api, huma.Operation{
		OperationID: "purge-module-database",
		Method:      http.MethodDelete,
		Path:        moduleDatabasePath,
		Summary:     "Delete the databases a module left behind",
		Description: "Deletes every database left behind under one instance, as list-module-databases lists " +
			"them. A database this server made on its own Postgres server is dropped with its role, on the " +
			"server it was made on. A SQLite file is deleted from the module's data directory. A database the " +
			"operator named with a connection string is only forgotten: it belongs to them, and they drop it " +
			"themselves. With forget, every record is only forgotten.\n\n" +
			"A purge cannot be undone. `confirm` repeats the instance as kind:name, and a request without it " +
			"purges nothing. A database a declaration grants again is refused. A role or a database whose " +
			"comment or owner names anything else is refused with MODULE_DATABASE_NOT_PURGED and is not " +
			"dropped. An instance with nothing left behind is a 404. It takes configure_resource on the " +
			"instance's resource kind.",
		Tags: []string{"resources"},
		Errors: []int{
			http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity,
			http.StatusNotImplemented, http.StatusBadGateway,
		},
	}, func(ctx context.Context, in *purgeModuleDatabaseInput) (*purgeModuleDatabaseOutput, error) {
		if err := requireModuleAdmin(ctx, cfg); err != nil {
			return nil, err
		}

		instance, err := ref.New(ref.Kind(in.Kind), in.Name)
		if err != nil {
			return nil, httpfault.RouteNotFound(http.MethodDelete, moduleDatabasesPath+"/"+in.Kind+"/"+in.Name)
		}

		if strings.TrimSpace(in.Confirm) != instance.String() {
			return nil, httpfault.RequestInvalid("query.confirm", "the instance repeated",
				"confirm repeats the instance as "+instance.String()+", because a purge cannot be undone")
		}

		purged, err := cfg.ModuleInstall.PurgeDatabases(ctx, principalFrom(ctx), instance, in.Forget)
		if err != nil {
			return nil, err
		}

		out := &purgeModuleDatabaseOutput{}
		out.Body.Items = purged

		return out, nil
	})
}

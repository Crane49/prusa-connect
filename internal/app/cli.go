package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/prusa3d/prusa-connect/internal/app/builtin"
	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/hostschema"
	"github.com/prusa3d/prusa-connect/internal/kernel/admission"
	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
	"github.com/prusa3d/prusa-connect/internal/platform/auth"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/dbstore"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/local"
	"github.com/prusa3d/prusa-connect/internal/platform/devicestore"
)

// The two subcommands an operator runs against a server that is not running.
//
// The other way to create the first administrator is POST /api/v1/auth/setup,
// which needs the server up, a browser or a curl, and somebody who knows the
// setup path is open. The setup path is open to whoever reaches it first, so a
// server that starts before it is set up can be taken by somebody else.
//
// `registrations` exists for a similar reason. On a headless machine with no
// browser, an operator would have to tunnel in to admit one device that shows
// a code.
//
// Both read the operator's configuration, open the database it names and
// migrate it, exactly as a start does. A deployment can so be set up before
// the server has ever run: the account and the first grant are written, and
// the server starts with an administrator already in it.

// ErrUsage reports a command line this build cannot act on. The caller renders
// it as a usage exit and not as a failure, so a supervisor can tell a typo
// from an outage.
var ErrUsage = errors.New("usage")

// listVerb is the word that reads a table without changing it. It is spelled
// once because both commands take it and both have to spell it the same way.
const listVerb = "list"

// AdminCommand is the word that selects the account commands.
const AdminCommand = "admin"

// RegistrationsCommand is the word that selects the admission commands.
const RegistrationsCommand = "registrations"

// Admin runs the account commands: create, reset-password and list.
//
// out is where a listing and a confirmation go. Nothing here prints a password
// or a hash: the password is the operator's own input, and printing it back
// would put it in a terminal's scrollback and in whatever collects that.
func Admin(ctx context.Context, out io.Writer, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: %s create|reset-password|%s", ErrUsage, AdminCommand, listVerb)
	}

	flags := flag.NewFlagSet(AdminCommand+" "+args[0], flag.ContinueOnError)
	flags.SetOutput(out)

	var (
		configPath = flags.String("config", defaultConfigPath, "the operator's configuration file")
		name       = flags.String("name", "", "the username, which is what the person types at the login")
		password   = flags.String("password", "", "the password, at least the floor the store enforces")
	)

	if err := flags.Parse(args[1:]); err != nil {
		return fmt.Errorf("%w: %w", ErrUsage, err)
	}

	// The line is checked before anything is opened. A command that cannot run
	// must not migrate a database on its way to saying so: `admin create` with
	// no arguments is a typo, and answering it by creating the deployment's
	// database is a side effect nobody asked for.
	if err := usableAccountLine(args[0], *name, *password); err != nil {
		return err
	}

	// A reset is the command somebody runs beside a server that is up. It must
	// not migrate that server's database to another version's schema.
	accounts, closeHost, err := openAccountsFor(ctx, *configPath, args[0] == "reset-password")
	if err != nil {
		return err
	}

	defer closeHost()

	switch args[0] {
	case "create":
		return createAccount(ctx, out, accounts, *name, *password)
	case "reset-password":
		return resetAccountPassword(ctx, out, accounts, *name, *password)
	default:
		return listAccounts(ctx, out, accounts)
	}
}

// usableAccountLine refuses a command line this build cannot act on, before any
// file is opened.
func usableAccountLine(command, name, password string) error {
	switch command {
	case "create", "reset-password":
		if name == "" || password == "" {
			return fmt.Errorf("%w: %s %s --name <username> --password <password>",
				ErrUsage, AdminCommand, command)
		}

		return nil
	case listVerb:
		return nil
	default:
		return fmt.Errorf("%w: %s %s is not a command. It is create, reset-password or list",
			ErrUsage, AdminCommand, command)
	}
}

// defaultConfigPath is the file every entry point reads when nobody says
// otherwise. It is the same default the core's own -config flag carries, spelled
// here because a subcommand that read a different file from the server would
// write its account into a database the server never opens.
const defaultConfigPath = "config/config.yaml"

// adminAccounts is what the account commands work through: the account store
// and the credential store the HTTP surface uses, and the id the first
// administrator is created under.
type adminAccounts struct {
	store       *local.Accounts
	credentials subjectEnder
	pairings    principalUnpairer
	// older counts the rows an older build wrote with no creator, which a
	// password change cannot end.
	older   olderRows
	firstID string
}

// openAccountsFor reads the configuration, opens the database it names, brings
// the schema up and returns the account store over it.
//
// The graph is opened and bootstrapped as well, and that is needed. The first
// administrator is created under the principal the graph's first grant was
// written for, so a `create` that did not write the grant would produce a
// person who can log in and do nothing.
//
// sameSchema refuses a database that another version of this program has
// migrated, before anything is written to it.
func openAccountsFor(ctx context.Context, path string, sameSchema bool) (adminAccounts, func(), error) {
	core, err := config.Load(path)
	if err != nil {
		return adminAccounts{}, nil, err
	}

	if err = core.Validate(); err != nil {
		return adminAccounts{}, nil, err
	}

	if sameSchema {
		if err = requireSameSchema(ctx, core); err != nil {
			return adminAccounts{}, nil, err
		}
	}

	host, closeHost, err := openGraph(ctx, core, "", quiet())
	if err != nil {
		return adminAccounts{}, nil, err
	}

	rows, err := accountRows(host)
	if err != nil {
		closeHost()

		return adminAccounts{}, nil, err
	}

	if err = importAccountFile(ctx, core.Auth.Accounts, rows, quiet()); err != nil {
		closeHost()

		return adminAccounts{}, nil, err
	}

	credentials, err := dbstore.NewCredentials(host.db, host.engine)
	if err != nil {
		closeHost()

		return adminAccounts{}, nil, err
	}

	pairings, err := dbstore.NewPairings(host.db, host.engine)
	if err != nil {
		closeHost()

		return adminAccounts{}, nil, err
	}

	options, err := accountOptions(core)
	if err != nil {
		closeHost()

		return adminAccounts{}, nil, err
	}

	return adminAccounts{
		store:       local.New(rows, nil, options...),
		credentials: auth.NewVerifier(credentials, nil),
		pairings:    pairings,
		older:       olderRows{tokens: credentials, pairings: pairings},
		firstID:     core.Principal,
	}, closeHost, nil
}

// requireSameSchema refuses a database whose schema this build did not write.
// It opens the database, reads the migration ledger and closes it again, and
// writes nothing.
func requireSameSchema(ctx context.Context, core config.Core) error {
	db, dialect, where, err := openDatabase(ctx, core)
	if err != nil {
		return err
	}

	defer func() { _ = db.Close() }() // A read-only check has nothing to lose on close.

	migrator, err := hostschema.Migrator(db, engineOf(dialect), "")
	if err != nil {
		return fmt.Errorf("read the database at %s: %w", where, err)
	}

	err = migrator.RequireSchemaOfThisBuild(ctx)

	switch {
	case errors.Is(err, sqlstore.ErrSchemaOlder):
		return fmt.Errorf("%w. This database is from an earlier version: start the new server once so it "+
			"updates the database, then run the reset again", err)
	case err != nil:
		return fmt.Errorf("%w. Run the reset with the same version as the server that uses this database", err)
	}

	return nil
}

// createAccount creates a person. The first one is created under the principal
// the graph's first grant names, so that person is an administrator. The rest
// are ordinary accounts holding whatever an administrator grants them.
func createAccount(ctx context.Context, out io.Writer, accounts adminAccounts, name, password string) error {
	spec := local.NewAccount{Username: name, Password: []byte(password)}

	first, err := accounts.store.NeedsSetup(ctx)
	if err != nil {
		return fmt.Errorf("read the accounts: %w", err)
	}

	if !first {
		created, createErr := accounts.store.Create(ctx, spec)
		if createErr != nil {
			return fmt.Errorf("create the account %q: %w", name, createErr)
		}

		fmt.Fprintf(out, "created %s (%s). It holds no grant yet: an administrator writes one.\n",
			created.Username, created.ID)

		return nil
	}

	created, err := accounts.store.CreateFirst(ctx, accounts.firstID, spec)
	if err != nil {
		return fmt.Errorf("create the first administrator %q: %w", name, err)
	}

	fmt.Fprintf(out, "created the first administrator %s (%s). The setup path is now closed.\n",
		created.Username, created.ID)

	return nil
}

// resetAccountPassword replaces a password without asking for the old one, and
// ends every session and api credential the account holds. It is for somebody
// who has locked themselves out, or whose password leaked. It takes access to
// this machine, which is the only credential it can ask for.
//
// A running server reads each credential from the database on every request,
// so it refuses the ended ones at their next request with no restart.
func resetAccountPassword(
	ctx context.Context, out io.Writer, accounts adminAccounts, name, password string,
) error {
	found, err := accountNamed(ctx, accounts, name)
	if err != nil {
		return err
	}

	if err = accounts.store.SetPassword(ctx, found.ID, []byte(password)); err != nil {
		return fmt.Errorf("set the password of %q: %w", name, err)
	}

	ended, err := revokeAccountCredentials(ctx, accounts.credentials, accounts.pairings, found.ID)
	if err != nil {
		return fmt.Errorf("the password of %q was replaced, and its sessions and api credentials "+
			"could not be ended: %w", name, err)
	}

	fmt.Fprintf(out, "the password of %s (%s) was replaced. %s and %s it held were ended.\n",
		found.Username, found.ID, counted(ended.sessions, "session"), counted(ended.apiCredentials, "api credential"))
	fmt.Fprintf(out, "%s it created for somebody else, directly or down a chain it started, ended too.\n",
		counted(ended.mintedForOthers, "api credential"))
	fmt.Fprintf(out, "%s to it, written by it or down a chain it started, ended too.\n",
		counted(ended.pairings, "pairing"))
	fmt.Fprintln(out, "Review the accounts and grants on this server, because whoever knew the old "+
		"password could have changed them.")
	tokens, paired, err := accounts.older.count(ctx)
	if err != nil {
		return err
	}

	if tokens > 0 || paired > 0 {
		fmt.Fprintf(out, "%s and %s were written by version %s or earlier and name nobody as their creator. "+
			"Those made for a service account or another person were not ended. List them and revoke the "+
			"ones you do not recognise.\n",
			counted(tokens, "api credential"), counted(paired, "pairing"), creatorRecordedSince)
	}

	return nil
}

// creatorRecordedSince is the last version that did not record who created a
// token or a pairing. A row written by it or earlier names nobody, and a
// password change cannot end it.
const creatorRecordedSince = "0.1.0"

// olderRows counts the rows an older build wrote with no creator.
type olderRows struct {
	tokens interface {
		CountTokensWithoutMinter(ctx context.Context) (int, error)
	}
	pairings interface {
		CountWithoutCreator(ctx context.Context) (int, error)
	}
}

func (o olderRows) count(ctx context.Context) (tokens, pairings int, err error) {
	if o.tokens == nil || o.pairings == nil {
		return 0, 0, nil
	}

	tokens, err = o.tokens.CountTokensWithoutMinter(ctx)
	if err != nil {
		return 0, 0, err
	}

	pairings, err = o.pairings.CountWithoutCreator(ctx)
	if err != nil {
		return 0, 0, err
	}

	return tokens, pairings, nil
}

// counted spells a count with its noun, so a line reads "1 session" and
// "2 sessions".
func counted(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}

	return fmt.Sprintf("%d %ss", n, noun)
}

// listAccounts prints who can log in. No hash and no credential: this is who
// exists, and nothing more.
func listAccounts(ctx context.Context, out io.Writer, accounts adminAccounts) error {
	rows, err := accounts.store.List(ctx)
	if err != nil {
		return fmt.Errorf("read the accounts: %w", err)
	}

	if len(rows) == 0 {
		fmt.Fprintln(out, "this server holds no accounts, so the first-run setup path is open.")

		return nil
	}

	table := tabwriter.NewWriter(out, 0, 0, columnGap, ' ', 0)

	fmt.Fprintln(table, "ID\tUSERNAME\tDISPLAY NAME\tCREATED")

	for _, row := range rows {
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\n",
			row.ID, row.Username, row.DisplayName, row.CreatedAt.Format(instantLayout))
	}

	return table.Flush()
}

// columnGap is the blank space between two columns of a listing. Two is enough
// to read a column boundary without a rule and narrow enough that a listing
// fits an eighty-column terminal.
const columnGap = 2

// instantLayout is how a listing prints a time: to the second, in UTC, because
// a farm reads its logs in one zone and a second is finer than anything here is
// decided on.
const instantLayout = "2006-01-02 15:04:05Z"

// accountNamed looks a person up by the username they type, which is what an
// operator running this knows. The store folds usernames, so the lookup folds
// too.
func accountNamed(ctx context.Context, accounts adminAccounts, name string) (local.Account, error) {
	rows, err := accounts.store.List(ctx)
	if err != nil {
		return local.Account{}, fmt.Errorf("read the accounts: %w", err)
	}

	folded := local.FoldUsername(name)

	for _, row := range rows {
		if row.Username == folded {
			return row, nil
		}
	}

	return local.Account{}, fmt.Errorf("no account is held under the username %q", name)
}

// Registrations runs the admission commands: list, approve and reject.
func Registrations(ctx context.Context, out io.Writer, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: %s %s|approve <code>|reject <code>", ErrUsage, RegistrationsCommand, listVerb)
	}

	flags := flag.NewFlagSet(RegistrationsCommand+" "+args[0], flag.ContinueOnError)
	flags.SetOutput(out)

	var (
		configPath = flags.String("config", defaultConfigPath, "the operator's configuration file")
		by         = flags.String("by", "", "who is deciding, recorded against the registration")
	)

	if err := flags.Parse(args[1:]); err != nil {
		return fmt.Errorf("%w: %w", ErrUsage, err)
	}

	if err := usableRegistrationLine(args[0], flags.Args()); err != nil {
		return err
	}

	core, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	if err = core.Validate(); err != nil {
		return err
	}

	host, closeHost, err := openGraph(ctx, core, "", quiet())
	if err != nil {
		return err
	}

	defer closeHost()

	// No announcer. This process has no event bus and nobody is subscribed to
	// one, so a frame here would reach nothing.
	store, err := devicestore.NewRegistrations(host.db, host.engine, nil)
	if err != nil {
		return err
	}

	rows, err := allRegistrations(ctx, store, core)
	if err != nil {
		return err
	}

	if args[0] == listVerb {
		return listRegistrations(out, rows)
	}

	return decideRegistration(ctx, out, store, rows, args[0], flags.Args(), decidedBy(*by))
}

// usableRegistrationLine refuses a command line this build cannot act on,
// before any file is opened.
func usableRegistrationLine(command string, rest []string) error {
	switch command {
	case "approve", "reject":
		if len(rest) != 1 {
			return fmt.Errorf("%w: %s %s <code>", ErrUsage, RegistrationsCommand, command)
		}

		return nil
	case listVerb:
		return nil
	default:
		return fmt.Errorf("%w: %s %s is not a command. It is list, approve or reject",
			ErrUsage, RegistrationsCommand, command)
	}
}

// decidedBy is who a decision is recorded against when nobody said. The command
// is run by whoever holds a shell on this machine, and this server cannot know
// more than that.
func decidedBy(by string) string {
	if by == "" {
		return "cli"
	}

	return by
}

// allRegistrations reads every connector's table. The connectors are read from
// the configuration and are not built, because building one mounts a driver
// and this process serves nothing.
func allRegistrations(
	ctx context.Context, store admission.Store, core config.Core,
) ([]admission.Record, error) {
	names := connectorNames(core)

	var out []admission.Record

	for _, name := range names {
		rows, err := store.Load(ctx, name)
		if err != nil {
			return nil, err
		}

		out = append(out, rows...)
	}

	slices.SortFunc(out, func(a, b admission.Record) int { return strings.Compare(a.Code, b.Code) })

	return out, nil
}

// connectorNames is every connector instance this configuration declares, and
// the shipping dialect for a file that declares none, which is what
// mountConnectors mounts in that case.
func connectorNames(core config.Core) []string {
	declared := core.DeclaredConnectors()
	if len(declared) == 0 {
		return []string{string(builtin.ShippingConnector())}
	}

	names := make([]string, 0, len(declared))
	for _, connector := range declared {
		names = append(names, connector.Name)
	}

	return names
}

// listRegistrations prints what is waiting and what was let through. The token
// digest is not printed: a listing an operator reads is not a place to put
// anything shaped like a credential.
func listRegistrations(out io.Writer, rows []admission.Record) error {
	if len(rows) == 0 {
		fmt.Fprintln(out, "no device is registered or waiting.")

		return nil
	}

	table := tabwriter.NewWriter(out, 0, 0, columnGap, ' ', 0)

	fmt.Fprintln(table, "CODE\tCONNECTOR\tSERIAL\tMODEL\tSTATE\tADMITTED BY")

	for _, row := range rows {
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\n",
			row.Code, row.Connector, row.Serial, row.Model, registrationState(row), row.AdmittedBy)
	}

	return table.Flush()
}

// registrationState is the one word an operator reads off a listing.
func registrationState(row admission.Record) string {
	switch {
	case row.Approved:
		return "approved"
	case row.Expired(nowUTC()):
		return "expired"
	default:
		return "waiting"
	}
}

// decideRegistration approves or rejects one code.
func decideRegistration(
	ctx context.Context, out io.Writer, store admission.Store,
	rows []admission.Record, verb string, args []string, by string,
) error {
	code := strings.ToUpper(strings.TrimSpace(args[0]))

	found := -1

	for index, row := range rows {
		if row.Code == code {
			found = index

			break
		}
	}

	if found < 0 {
		return fmt.Errorf("no registration answers to the code %q", code)
	}

	row := rows[found]

	if verb == "reject" {
		if err := store.Forget(ctx, row.Connector, row.Code); err != nil {
			return err
		}

		fmt.Fprintf(out, "rejected %s on %s. %s\n", row.Code, row.Connector, runningServerNotice)

		return nil
	}

	if row.Expired(nowUTC()) {
		return fmt.Errorf("the code %q has expired, so the device is registering again with a new one", code)
	}

	row.Approved = true
	row.Endorsed = true
	row.AdmittedBy = by
	row.AdmittedAt = nowUTC()
	row.ExpiresAt = time.Time{}

	if err := store.Save(ctx, row); err != nil {
		return err
	}

	fmt.Fprintf(out, "approved %s on %s. %s\n", row.Code, row.Connector, runningServerNotice)

	return nil
}

// runningServerNotice tells the operator when a decision takes effect.
//
// A running core holds its registration table in memory and writes through to
// these rows. It does not read them again until it starts. So the server acts
// on a decision made here only at its next start, and an operator with a
// server running should use the API.
const runningServerNotice = "A core that is already running reads its table at startup, so restart it, " +
	"or use the API instead: POST /api/v1/registrations/<code>/approve."

// nowUTC is the clock these commands read. They are one-shot: there is nothing
// to inject a clock into and nothing that waits.
func nowUTC() time.Time { return time.Now().UTC() }

// quiet is a logger that writes nothing, for a command whose output is what it
// prints and not what the core logs on its way through a start-up step.
func quiet() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

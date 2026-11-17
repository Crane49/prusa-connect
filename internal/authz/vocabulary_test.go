package authz

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

func TestNewTupleAcceptsTheVocabulary(t *testing.T) {
	t.Parallel()

	accepted := []string{
		"group:maintenance#member@user:alice",
		"group:all-staff#member@group:maintenance#member",
		"org:acme#admin@user:root",
		"org:acme#operator@group:maintenance#member",
		"org:acme#viewer@user:olga",
		"device_group:cell-a#parent@device_group:floor-one",
		"device_group:cell-a#org@org:acme",
		"device_group:floor-one#manager@user:alice",
		"device_group:floor-one#operator@service_account:sa-ui",
		"device_group:floor-one#viewer@node:site-b",
		"device:p-alpha#org@org:acme",
		"device:p-alpha#group@device_group:cell-a",
		"device:p-alpha#agent@device_credential:sn-alpha",
		"device:p-alpha#blocked@user:olga",
		"device:p-alpha#manager@user:root",
		"device:p-alpha#operator@group:maintenance#member",
		"device:p-alpha#viewer@node:site-b",
	}

	for _, text := range accepted {
		t.Run(text, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, text, mustTuple(t, text).String())
		})
	}
}

func TestNewTupleRefusesWhatIsNotInTheVocabulary(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		object string
		rel    Relation
		who    string
		want   fault.Code
	}{
		{
			name:   "a service account cannot hold an org role",
			object: "org:acme", rel: RelAdmin, who: "service_account:sa-ui", want: CodeInvalidSubject(),
		},
		{
			name:   "a service account cannot join a team",
			object: "group:maintenance", rel: RelMember, who: "service_account:sa-ui", want: CodeInvalidSubject(),
		},
		{
			name:   "a node cannot hold an org role",
			object: "org:acme", rel: RelViewer, who: "node:site-b", want: CodeInvalidSubject(),
		},
		{
			name:   "a device is not grantable as a viewer",
			object: "device:p-alpha", rel: RelViewer, who: "device_credential:sn-alpha", want: CodeInvalidSubject(),
		},
		{
			name:   "a user is not a printer's agent",
			object: "device:p-alpha", rel: RelAgent, who: "user:alice", want: CodeInvalidSubject(),
		},
		{
			name:   "a printer is not a subject of a role",
			object: "device:p-alpha", rel: RelViewer, who: "device:p-beta", want: CodeInvalidSubject(),
		},
		{
			name:   "a group userset of the wrong relation",
			object: "device:p-alpha", rel: RelViewer, who: "group:maintenance#viewer", want: CodeUnknownRelation(),
		},
		{
			name:   "admin exists on org and nowhere else",
			object: "device:p-alpha", rel: RelAdmin, who: "user:root", want: CodeUnknownRelation(),
		},
		{
			name:   "a printer group has no members",
			object: "device_group:floor-one", rel: RelMember, who: "user:alice", want: CodeUnknownRelation(),
		},
		{
			name:   "a device cannot be blocked",
			object: "device:p-alpha", rel: RelBlocked, who: "device_credential:sn-alpha", want: CodeInvalidSubject(),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			object, err := ParseObject(tc.object)
			require.NoError(t, err)

			who, err := ParseSubject(tc.who)
			if err != nil {
				requireCode(t, err, tc.want)

				return
			}

			_, err = NewTuple(object, tc.rel, who)
			requireCode(t, err, tc.want)
		})
	}
}

func TestParseRefusesMalformedNames(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		text string
		want fault.Code
	}{
		{name: "a type outside the closed set", text: "robot:r2", want: CodeUnknownType()},
		{name: "no type at all", text: "alice", want: CodeInvalidID()},
		{name: "an uppercase id", text: "user:Alice", want: CodeInvalidID()},
		{name: "an underscore in an id", text: "user:al_ice", want: CodeInvalidID()},
		{name: "an empty id", text: "user:", want: CodeInvalidID()},
		{name: "a path in an id", text: "device:../etc", want: CodeInvalidID()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, objectErr := ParseObject(tc.text)
			requireCode(t, objectErr, tc.want)

			_, subjectErr := ParseSubject(tc.text)
			requireCode(t, subjectErr, tc.want)
		})
	}
}

func TestSubjectRoundTrip(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		text    string
		userset bool
	}{
		{name: "a principal", text: "user:alice"},
		{name: "a service account", text: "service_account:sa-ui"},
		{name: "a userset", text: "group:maintenance#member", userset: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			who, err := ParseSubject(tc.text)
			require.NoError(t, err)
			require.Equal(t, tc.text, who.String())
			require.Equal(t, tc.userset, who.IsUserset())
			require.False(t, who.IsZero())
		})
	}
}

func TestActionVocabulary(t *testing.T) {
	t.Parallel()

	t.Run("every action is valid and carries its own bit", func(t *testing.T) {
		t.Parallel()

		seen := actionSet(0)

		for _, action := range Actions() {
			require.Truef(t, action.Valid(), "%s", action)
			require.Zerof(t, seen&action.mask(), "%s shares a bit with another action", action)

			seen |= action.mask()
		}
	})

	t.Run("anything else is not", func(t *testing.T) {
		t.Parallel()

		for _, text := range []string{"", "viewer", "manager", "view telemetry", "VIEW_TELEMETRY"} {
			require.Falsef(t, Action(text).Valid(), "%q", text)
		}
	})
}

// TestRoleLayer holds the two properties every action inherits: the roles nest,
// and an agent credential is not a role.
func TestRoleLayer(t *testing.T) {
	t.Parallel()

	t.Run("each level contains the one below", func(t *testing.T) {
		t.Parallel()

		for name, mask := range map[string]func(level) actionSet{
			"device":   level.deviceMask,
			"resource": level.resourceMask,
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				require.Equal(t, mask(levelViewer), mask(levelViewer)&mask(levelOperator))
				require.Equal(t, mask(levelOperator), mask(levelOperator)&mask(levelManager))
				require.NotEqual(t, mask(levelViewer), mask(levelOperator))
				require.NotEqual(t, mask(levelOperator), mask(levelManager))
				require.Zero(t, mask(levelNone))
			})
		}
	})

	t.Run("the two domains share no bit", func(t *testing.T) {
		t.Parallel()

		require.Zero(t, levelManager.deviceMask()&levelManager.resourceMask(),
			"a printer role reaches a resource action")
		require.Zero(t, agentMask&levelManager.resourceMask(),
			"a printer credential reaches a resource action")
	})

	t.Run("an agent reaches no role action", func(t *testing.T) {
		t.Parallel()

		require.Zero(t, agentMask&maskOperate)
		require.Zero(t, agentMask&maskOperate)
		require.Zero(t, agentMask&maskOperate)
		require.Zero(t, agentMask&maskAdminister)
		require.NotZero(t, agentMask&maskObserve)
	})

	t.Run("no role reaches a machine action", func(t *testing.T) {
		t.Parallel()

		machine := maskReportTelemetry | maskFetchJob
		require.Zero(t, levelManager.deviceMask()&machine)
	})
}

func TestTypeVocabulary(t *testing.T) {
	t.Parallel()

	t.Run("the declared types are valid", func(t *testing.T) {
		t.Parallel()

		for _, typ := range Types() {
			require.Truef(t, typ.Valid(), "%s", typ)
		}
	})

	t.Run("anything else is not", func(t *testing.T) {
		t.Parallel()

		for _, text := range []string{"", "robot", "User", "team", "resource"} {
			require.Falsef(t, Type(text).Valid(), "%q", text)
		}
	})
}

func TestOpenRefusesAnUnusableStore(t *testing.T) {
	t.Parallel()

	t.Run("no database", func(t *testing.T) {
		t.Parallel()

		_, err := Open(nil, DialectSQLite)
		require.ErrorIs(t, err, ErrNoDatabase)
	})

	t.Run("no dialect", func(t *testing.T) {
		t.Parallel()

		_, err := Open(new(sql.DB), Dialect(0))
		require.ErrorIs(t, err, ErrUnsupportedDialect)
	})
}

func TestPostgresRenderingNumbersPlaceholders(t *testing.T) {
	t.Parallel()

	require.Equal(t,
		"SELECT 1 WHERE a = $1 AND b = $2",
		DialectPostgres.render("SELECT 1 WHERE a = ? AND b = ?"),
	)
	require.Equal(t,
		"SELECT 1 WHERE a = ? AND b = ?",
		DialectSQLite.render("SELECT 1 WHERE a = ? AND b = ?"),
	)
}

package upgrade

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSplitPostgresPasswordRemovesEverySpelling(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		dsn, clean, password string
	}{
		{"postgres://pcl:s3cret@db:5432/pcl?sslmode=require", "postgres://pcl@db:5432/pcl?sslmode=require", "s3cret"},
		{"postgresql://pcl@db/pcl?password=s3cret&sslmode=disable", "postgresql://pcl@db/pcl?sslmode=disable", "s3cret"},
		{"postgres://pcl@db/pcl", "postgres://pcl@db/pcl", ""},
		{"host=db user=pcl password=s3cret dbname=pcl", "host=db user=pcl dbname=pcl", "s3cret"},
		{`host=db password='it\'s secret' dbname=pcl`, "host=db dbname=pcl", "it's secret"},
	} {
		clean, password := splitPostgresPassword(tc.dsn)
		if clean != tc.clean || password != tc.password {
			t.Errorf("%q split into %q and %q, want %q and %q", tc.dsn, clean, password, tc.clean, tc.password)
		}
	}
}

// pg_dump and psql read the password from PGPASSWORD. The command line every
// local user can list never carries it.
func TestPostgresBackupKeepsThePasswordOffTheCommandLine(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("the stand-in programs are shell scripts")
	}

	root := t.TempDir()
	record := filepath.Join(root, "calls")
	script := "#!/bin/sh\nprintf '%s|%s\\n' \"$*\" \"$PGPASSWORD\" >> " + record + "\n" +
		"for arg in \"$@\"; do case \"$arg\" in --file=*) : > \"${arg#--file=}\";; esac; done\n"
	program := filepath.Join(root, "pg")
	if err := os.WriteFile(program, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	manager := BackupManager{Root: filepath.Join(root, "backups"), PGDumpPath: program, PSQLPath: program}
	if _, err := manager.Create(t.Context(), BackupPlan{
		ID: "dump",
		Postgres: []PostgresSource{{
			Name: "host", DSN: "postgres://pcl:s3cret@db/pcl", Mode: PostgresDump, Acknowledged: true,
		}},
	}); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 2 {
		t.Fatalf("the client programs ran %d times: %q", len(lines), content)
	}
	for _, line := range lines {
		args, password, _ := strings.Cut(line, "|")
		if strings.Contains(args, "s3cret") {
			t.Fatalf("the password is on the command line: %s", args)
		}
		if password != "s3cret" {
			t.Fatalf("PGPASSWORD is %q", password)
		}
	}
}

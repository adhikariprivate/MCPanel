package dbadmin

import (
	"errors"
	"strings"
	"testing"
)

type pgFake struct {
	out   string
	err   error
	calls []string
}

func (f *pgFake) Run(sql string) (string, error) { f.calls = append(f.calls, sql); return f.out, f.err }

func TestPostgresCreateUsesOwnedRoleAndDatabase(t *testing.T) {
	f := &pgFake{out: "0"}
	a := Postgres{Runner: f}
	if err := a.Create("blog_shop", "SafePassword2345"); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 2 || !strings.Contains(f.calls[0], "pg_database") || !strings.Contains(f.calls[1], `CREATE ROLE "blog_shop" LOGIN PASSWORD 'SafePassword2345'`) || !strings.Contains(f.calls[1], `CREATE DATABASE "blog_shop" OWNER "blog_shop"`) || !strings.Contains(f.calls[1], `REVOKE ALL ON DATABASE "blog_shop" FROM PUBLIC`) {
		t.Fatalf("unexpected PostgreSQL SQL: %#v", f.calls)
	}
}

func TestPostgresNamesAndPasswordsCannotInjectSQL(t *testing.T) {
	f := &pgFake{out: "0"}
	a := Postgres{Runner: f}
	if err := a.Create(`x"; DROP ROLE postgres;--`, "SafePassword2345"); err == nil {
		t.Fatal("unsafe name was accepted")
	}
	if err := a.Create("blog", `pw'; DROP ROLE postgres;--`); err == nil {
		t.Fatal("unsafe password was accepted")
	}
	if len(f.calls) != 0 {
		t.Fatalf("invalid inputs reached psql: %#v", f.calls)
	}
}

func TestPostgresCreateCleansRoleAfterDatabaseFailure(t *testing.T) {
	f := &pgCreateFailure{calls: []string{}}
	a := Postgres{Runner: f}
	if err := a.Create("blog", "SafePassword2345"); err == nil || strings.Contains(err.Error(), "SafePassword2345") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.calls) != 3 || !strings.Contains(f.calls[2], "DROP ROLE IF EXISTS \"blog\"") {
		t.Fatalf("failed creation was not cleaned up: %#v", f.calls)
	}
}

type pgCreateFailure struct{ calls []string }

func (f *pgCreateFailure) Run(sql string) (string, error) {
	f.calls = append(f.calls, sql)
	if len(f.calls) == 1 {
		return "0", nil
	}
	if len(f.calls) == 2 {
		return "", errors.New("failed SafePassword2345")
	}
	return "", nil
}

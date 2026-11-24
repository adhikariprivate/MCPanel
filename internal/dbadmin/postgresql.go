package dbadmin

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// PSQLRunner runs the local PostgreSQL administrative client using peer auth as the postgres OS account.
type PSQLRunner struct{}

func (PSQLRunner) Run(sql string) (string, error) {
	cmd := exec.Command("runuser", "-u", "postgres", "--", "psql", "-X", "-A", "-t", "-v", "ON_ERROR_STOP=1", "-d", "postgres")
	cmd.Stdin = strings.NewReader(sql)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("psql: %s", strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

type Postgres struct{ Runner Runner }

func pgIdent(s string) string   { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
func pgLiteral(s string) string { return `'` + strings.ReplaceAll(s, `'`, `''`) + `'` }

func (p Postgres) InUse(name string) (bool, error) {
	if !ValidName(name) {
		return false, fmt.Errorf("invalid name %q", name)
	}
	out, err := p.Runner.Run(fmt.Sprintf("SELECT (SELECT count(*) FROM pg_database WHERE datname=%s) + (SELECT count(*) FROM pg_roles WHERE rolname=%s);\n", pgLiteral(name), pgLiteral(name)))
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "0", nil
}

func (p Postgres) Create(name, password string) error {
	if !ValidName(name) {
		return fmt.Errorf("invalid name %q", name)
	}
	if !ValidPassword(password) {
		return errors.New("invalid password: 12 to 64 printable characters without space, quotes, backslash or backtick")
	}
	used, err := p.InUse(name)
	if err != nil {
		return err
	}
	if used {
		return fmt.Errorf("a database or role named %q already exists on the server", name)
	}
	_, err = p.Runner.Run(fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD %s;\nCREATE DATABASE %s OWNER %s;\nREVOKE ALL ON DATABASE %s FROM PUBLIC;\n", pgIdent(name), pgLiteral(password), pgIdent(name), pgIdent(name), pgIdent(name)))
	if err != nil {
		_, _ = p.Runner.Run(fmt.Sprintf("DROP DATABASE IF EXISTS %s;\nDROP ROLE IF EXISTS %s;\n", pgIdent(name), pgIdent(name)))
	}
	return redact(err, password)
}

func (p Postgres) SetPassword(name, password string) error {
	if !ValidName(name) {
		return fmt.Errorf("invalid name %q", name)
	}
	if !ValidPassword(password) {
		return errors.New("invalid password: 12 to 64 printable characters without space, quotes, backslash or backtick")
	}
	used, err := p.InUse(name)
	if err != nil {
		return err
	}
	if !used {
		return fmt.Errorf("no database or role named %q on the server", name)
	}
	_, err = p.Runner.Run(fmt.Sprintf("ALTER ROLE %s PASSWORD %s;\n", pgIdent(name), pgLiteral(password)))
	return redact(err, password)
}

func (p Postgres) Drop(name string) error {
	if !ValidName(name) {
		return fmt.Errorf("invalid name %q", name)
	}
	_, err := p.Runner.Run(fmt.Sprintf("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=%s AND pid <> pg_backend_pid();\nDROP DATABASE IF EXISTS %s;\nDROP ROLE IF EXISTS %s;\n", pgLiteral(name), pgIdent(name), pgIdent(name)))
	return err
}

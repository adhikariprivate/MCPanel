// Package store keeps hosted sites in a SQLite database.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver, so the binary stays static

	"mpn/internal/site"
)

var (
	ErrExists   = errors.New("already exists")
	ErrNotFound = errors.New("not found")
	ErrInUse    = errors.New("still in use")
)

// migrations[i] upgrades a database from version i to i+1. Never edit an entry a release has already used.
var migrations = []string{
	schemaV1,
	`ALTER TABLE sites ADD COLUMN tls INTEGER NOT NULL DEFAULT 0`,
	`CREATE TABLE databases (
    name       TEXT PRIMARY KEY,
    site_id    INTEGER NOT NULL REFERENCES sites(id) ON DELETE RESTRICT,
    created_at INTEGER NOT NULL
);
CREATE INDEX databases_site ON databases(site_id);`,
	// Settings a person may change later: the PHP settings (JSON) and the memory of a node app.
	`ALTER TABLE sites ADD COLUMN php_ini TEXT;
ALTER TABLE sites ADD COLUMN mem_mb INTEGER NOT NULL DEFAULT 0;`,
	// Python sites. SQLite cannot change a CHECK constraint, so the table is rebuilt. The marker on the first line makes
	// runMigration switch foreign keys off for it: dropping the old table with them on would delete every hostname row
	// through ON DELETE CASCADE.
	noForeignKeys + `
CREATE TABLE sites_new (
    id           INTEGER PRIMARY KEY,
    name         TEXT NOT NULL UNIQUE,
    kind         TEXT NOT NULL CHECK (kind IN ('static','php','node','proxy','python')),
    system_user  TEXT NOT NULL UNIQUE,
    root_dir     TEXT NOT NULL,
    php_version  TEXT,
    app_port     INTEGER UNIQUE,
    node_version TEXT,
    entry        TEXT,
    upstream     TEXT,
    created_at   INTEGER NOT NULL,
    tls          INTEGER NOT NULL DEFAULT 0,
    php_ini      TEXT,
    mem_mb       INTEGER NOT NULL DEFAULT 0,
    py_version   TEXT
);
INSERT INTO sites_new (id, name, kind, system_user, root_dir, php_version, app_port, node_version, entry, upstream, created_at, tls, php_ini, mem_mb)
    SELECT id, name, kind, system_user, root_dir, php_version, app_port, node_version, entry, upstream, created_at, tls, php_ini, mem_mb FROM sites;
DROP TABLE sites;
ALTER TABLE sites_new RENAME TO sites;`,
	// The domain choices of a site: the www redirect and whether plain HTTP is still served once HTTPS is on.
	`ALTER TABLE sites ADD COLUMN www TEXT NOT NULL DEFAULT '';
ALTER TABLE sites ADD COLUMN allow_http INTEGER NOT NULL DEFAULT 0;`,
	// Where the certificate comes from. Every site that has SSL until now got it from Let's Encrypt.
	`ALTER TABLE sites ADD COLUMN ssl_mode TEXT NOT NULL DEFAULT '';
UPDATE sites SET ssl_mode = 'letsencrypt' WHERE tls = 1;`,
	// The nginx configuration a person wrote by hand; empty means the one mpn makes.
	`ALTER TABLE sites ADD COLUMN vhost TEXT NOT NULL DEFAULT '';`,
	// Scheduled jobs of a site. They go away with the site.
	`CREATE TABLE cron_jobs (
    id         INTEGER PRIMARY KEY,
    site_id    INTEGER NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    schedule   TEXT NOT NULL,
    command    TEXT NOT NULL,
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL
);
CREATE INDEX cron_jobs_site ON cron_jobs(site_id);`,
	// The firewall: the rules a person added, and a place for settings of the whole server.
	`CREATE TABLE firewall_rules (
    id         INTEGER PRIMARY KEY,
    action     TEXT NOT NULL CHECK (action IN ('allow','deny')),
    proto      TEXT NOT NULL CHECK (proto IN ('tcp','udp','both')),
    port       TEXT NOT NULL DEFAULT '',
    source     TEXT NOT NULL DEFAULT '',
    comment    TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL
);
CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);`,
	// Compression and browser caching of static files, switched on per site.
	`ALTER TABLE sites ADD COLUMN speed INTEGER NOT NULL DEFAULT 0;`,
	// The application mpn installed in a PHP site (WordPress, Laravel...), empty for a site with its own code.
	`ALTER TABLE sites ADD COLUMN app TEXT NOT NULL DEFAULT '';`,
	// The mail server: the domains it receives for (with the private DKIM key of each), the mailboxes (with the password hash) and the
	// aliases. Everything goes away with its domain. The database is readable by root only, which is why the keys can live here.
	`CREATE TABLE mail_domains (
    name       TEXT PRIMARY KEY,
    dkim_key   TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL
);
CREATE TABLE mailboxes (
    address    TEXT PRIMARY KEY,
    domain     TEXT NOT NULL REFERENCES mail_domains(name) ON DELETE CASCADE,
    hash       TEXT NOT NULL,
    quota_mb   INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL
);
CREATE INDEX mailboxes_domain ON mailboxes(domain);
CREATE TABLE mail_aliases (
    address    TEXT PRIMARY KEY,
    domain     TEXT NOT NULL REFERENCES mail_domains(name) ON DELETE CASCADE,
    targets    TEXT NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX mail_aliases_domain ON mail_aliases(domain);`,
	// The web mail under /webmail/ of a site's own name, switched on per site.
	`ALTER TABLE sites ADD COLUMN webmail INTEGER NOT NULL DEFAULT 0;`,
	// The activity log of the panel: what was done, from which address, to which website (see internal/audit). Only root writes it.
	`CREATE TABLE audit_log (
    id     INTEGER PRIMARY KEY,
    at     INTEGER NOT NULL,
    ip     TEXT NOT NULL DEFAULT '',
    site   TEXT NOT NULL DEFAULT '',
    op     TEXT NOT NULL,
    kind   TEXT NOT NULL,
    detail TEXT NOT NULL DEFAULT '',
    ok     INTEGER NOT NULL,
    error  TEXT NOT NULL DEFAULT '',
    danger INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX audit_log_at ON audit_log(at);
CREATE INDEX audit_log_site ON audit_log(site, at);`,
	// Telegram alerts (internal/notify), on the hub: the machines that report to it (their keys as hashes only), the pairing codes not
	// used yet (as hashes), the messages waiting to go out, and the chats that sent /start to the bot.
	`CREATE TABLE notify_nodes (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    key_hash   TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    last_seen  INTEGER NOT NULL DEFAULT 0,
    report     TEXT NOT NULL DEFAULT '',
    rules      TEXT NOT NULL DEFAULT '',
    lost       INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE notify_pairs (
    hash    TEXT PRIMARY KEY,
    name    TEXT NOT NULL,
    expires INTEGER NOT NULL
);
CREATE TABLE notify_outbox (
    id         INTEGER PRIMARY KEY,
    text       TEXT NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE TABLE notify_chats (
    id    INTEGER PRIMARY KEY,
    title TEXT NOT NULL,
    at    INTEGER NOT NULL
);`,
	// An application that finishes its setup in the browser (phpBB) has done so.
	`ALTER TABLE sites ADD COLUMN app_ready INTEGER NOT NULL DEFAULT 0;`,
	// Binary and mise apps use a TCP port; mise also persists the task selected for supervision.
	noForeignKeys + `
CREATE TABLE sites_new (
    id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE,
    kind TEXT NOT NULL CHECK (kind IN ('static','php','node','proxy','python','binary','mise')),
    system_user TEXT NOT NULL UNIQUE, root_dir TEXT NOT NULL, php_version TEXT,
    app_port INTEGER UNIQUE, node_version TEXT, py_version TEXT, entry TEXT, upstream TEXT,
    created_at INTEGER NOT NULL, tls INTEGER NOT NULL DEFAULT 0, php_ini TEXT, mem_mb INTEGER NOT NULL DEFAULT 0,
    www TEXT NOT NULL DEFAULT '', allow_http INTEGER NOT NULL DEFAULT 0, ssl_mode TEXT NOT NULL DEFAULT '',
    vhost TEXT NOT NULL DEFAULT '', speed INTEGER NOT NULL DEFAULT 0, webmail INTEGER NOT NULL DEFAULT 0,
    app TEXT NOT NULL DEFAULT '', app_ready INTEGER NOT NULL DEFAULT 0, mise_task TEXT NOT NULL DEFAULT ''
);
INSERT INTO sites_new (id,name,kind,system_user,root_dir,php_version,app_port,node_version,py_version,entry,upstream,created_at,tls,php_ini,mem_mb,www,allow_http,ssl_mode,vhost,speed,webmail,app,app_ready)
SELECT id,name,kind,system_user,root_dir,php_version,app_port,node_version,py_version,entry,upstream,created_at,tls,php_ini,mem_mb,www,allow_http,ssl_mode,vhost,speed,webmail,app,app_ready FROM sites;
DROP TABLE sites;
ALTER TABLE sites_new RENAME TO sites;`,
	`ALTER TABLE databases ADD COLUMN engine TEXT NOT NULL DEFAULT 'mysql';`,
	// Delegated panel grants use a random stable identity so deleting and recreating a site with the same name cannot restore old access.
	`ALTER TABLE sites ADD COLUMN access_id TEXT NOT NULL DEFAULT '';
UPDATE sites SET access_id = lower(hex(randomblob(16))) WHERE access_id = '';
CREATE UNIQUE INDEX sites_access_id ON sites(access_id);`,
	// panel migrate cleans Node.js/Python generated OS config before this breaking, data-preserving schema change.
	"-- mpn:remove-app-runtimes",
	`CREATE TABLE background_services (
	    id INTEGER PRIMARY KEY AUTOINCREMENT,
    site_id INTEGER NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('php','binary','mise')),
    command TEXT NOT NULL DEFAULT '',
    task TEXT NOT NULL DEFAULT '',
    memory_mb INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(site_id, name)
);
CREATE INDEX background_services_site ON background_services(site_id, id);`,
}

// noForeignKeys, at the start of a migration, runs it with foreign key enforcement off and checks them again afterwards.
const noForeignKeys = "-- mpn:no-foreign-keys"

var schemaVersion = len(migrations)

// CurrentSchemaVersion is the schema version written by this build.
func CurrentSchemaVersion() int { return schemaVersion }

// SchemaVersionAt reads a database's schema version without applying migrations.
func SchemaVersionAt(path string) (int, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return 0, nil
	} else if err != nil {
		return 0, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return 0, err
	}
	return version, nil
}

const schemaV1 = `
CREATE TABLE sites (
    id          INTEGER PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    kind        TEXT NOT NULL CHECK (kind IN ('static','php','node','proxy')),
    system_user TEXT NOT NULL UNIQUE,
    root_dir    TEXT NOT NULL,
    php_version TEXT,
    app_port    INTEGER UNIQUE,
    node_version TEXT,
    entry       TEXT,
    upstream    TEXT,
    created_at  INTEGER NOT NULL
);
CREATE TABLE hostnames (
    hostname TEXT PRIMARY KEY,
    site_id  INTEGER NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    position INTEGER NOT NULL
);
CREATE INDEX hostnames_site ON hostnames(site_id);
`

type Store struct{ db *sql.DB }

// Open opens (and if needed creates) the database at path and brings its schema up to date.
func Open(path string) (*Store, error) {
	return openDB(path, false)
}

// OpenAfterRuntimeCleanup is for the explicit panel migration after it has removed MCPN-generated files for legacy runtime sites.
// Ordinary callers must use Open, which refuses to apply this breaking migration when such sites remain.
func OpenAfterRuntimeCleanup(path string) (*Store, error) { return openDB(path, true) }

func openDB(path string, runtimeCleanupDone bool) (*Store, error) {
	// The database is for root alone. Creating it with mode 0600 first leaves no moment when another account could read it.
	if f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600); err == nil {
		f.Close()
	}
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // one writer, and it keeps the foreign_keys pragma on every statement
	s := &Store{db}
	if err := s.migrate(runtimeCleanupDone); err != nil {
		db.Close()
		return nil, err
	}
	restrict(path)
	return s, nil
}

// restrict makes the database and its journal files private. SQLite gives the journal files the mode of the main file when
// it creates them, but files left by an earlier version keep the mode they had.
func restrict(path string) {
	for _, suffix := range []string{"", "-wal", "-shm"} {
		_ = os.Chmod(path+suffix, 0o600)
	}
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(runtimeCleanupDone bool) error {
	var v int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return err
	}
	switch {
	case v == schemaVersion:
		return nil
	case v > schemaVersion:
		return fmt.Errorf("database schema %d is newer than this build supports (%d)", v, schemaVersion)
	}
	// Ordinary commands must never silently remove runtime sites. The explicit panel migration cleans their OS artifacts first.
	if !runtimeCleanupDone && v > 0 && v < len(migrations) {
		legacy, err := legacyRuntimeSiteCount(s.db)
		if err != nil {
			return err
		}
		if legacy > 0 {
			return fmt.Errorf("%d Node.js/Python site(s) need OS cleanup before this database can be upgraded; run mpn panel migrate -apply", legacy)
		}
	}
	for i := v; i < len(migrations); i++ {
		if migrations[i] == "-- mpn:remove-app-runtimes" {
			if err := s.removeAppRuntimes(i + 1); err != nil {
				return fmt.Errorf("migrating database to version %d: %w", i+1, err)
			}
			continue
		}
		if err := s.runMigration(i); err != nil {
			return fmt.Errorf("migrating database to version %d: %w", i+1, err)
		}
	}
	return nil
}

func legacyRuntimeSiteCount(db *sql.DB) (int, error) {
	var hasSites int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='sites'`).Scan(&hasSites); err != nil {
		return 0, err
	}
	if hasSites == 0 {
		return 0, nil
	}
	var n int
	err := db.QueryRow(`SELECT count(*) FROM sites WHERE kind IN ('node','python')`).Scan(&n)
	return n, err
}

// LegacyRuntimeSite describes a Node.js/Python site that must have its generated OS config cleaned before the breaking migration.
type LegacyRuntimeSite struct {
	Name, User, Kind, AccessID string
	CustomVhost                bool
}

// LegacyRuntimeSites reads the stable columns present since schema v1 without invoking the normal auto-migrator.
func LegacyRuntimeSites(path string) ([]LegacyRuntimeSite, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var hasSites int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='sites'`).Scan(&hasSites); err != nil {
		return nil, err
	}
	if hasSites == 0 {
		return nil, nil
	}
	cols, err := db.Query(`PRAGMA table_info(sites)`)
	if err != nil {
		return nil, err
	}
	hasVhost, hasAccessID := false, false
	for cols.Next() {
		var cid, notnull, pk int
		var name, typ string
		var def any
		if err := cols.Scan(&cid, &name, &typ, &notnull, &def, &pk); err != nil {
			cols.Close()
			return nil, err
		}
		if name == "vhost" {
			hasVhost = true
		}
		if name == "access_id" {
			hasAccessID = true
		}
	}
	if err := cols.Err(); err != nil {
		cols.Close()
		return nil, err
	}
	cols.Close()
	colsToRead := []string{"name", "system_user", "kind"}
	if hasVhost {
		colsToRead = append(colsToRead, "vhost")
	}
	if hasAccessID {
		colsToRead = append(colsToRead, "access_id")
	}
	query := `SELECT ` + strings.Join(colsToRead, ",") + ` FROM sites WHERE kind IN ('node','python') ORDER BY name`
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LegacyRuntimeSite
	for rows.Next() {
		var x LegacyRuntimeSite
		scan := []any{&x.Name, &x.User, &x.Kind}
		var vhost sql.NullString
		if hasVhost {
			scan = append(scan, &vhost)
		}
		var accessID sql.NullString
		if hasAccessID {
			scan = append(scan, &accessID)
		}
		if err := rows.Scan(scan...); err != nil {
			return nil, err
		}
		x.CustomVhost, x.AccessID = vhost.String != "", accessID.String
		out = append(out, x)
	}
	return out, rows.Err()
}

// removeAppRuntimes keeps database names and engines while detaching them from removed site rows. It runs atomically
// after panel migrate has removed generated OS config; site home directories and database contents are never touched.
func (s *Store) removeAppRuntimes(version int) error {
	if _, err := s.db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	defer s.db.Exec(`PRAGMA foreign_keys=ON`)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`CREATE TABLE databases_new (
 name TEXT PRIMARY KEY, site_id INTEGER REFERENCES sites(id),
 created_at INTEGER NOT NULL, engine TEXT NOT NULL DEFAULT 'mysql'
);
INSERT INTO databases_new (name,site_id,created_at,engine) SELECT name,site_id,created_at,engine FROM databases;
DROP TABLE databases;
ALTER TABLE databases_new RENAME TO databases;
CREATE INDEX databases_site ON databases(site_id);`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM hostnames WHERE site_id IN (SELECT id FROM sites WHERE kind IN ('node','python'));
DELETE FROM cron_jobs WHERE site_id IN (SELECT id FROM sites WHERE kind IN ('node','python'));
UPDATE databases SET site_id=NULL WHERE site_id IN (SELECT id FROM sites WHERE kind IN ('node','python'));
DELETE FROM sites WHERE kind IN ('node','python');`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE TABLE sites_new (
 id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE,
 kind TEXT NOT NULL CHECK (kind IN ('static','php','proxy','binary','mise')),
 system_user TEXT NOT NULL UNIQUE, root_dir TEXT NOT NULL, php_version TEXT,
 app_port INTEGER UNIQUE, entry TEXT, upstream TEXT,
 created_at INTEGER NOT NULL, tls INTEGER NOT NULL DEFAULT 0, php_ini TEXT, mem_mb INTEGER NOT NULL DEFAULT 0,
 www TEXT NOT NULL DEFAULT '', allow_http INTEGER NOT NULL DEFAULT 0, ssl_mode TEXT NOT NULL DEFAULT '',
 vhost TEXT NOT NULL DEFAULT '', speed INTEGER NOT NULL DEFAULT 0, webmail INTEGER NOT NULL DEFAULT 0,
 app TEXT NOT NULL DEFAULT '', app_ready INTEGER NOT NULL DEFAULT 0, mise_task TEXT NOT NULL DEFAULT '',
 access_id TEXT NOT NULL DEFAULT ''
);
INSERT INTO sites_new (id,name,kind,system_user,root_dir,php_version,app_port,entry,upstream,created_at,tls,php_ini,mem_mb,www,allow_http,ssl_mode,vhost,speed,webmail,app,app_ready,mise_task,access_id)
SELECT id,name,kind,system_user,root_dir,php_version,app_port,entry,upstream,created_at,tls,php_ini,mem_mb,www,allow_http,ssl_mode,vhost,speed,webmail,app,app_ready,mise_task,access_id FROM sites;
DROP TABLE sites;
ALTER TABLE sites_new RENAME TO sites;
CREATE UNIQUE INDEX sites_access_id ON sites(access_id);`); err != nil {
		return err
	}
	var table string
	if err := tx.QueryRow(`PRAGMA foreign_key_check`).Scan(&table); err == nil {
		return fmt.Errorf("runtime removal left a broken foreign key in %q", table)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) runMigration(i int) error {
	if strings.HasPrefix(migrations[i], noForeignKeys) {
		// The pragma has no effect inside a transaction. There is one connection, so it holds for the whole migration.
		if _, err := s.db.Exec("PRAGMA foreign_keys = OFF"); err != nil {
			return err
		}
		defer s.db.Exec("PRAGMA foreign_keys = ON")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(migrations[i]); err != nil {
		return err
	}
	if strings.HasPrefix(migrations[i], noForeignKeys) {
		var table string
		if err := tx.QueryRow("PRAGMA foreign_key_check").Scan(&table); err == nil {
			return fmt.Errorf("the rebuilt tables break a foreign key in %q", table)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
		return err
	}
	return tx.Commit()
}

func nullStr(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// existsError says what was already taken, in words a user can act on, and matches ErrExists with errors.Is.
type existsError struct{ what string }

func (e existsError) Error() string        { return e.what + " is already taken" }
func (e existsError) Is(target error) bool { return target == ErrExists }

// uniqueLabels names the columns whose UNIQUE constraint a user can run into.
var uniqueLabels = map[string]string{
	"sites.name": "site name", "sites.system_user": "system user", "sites.app_port": "application port",
	"hostnames.hostname": "hostname", "databases.name": "database name",
}

// wrap turns SQLite's "UNIQUE constraint failed: hostnames.hostname (1555)" into "hostname is already taken".
func wrap(err error) error {
	msg := err.Error()
	const marker = "UNIQUE constraint failed: "
	i := strings.Index(msg, marker)
	if i < 0 {
		return err
	}
	col := strings.TrimSpace(strings.SplitN(msg[i+len(marker):], " (", 2)[0])
	if label, ok := uniqueLabels[col]; ok {
		return existsError{label}
	}
	return existsError{col}
}

// AddSite validates and stores a site with its hostnames, all or nothing.
// Fields that do not belong to the site's kind are not stored.
func (s *Store) AddSite(st site.Site) error {
	if err := st.Validate(); err != nil {
		return err
	}
	// The default system user equals the site name, so a repeated name would otherwise be reported as a taken
	// "system user". The UNIQUE columns still guard against a race between two callers.
	var one int
	if s.db.QueryRow(`SELECT 1 FROM sites WHERE name = ?`, st.Name).Scan(&one) == nil {
		return existsError{"site name"}
	}
	var php, upstream, phpIni, port, entry any
	mem := 0
	switch st.Kind {
	case site.PHP:
		php = nullStr(st.PHPVersion)
		phpIni = iniJSON(st.PHPIni)
	case site.Binary:
		port, entry, mem = appPort(st), nullStr(st.Entry), st.MemMB
	case site.Mise:
		port, mem = st.AppPort, st.MemMB
	case site.Proxy:
		upstream = nullStr(st.Upstream)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	mode := sslModeOf(st)
	res, err := tx.Exec(`INSERT INTO sites (name, kind, system_user, root_dir, php_version, app_port, entry, upstream, tls, ssl_mode, php_ini, mem_mb, www, allow_http, app, created_at, mise_task, access_id)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,lower(hex(randomblob(16))))`, st.Name, string(st.Kind), st.User, st.Root, php, port, entry, upstream, mode != "", mode, phpIni, mem, st.WWW, st.AllowHTTP, st.App, time.Now().Unix(), st.MiseTask)
	if err != nil {
		return wrap(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	for i, h := range st.Hostnames {
		if _, err := tx.Exec(`INSERT INTO hostnames (hostname, site_id, position) VALUES (?,?,?)`, h, id, i); err != nil {
			return wrap(err)
		}
	}
	return tx.Commit()
}

func (s *Store) GetSite(name string) (site.Site, error) {
	list, err := s.load("WHERE name = ?", name)
	if err != nil {
		return site.Site{}, err
	}
	if len(list) == 0 {
		return site.Site{}, fmt.Errorf("site %q: %w", name, ErrNotFound)
	}
	return list[0], nil
}

func (s *Store) ListSites() ([]site.Site, error) { return s.load("") }

// sslModeOf is the certificate source of a site. A site that has SSL but no recorded source got it from Let's Encrypt, as
// every site did before there was a choice.
func sslModeOf(st site.Site) string {
	if st.SSLMode == site.SSLNone && st.TLS {
		return site.SSLLetsEncrypt
	}
	if !st.TLS {
		return site.SSLNone
	}
	return st.SSLMode
}

// SetSSL records where the certificate of a site comes from, or that it has none. The site is served over SSL exactly
// when it has a source.
func (s *Store) SetSSL(name, mode string) error {
	if !site.ValidSSLMode(mode) {
		return fmt.Errorf("unknown SSL source %q", mode)
	}
	res, err := s.db.Exec(`UPDATE sites SET tls = ?, ssl_mode = ? WHERE name = ?`, mode != "", mode, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("site %q: %w", name, ErrNotFound)
	}
	return nil
}

// SetApp records the application installed in a PHP site, or "" for none.
func (s *Store) SetApp(name, app string) error {
	if app != "" && !site.ValidApp(app) {
		return fmt.Errorf("unknown application %q", app)
	}
	res, err := s.db.Exec(`UPDATE sites SET app = ? WHERE name = ?`, app, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("site %q: %w", name, ErrNotFound)
	}
	return nil
}

// SetAppReady records that the application of a site finished its setup in the browser.
func (s *Store) SetAppReady(name string, ready bool) error {
	res, err := s.db.Exec(`UPDATE sites SET app_ready = ? WHERE name = ?`, ready, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("site %q: %w", name, ErrNotFound)
	}
	return nil
}

// SetVhost stores the nginx configuration a person wrote for a site, or "" to go back to the one mpn makes.
func (s *Store) SetVhost(name, text string) error {
	res, err := s.db.Exec(`UPDATE sites SET vhost = ? WHERE name = ?`, text, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("site %q: %w", name, ErrNotFound)
	}
	return nil
}

// SetTLS records whether a site is served over SSL, from Let's Encrypt when it is.
func (s *Store) SetTLS(name string, on bool) error {
	if on {
		return s.SetSSL(name, site.SSLLetsEncrypt)
	}
	return s.SetSSL(name, site.SSLNone)
}

// SetDomain stores the domain of a site: its host names (the address first), the www choice, whether plain HTTP is still
// served and whether HTTPS is on. The host names are replaced all together, so a failure leaves the old ones.
func (s *Store) SetDomain(st site.Site) error {
	if err := st.Validate(); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id int64
	if err := tx.QueryRow(`SELECT id FROM sites WHERE name = ?`, st.Name).Scan(&id); err != nil {
		return fmt.Errorf("site %q: %w", st.Name, ErrNotFound)
	}
	mode := sslModeOf(st)
	if _, err := tx.Exec(`UPDATE sites SET www = ?, allow_http = ?, tls = ?, ssl_mode = ?, speed = ?, webmail = ? WHERE id = ?`, st.WWW, st.AllowHTTP, mode != "", mode, st.Speed, st.Webmail, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM hostnames WHERE site_id = ?`, id); err != nil {
		return err
	}
	for i, h := range st.Hostnames {
		if _, err := tx.Exec(`INSERT INTO hostnames (hostname, site_id, position) VALUES (?,?,?)`, h, id, i); err != nil {
			return wrap(err)
		}
	}
	return tx.Commit()
}

func (s *Store) DeleteSite(name string) error {
	res, err := s.db.Exec(`DELETE FROM sites WHERE name = ?`, name)
	if err != nil {
		if strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
			return fmt.Errorf("site %q still has databases: %w", name, ErrInUse)
		}
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("site %q: %w", name, ErrNotFound)
	}
	return nil
}

// AllocAppPort returns the lowest port in [lo, hi] that no site uses. Two callers can get the same
// port; the UNIQUE column then makes the second AddSite fail with ErrExists.
func (s *Store) AllocAppPort(lo, hi int) (int, error) {
	rows, err := s.db.Query(`SELECT app_port FROM sites WHERE app_port BETWEEN ? AND ? ORDER BY app_port`, lo, hi)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	next := lo
	for rows.Next() {
		var p int
		if err := rows.Scan(&p); err != nil {
			return 0, err
		}
		if p > next {
			break
		}
		next = p + 1
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if next > hi {
		return 0, fmt.Errorf("no free port between %d and %d", lo, hi)
	}
	return next, nil
}

// load reads sites matching where (a fixed string from this package, never user input) and their hostnames.
func (s *Store) load(where string, args ...any) ([]site.Site, error) {
	rows, err := s.db.Query(`SELECT id, name, kind, system_user, root_dir, php_version, app_port, entry, upstream, tls, ssl_mode, php_ini, mem_mb, www, allow_http, vhost, speed, webmail, app, app_ready, mise_task, access_id
		FROM sites `+where+` ORDER BY name`, args...)
	if err != nil {
		return nil, err
	}
	var out []site.Site
	index := map[int64]int{}
	for rows.Next() {
		var id int64
		var st site.Site
		var kind string
		var php, up, entry, ini sql.NullString
		var port sql.NullInt64
		var miseTask string
		if err := rows.Scan(&id, &st.Name, &kind, &st.User, &st.Root, &php, &port, &entry, &up, &st.TLS, &st.SSLMode, &ini, &st.MemMB, &st.WWW, &st.AllowHTTP, &st.CustomVhost, &st.Speed, &st.Webmail, &st.App, &st.AppReady, &miseTask, &st.AccessID); err != nil {
			rows.Close()
			return nil, err
		}
		st.SSLMode = sslModeOf(st)
		st.ID = id
		if ini.String != "" {
			if err := json.Unmarshal([]byte(ini.String), &st.PHPIni); err != nil {
				rows.Close()
				return nil, fmt.Errorf("site %q: the stored PHP settings are not readable: %w", st.Name, err)
			}
		}
		st.Kind, st.PHPVersion, st.AppPort, st.Upstream, st.MiseTask = site.Kind(kind), php.String, int(port.Int64), up.String, miseTask
		st.Entry = entry.String
		index[id] = len(out)
		out = append(out, st)
	}
	err = rows.Err()
	rows.Close() // must be closed before the next query because the pool has one connection
	if err != nil {
		return nil, err
	}
	hrows, err := s.db.Query(`SELECT site_id, hostname FROM hostnames
		WHERE site_id IN (SELECT id FROM sites `+where+`) ORDER BY site_id, position`, args...)
	if err != nil {
		return nil, err
	}
	defer hrows.Close()
	for hrows.Next() {
		var id int64
		var h string
		if err := hrows.Scan(&id, &h); err != nil {
			return nil, err
		}
		out[index[id]].Hostnames = append(out[index[id]].Hostnames, h)
	}
	return out, hrows.Err()
}

// appPort is the port column of an app: NULL for one on a Unix socket, so any number of them fit the UNIQUE column.
func appPort(st site.Site) any {
	if st.AppPort == 0 {
		return nil
	}
	return st.AppPort
}

// iniJSON stores the PHP settings of a site, or NULL when there are none.
func iniJSON(m map[string]string) any {
	if len(m) == 0 {
		return nil
	}
	b, _ := json.Marshal(m) // a map of strings always marshals
	return string(b)
}

// UpdateSite stores the settings of a site that a person may change: its web root, PHP version and settings, the version,
// port, start script and memory of a node app, and the address a proxy forwards to. The name, kind and account never
// change; asking for that is an error, so a caller cannot rename a site into somebody else's account by mistake.
func (s *Store) UpdateSite(st site.Site) error {
	if err := st.Validate(); err != nil {
		return err
	}
	old, err := s.GetSite(st.Name)
	if err != nil {
		return err
	}
	if old.Kind != st.Kind || old.User != st.User {
		return fmt.Errorf("site %q: the kind and the account of a site cannot be changed", st.Name)
	}
	var php, upstream, phpIni, port, entry any
	mem := 0
	switch st.Kind {
	case site.PHP:
		php, phpIni = nullStr(st.PHPVersion), iniJSON(st.PHPIni)
	case site.Binary:
		port, entry, mem = appPort(st), nullStr(st.Entry), st.MemMB
	case site.Mise:
		port, mem = st.AppPort, st.MemMB
	case site.Proxy:
		upstream = nullStr(st.Upstream)
	}
	res, err := s.db.Exec(`UPDATE sites SET root_dir = ?, php_version = ?, php_ini = ?, app_port = ?, entry = ?, upstream = ?, mem_mb = ?, mise_task = ?
		WHERE name = ?`, st.Root, php, phpIni, port, entry, upstream, mem, st.MiseTask, st.Name)
	if err != nil {
		return wrap(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("site %q: %w", st.Name, ErrNotFound)
	}
	return nil
}

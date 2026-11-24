// Package backup builds the plans that save a site (its web root and its databases) and put it back. Like provision,
// it only describes the work; nothing here touches the machine.
//
// A backup is a directory /var/backups/mpn/<site>/<stamp>/ owned by root with mode 0700, holding files.tar.gz, one
// <database>.sql.gz per database, and a MANIFEST written last. A directory without MANIFEST is an unfinished backup.
package backup

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"mpn/internal/dbadmin"
	"mpn/internal/installer"
	"mpn/internal/site"
)

// Base is the directory that holds every site's backups. Only root can enter it.
const Base = "/var/backups/mpn"

var stampRe = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}$`)

// ValidStamp reports whether s looks like 20261104-031500, the name of one backup.
func ValidStamp(s string) bool { return stampRe.MatchString(s) }

func SiteDir(name string) string    { return Base + "/" + name }
func Dir(name, stamp string) string { return SiteDir(name) + "/" + stamp }

// Manifest says what a backup holds.
//
// Htdocs is set when files.tar.gz holds the whole htdocs of a site whose web root lies below it (mpn 1.1.1 and later).
// Backups made before that hold only the web root, and are put back there.
//
// From 1.2.0 a MANIFEST also holds the SHA-256 of each file (sum=<file>:<hex>) and ends with mac=<hex>, an HMAC-SHA256 of all
// the lines above it with the key of this server (KeyFile). A backup that comes back from off-site storage is used only when its
// MAC is right, so whoever can write to the storage cannot hand the server a backup of their own.
type Manifest struct {
	Site    string
	Stamp   string
	Files   bool
	Htdocs  bool
	DBs     []string
	Engines map[string]string // absent entries are MySQL-family for manifests made before engine tracking
	Sums    map[string]string // file name to SHA-256, in hex
	Signed  bool              // the mac line was there and is right for this server's key (set by ParseManifest)
}

const header = "mpn backup 1"

// body is the MANIFEST without its mac line.
func (m Manifest) body() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\nsite=%s\nstamp=%s\n", header, m.Site, m.Stamp)
	if m.Files {
		b.WriteString("files=files.tar.gz\n")
	}
	if m.Htdocs {
		b.WriteString("files_root=htdocs\n")
	}
	for _, d := range m.DBs {
		fmt.Fprintf(&b, "db=%s\n", d)
		if m.Engines[d] == "postgresql" {
			fmt.Fprintf(&b, "db_engine=%s:postgresql\n", d)
		}
	}
	names := make([]string, 0, len(m.Sums))
	for n := range m.Sums {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(&b, "sum=%s:%s\n", n, m.Sums[n])
	}
	return b.String()
}

// String is the MANIFEST without a MAC, as mpn 1.1 wrote it; Sign gives the signed text.
func (m Manifest) String() string { return m.body() }

// Sign returns the MANIFEST with its mac line, made with key.
func (m Manifest) Sign(key []byte) string {
	b := m.body()
	return b + "mac=" + mac(key, b) + "\n"
}

func mac(key []byte, text string) string {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(text))
	return hex.EncodeToString(h.Sum(nil))
}

// ParseManifest reads a MANIFEST. Every value is checked, because a restore puts the names into shell commands. Without a key,
// or for a MANIFEST of 1.1 that has no mac line, Signed is false.
func ParseManifest(text string, key ...[]byte) (Manifest, error) {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if lines[0] != header {
		return Manifest{}, fmt.Errorf("not an mpn backup manifest (first line %q)", lines[0])
	}
	var m Manifest
	if last := lines[len(lines)-1]; strings.HasPrefix(last, "mac=") {
		got := strings.TrimPrefix(last, "mac=")
		lines = lines[:len(lines)-1]
		body := strings.Join(lines, "\n") + "\n"
		for _, k := range key {
			if len(k) > 0 && hmac.Equal([]byte(got), []byte(mac(k, body))) {
				m.Signed = true
			}
		}
	}
	for _, l := range lines[1:] {
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			return Manifest{}, fmt.Errorf("manifest line %q has no '='", l)
		}
		switch k {
		case "site":
			if !site.ValidName(v) {
				return Manifest{}, fmt.Errorf("manifest has an invalid site name %q", v)
			}
			m.Site = v
		case "stamp":
			if !ValidStamp(v) {
				return Manifest{}, fmt.Errorf("manifest has an invalid stamp %q", v)
			}
			m.Stamp = v
		case "files":
			if v != "files.tar.gz" {
				return Manifest{}, fmt.Errorf("manifest has an unexpected files entry %q", v)
			}
			m.Files = true
		case "files_root":
			if v != "htdocs" {
				return Manifest{}, fmt.Errorf("manifest has an unexpected files_root %q", v)
			}
			m.Htdocs = true
		case "db":
			if !dbadmin.ValidName(v) {
				return Manifest{}, fmt.Errorf("manifest has an invalid database name %q", v)
			}
			m.DBs = append(m.DBs, v)
		case "db_engine":
			db, engine, ok := strings.Cut(v, ":")
			if !ok || !dbadmin.ValidName(db) || engine != "postgresql" || !contains(m.DBs, db) {
				return Manifest{}, fmt.Errorf("manifest has an invalid database engine line %q", v)
			}
			if m.Engines == nil {
				m.Engines = map[string]string{}
			}
			m.Engines[db] = engine
		case "sum":
			n, h, ok := strings.Cut(v, ":")
			if !ok || !sumRe.MatchString(h) || (n != "files.tar.gz" && !(strings.HasSuffix(n, ".sql.gz") && dbadmin.ValidName(strings.TrimSuffix(n, ".sql.gz")))) {
				return Manifest{}, fmt.Errorf("manifest has an invalid sum line %q", v)
			}
			if m.Sums == nil {
				m.Sums = map[string]string{}
			}
			m.Sums[n] = h
		case "mac":
			return Manifest{}, fmt.Errorf("manifest has a mac line that is not the last one")
		default:
			return Manifest{}, fmt.Errorf("manifest has an unknown key %q", k)
		}
	}
	if m.Site == "" || m.Stamp == "" {
		return Manifest{}, fmt.Errorf("manifest lacks a site or a stamp")
	}
	return m, nil
}

// Engine returns the recorded engine, defaulting legacy manifests to MySQL-family.
func (m Manifest) Engine(db string) string {
	if m.Engines[db] == "postgresql" {
		return "postgresql"
	}
	return "mysql"
}

var sumRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// KeyFile is the key the MANIFEST of every backup of this server is signed with. It is made once, by root, and never leaves the
// server: a copy of the backups off the server does not carry it.
const KeyFile = "/etc/mpn/backup.key"

// CheckFileSum compares a file of a backup with the SHA-256 its MANIFEST holds. A MANIFEST without sums (made before 1.2.0) has
// nothing to compare, and is accepted only where its MAC was not asked for.
func CheckFileSum(m Manifest, path string) error {
	want, ok := m.Sums[filepath.Base(path)]
	if !ok {
		return fmt.Errorf("the MANIFEST has no checksum for %s", filepath.Base(path))
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != want {
		return fmt.Errorf("its SHA-256 differs from the one in the MANIFEST")
	}
	return nil
}

// checkDBs makes sure every database name is safe and belongs to the site.
func checkDBs(siteName string, dbs []string) error {
	for _, d := range dbs {
		if !dbadmin.ValidName(d) || !dbadmin.OwnedBy(d, siteName) {
			return fmt.Errorf("database %q is not a database of site %q", d, siteName)
		}
	}
	return nil
}

// CreatePlan saves the web root and the given databases. dump is the path of mariadb-dump or mysqldump.
//
// The archive is made by the site's own user, not by root: root reading a directory the user controls could be tricked
// with a symlink into copying a file the user cannot read, and the user could then get it back with a restore.
// tar's exit status 1 means only that a file changed while it was read, which is normal on a live site. An empty or
// cut-off archive (for instance when runuser itself fails, which also exits 1) is caught by gzip -t.
func CreatePlan(s site.Site, stamp string, dbs []string, dump string) (installer.Plan, error) {
	return CreatePlanWithEngines(s, stamp, dbs, dump, nil)
}

// CreatePlanWithEngines saves databases with their own engine. An empty engine entry is legacy MySQL-family.
func CreatePlanWithEngines(s site.Site, stamp string, dbs []string, dump string, engines map[string]string) (installer.Plan, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if !ValidStamp(stamp) {
		return nil, fmt.Errorf("invalid backup stamp %q", stamp)
	}
	needsMySQL := false
	for _, d := range dbs {
		if engines[d] != "postgresql" {
			needsMySQL = true
		}
	}
	if needsMySQL && !site.SafePath(dump) {
		return nil, fmt.Errorf("invalid dump program %q", dump)
	}
	if err := checkDBs(s.Name, dbs); err != nil {
		return nil, err
	}
	for _, d := range dbs {
		if e := engines[d]; e != "" && e != "mysql" && e != "postgresql" {
			return nil, fmt.Errorf("unsupported database engine %q for %q", e, d)
		}
	}
	dir := Dir(s.Name, stamp)
	files := dir + "/files.tar.gz"
	plan := installer.Plan{
		{Name: "backup directory", Actions: []installer.Action{
			installer.Run("install", "-d", "-m", "0700", "-o", "root", "-g", "root", Base, SiteDir(s.Name)),
			installer.Run("mkdir", "-m", "0700", dir), // fails when it exists, so a backup is never overwritten
		}},
		{Name: "files", Actions: []installer.Action{installer.Run("sh", "-c", fmt.Sprintf(
			`[ -d %[2]s ] || { echo 'site root %[2]s does not exist' >&2; exit 1; }; umask 077; runuser -u %[1]s -- tar -C %[2]s -czf - . > %[3]s; rc=$?; [ "$rc" -le 1 ] && gzip -t %[3]s || { echo 'archiving the site files failed' >&2; exit 1; }`,
			s.User, filesRoot(s), files))}},
	}
	for _, d := range dbs {
		sql := fmt.Sprintf("%s/%s.sql", dir, d)
		if engines[d] == "postgresql" {
			plan = append(plan, installer.Step{Name: "database " + d, Actions: []installer.Action{installer.Run("sh", "-c", fmt.Sprintf(
				`umask 077; runuser -u postgres -- pg_dump --clean --if-exists --no-owner --no-acl %[1]s > %[2]s || { rm -f %[2]s; echo 'dump of %[1]s failed' >&2; exit 1; }; gzip -f %[2]s`, d, sql))}})
			continue
		}
		plan = append(plan, installer.Step{Name: "database " + d, Actions: []installer.Action{installer.Run("sh", "-c", fmt.Sprintf(
			`umask 077; %[1]s --single-transaction --routines --events --triggers --default-character-set=utf8mb4 %[2]s > %[3]s || { rm -f %[3]s; echo 'dump of %[2]s failed' >&2; exit 1; }; gzip -f %[3]s`,
			dump, d, sql))}})
	}
	_, below := s.RootSub()
	m := Manifest{Site: s.Name, Stamp: stamp, Files: true, Htdocs: below && s.Root != site.HtdocsOf(s.User), DBs: dbs, Engines: engines}
	// The MANIFEST is written by "mpn backup seal", which reads the files to sum them and signs the result with KeyFile; it is the
	// last step, so a backup cut off before it has no MANIFEST and counts as unfinished.
	args := []string{"backup", "seal", "-dir", dir, "-site", s.Name, "-stamp", stamp}
	if m.Htdocs {
		args = append(args, "-htdocs")
	}
	for _, d := range dbs {
		args = append(args, "-dbname", d)
	}
	return append(plan, installer.Step{Name: "manifest", Actions: []installer.Action{
		installer.Run(append([]string{"/usr/local/bin/mpn"}, args...)...),
	}}), nil
}

// Seal sums the files of a finished backup and writes its signed MANIFEST, through a temporary file and a rename.
func Seal(dir string, m Manifest, key []byte) error {
	names := []string{}
	if m.Files {
		names = append(names, "files.tar.gz")
	}
	for _, d := range m.DBs {
		names = append(names, d+".sql.gz")
	}
	m.Sums = map[string]string{}
	for _, n := range names {
		f, err := os.Open(filepath.Join(dir, n))
		if err != nil {
			return err
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil {
			return err
		}
		m.Sums[n] = hex.EncodeToString(h.Sum(nil))
	}
	tmp := filepath.Join(dir, ".MANIFEST.tmp")
	if err := os.WriteFile(tmp, []byte(m.Sign(key)), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "MANIFEST"))
}

// LoadKey reads KeyFile, making it the first time (32 random bytes, root only).
func LoadKey(path string) ([]byte, error) {
	if b, err := os.ReadFile(path); err == nil {
		k, err := hex.DecodeString(strings.TrimSpace(string(b)))
		if err != nil || len(k) != 32 {
			return nil, fmt.Errorf("%s is damaged", path)
		}
		return k, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return LoadKey(path) // made at the same moment by another backup
		}
		return nil, err
	}
	_, err = f.WriteString(hex.EncodeToString(k) + "\n")
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return k, err
}

// filesRoot is the folder a backup archives and a restore extracts into: htdocs when the web root lies below it, so a site
// whose web root is htdocs/public (Laravel, Flarum, Symfony) keeps its configuration, storage and vendor folders, which sit
// beside public and would otherwise be left out. A web root outside htdocs is archived as it is.
func filesRoot(s site.Site) string {
	if _, ok := s.RootSub(); ok {
		return site.HtdocsOf(s.User)
	}
	return s.Root
}

// restoreRoot is where a backup's archive is extracted: htdocs for an archive of the whole htdocs, the web root for one
// made before 1.1.1 (which holds only the web root). An archive of htdocs is refused below for a site whose web root is
// no longer under htdocs, by RestorePlan.
func restoreRoot(s site.Site, m Manifest) string {
	if m.Htdocs {
		return site.HtdocsOf(s.User)
	}
	return s.Root
}

// RestorePlan puts back the files and/or the given databases from one backup.
//
// Files are extracted by the site's user, so the archive can only write where that user could write anyway. Files
// that exist now but are not in the backup stay. Each dump drops and re-creates its own tables; the database itself
// must already exist, and tables created since the backup stay. client is the path of the mariadb or mysql program.
func RestorePlan(s site.Site, m Manifest, files bool, dbs []string, client string) (installer.Plan, error) {
	return RestorePlanWithEngines(s, m, files, dbs, client, nil)
}

// RestorePlanWithEngines restores each dump with the database client recorded in its manifest.
func RestorePlanWithEngines(s site.Site, m Manifest, files bool, dbs []string, client string, currentEngines map[string]string) (installer.Plan, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if m.Site != s.Name {
		return nil, fmt.Errorf("this backup is of site %q, not %q", m.Site, s.Name)
	}
	if !ValidStamp(m.Stamp) {
		return nil, fmt.Errorf("invalid backup stamp %q", m.Stamp)
	}
	if files && !m.Files {
		return nil, fmt.Errorf("backup %s has no files", m.Stamp)
	}
	if _, below := s.RootSub(); files && m.Htdocs && !below {
		return nil, fmt.Errorf("backup %s holds the folder htdocs, but the web root of site %q is now outside it (%s)", m.Stamp, s.Name, s.Root)
	}
	needsMySQL := false
	for _, d := range dbs {
		if m.Engine(d) != "postgresql" {
			needsMySQL = true
		}
	}
	if needsMySQL && !site.SafePath(client) {
		return nil, fmt.Errorf("invalid database client %q", client)
	}
	if err := checkDBs(s.Name, dbs); err != nil {
		return nil, err
	}
	for _, d := range dbs {
		if !contains(m.DBs, d) {
			return nil, fmt.Errorf("backup %s has no database %q (it has: %s)", m.Stamp, d, strings.Join(m.DBs, ", "))
		}
	}
	dir := Dir(s.Name, m.Stamp)
	var plan installer.Plan
	if files {
		f := dir + "/files.tar.gz"
		plan = append(plan, installer.Step{Name: "files", Actions: []installer.Action{installer.Run("sh", "-c", fmt.Sprintf(
			`[ -d %[2]s ] || { echo 'site root %[2]s does not exist; apply the site first' >&2; exit 1; }; gzip -t %[3]s && runuser -u %[1]s -- tar -C %[2]s -xzpf - < %[3]s`,
			s.User, restoreRoot(s, m), f))}})
	}
	for _, d := range dbs {
		f := fmt.Sprintf("%s/%s.sql.gz", dir, d)
		if m.Engine(d) != currentEngine(currentEngines, d) {
			return nil, fmt.Errorf("database %q uses %s, but its backup uses %s", d, currentEngine(currentEngines, d), m.Engine(d))
		}
		if m.Engine(d) == "postgresql" {
			plan = append(plan, installer.Step{Name: "database " + d, Actions: []installer.Action{installer.Run("sh", "-c", fmt.Sprintf(
				`gzip -t %[1]s && gzip -dc %[1]s | runuser -u postgres -- psql -X -v ON_ERROR_STOP=1 --role=%[2]s -d %[2]s`, f, d))}})
			continue
		}
		plan = append(plan, installer.Step{Name: "database " + d, Actions: []installer.Action{installer.Run("sh", "-c", fmt.Sprintf(
			`gzip -t %[2]s && gzip -dc %[2]s | %[1]s --default-character-set=utf8mb4 %[3]s`, client, f, d))}})
	}
	if len(plan) == 0 {
		return nil, fmt.Errorf("nothing to restore: choose the files, a database, or both")
	}
	return plan, nil
}

func currentEngine(engines map[string]string, db string) string {
	if engines[db] == "postgresql" {
		return "postgresql"
	}
	return "mysql"
}

// Expired returns the stamps to delete so that only the newest keep remain. Names that are not stamps are ignored.
func Expired(stamps []string, keep int) []string {
	if keep < 1 {
		return nil
	}
	var ok []string
	for _, s := range stamps {
		if ValidStamp(s) {
			ok = append(ok, s)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ok)))
	if len(ok) <= keep {
		return nil
	}
	return ok[keep:]
}

// PrunePlan deletes the named backups of a site.
func PrunePlan(siteName string, stamps []string) (installer.Plan, error) {
	if !site.ValidName(siteName) {
		return nil, fmt.Errorf("invalid site name %q", siteName)
	}
	var acts []installer.Action
	for _, st := range stamps {
		if !ValidStamp(st) {
			return nil, fmt.Errorf("invalid backup stamp %q", st)
		}
		acts = append(acts, installer.Run("rm", "-rf", "--", Dir(siteName, st)))
	}
	return installer.Plan{{Name: "delete old backups", Actions: acts}}, nil
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

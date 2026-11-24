package store

import (
	"fmt"
	"time"
)

// Database is a database that mpn created for a site. The password is not kept anywhere: the database server holds
// its hash, and a forgotten password is replaced, not recovered.
type Database struct {
	Name   string
	Site   string
	Engine string
}

// AddDatabase records a database as belonging to a site.
func (s *Store) AddDatabase(site, name string) error {
	return s.AddDatabaseEngine(site, name, "mysql")
}

// AddDatabaseEngine records a database and the engine that owns it. Existing rows migrate to mysql.
func (s *Store) AddDatabaseEngine(site, name, engine string) error {
	if engine != "mysql" && engine != "postgresql" {
		return fmt.Errorf("unsupported database engine %q", engine)
	}
	res, err := s.db.Exec(`INSERT INTO databases (name, site_id, created_at, engine) SELECT ?, id, ?, ? FROM sites WHERE name = ?`,
		name, time.Now().Unix(), engine, site)
	if err != nil {
		return wrap(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("site %q: %w", site, ErrNotFound)
	}
	return nil
}

// ListDatabases returns the databases of one site, or of all sites when site is empty, ordered by name.
func (s *Store) ListDatabases(site string) ([]Database, error) {
	q, args := `SELECT d.name, COALESCE(s.name,''), d.engine FROM databases d LEFT JOIN sites s ON s.id = d.site_id ORDER BY d.name`, []any{}
	if site != "" {
		q, args = `SELECT d.name, COALESCE(s.name,''), d.engine FROM databases d JOIN sites s ON s.id = d.site_id WHERE s.name = ? ORDER BY d.name`, []any{site}
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Database
	for rows.Next() {
		var d Database
		if err := rows.Scan(&d.Name, &d.Site, &d.Engine); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) GetDatabase(name string) (Database, error) {
	var d Database
	err := s.db.QueryRow(`SELECT d.name, COALESCE(s.name,''), d.engine FROM databases d LEFT JOIN sites s ON s.id = d.site_id WHERE d.name = ?`, name).Scan(&d.Name, &d.Site, &d.Engine)
	if err != nil {
		return Database{}, fmt.Errorf("database %q: %w", name, ErrNotFound)
	}
	return d, nil
}

func (s *Store) DeleteDatabase(name string) error {
	res, err := s.db.Exec(`DELETE FROM databases WHERE name = ?`, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("database %q: %w", name, ErrNotFound)
	}
	return nil
}

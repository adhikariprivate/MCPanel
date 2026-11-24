package store

import (
	"fmt"
	"time"

	"mpn/internal/bgservice"
	"mpn/internal/site"
)

func (s *Store) BackgroundServices(siteName string) ([]bgservice.Service, error) {
	rows, err := s.db.Query(`SELECT b.id,b.name,b.kind,b.command,b.task,b.memory_mb FROM background_services b JOIN sites x ON x.id=b.site_id WHERE x.name=? ORDER BY b.id`, siteName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []bgservice.Service{}
	for rows.Next() {
		var v bgservice.Service
		v.Site = siteName
		if err := rows.Scan(&v.ID, &v.Name, &v.Kind, &v.Command, &v.Task, &v.MemoryMB); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) BackgroundService(siteName string, id int64) (bgservice.Service, error) {
	var v bgservice.Service
	v.Site = siteName
	err := s.db.QueryRow(`SELECT b.id,b.name,b.kind,b.command,b.task,b.memory_mb FROM background_services b JOIN sites x ON x.id=b.site_id WHERE x.name=? AND b.id=?`, siteName, id).Scan(&v.ID, &v.Name, &v.Kind, &v.Command, &v.Task, &v.MemoryMB)
	if err != nil {
		return v, fmt.Errorf("site %q has no background service %d: %w", siteName, id, ErrNotFound)
	}
	return v, nil
}

func (s *Store) AddBackgroundService(siteName string, v bgservice.Service) (int64, error) {
	if err := bgservice.Valid(v); err != nil {
		return 0, err
	}
	x, err := s.GetSite(siteName)
	if err != nil {
		return 0, err
	}
	if v.Kind == bgservice.Mise && x.Kind != site.Mise {
		return 0, fmt.Errorf("mise tasks require a mise site")
	}
	if v.Kind == bgservice.PHP && x.Kind != site.PHP {
		return 0, fmt.Errorf("PHP commands require a PHP site")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var siteID int64
	if err := tx.QueryRow(`SELECT id FROM sites WHERE name=?`, siteName).Scan(&siteID); err != nil {
		return 0, fmt.Errorf("site %q: %w", siteName, ErrNotFound)
	}
	now := time.Now().Unix()
	res, err := tx.Exec(`INSERT INTO background_services(site_id,name,kind,command,task,memory_mb,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, siteID, v.Name, v.Kind, v.Command, v.Task, v.MemoryMB, now, now)
	if err != nil {
		return 0, wrap(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func (s *Store) UpdateBackgroundService(siteName string, id int64, v bgservice.Service) error {
	if err := bgservice.Valid(v); err != nil {
		return err
	}
	x, err := s.GetSite(siteName)
	if err != nil {
		return err
	}
	if v.Kind == bgservice.Mise && x.Kind != site.Mise {
		return fmt.Errorf("mise tasks require a mise site")
	}
	if v.Kind == bgservice.PHP && x.Kind != site.PHP {
		return fmt.Errorf("PHP commands require a PHP site")
	}
	res, err := s.db.Exec(`UPDATE background_services SET name=?,kind=?,command=?,task=?,memory_mb=?,updated_at=? WHERE id=? AND site_id=(SELECT id FROM sites WHERE name=?)`, v.Name, v.Kind, v.Command, v.Task, v.MemoryMB, time.Now().Unix(), id, siteName)
	if err != nil {
		return wrap(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("site %q has no background service %d: %w", siteName, id, ErrNotFound)
	}
	return nil
}

func (s *Store) DeleteBackgroundService(siteName string, id int64) error {
	res, err := s.db.Exec(`DELETE FROM background_services WHERE id=? AND site_id=(SELECT id FROM sites WHERE name=?)`, id, siteName)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("site %q has no background service %d: %w", siteName, id, ErrNotFound)
	}
	return nil
}

package store

import (
	"strings"
	"time"

	"mpn/internal/audit"
)

// AddAudit writes one entry of the activity log. At is set to now when it is zero.
func (s *Store) AddAudit(e audit.Entry) error {
	if e.At == 0 {
		e.At = time.Now().Unix()
	}
	_, err := s.db.Exec(`INSERT INTO audit_log (at, ip, site, op, kind, detail, ok, error, danger) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.At, e.IP, e.Site, e.Op, e.Kind, e.Detail, e.OK, e.Error, e.Danger)
	return err
}

// PruneAudit deletes the entries older than before and says how many went.
func (s *Store) PruneAudit(before time.Time) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM audit_log WHERE at < ?`, before.Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ListAudit returns the entries the filter chooses, newest first. The filter must already be checked (audit.Valid...); a day is read
// in the time zone loc.
func (s *Store) ListAudit(f audit.Filter, loc *time.Location) (audit.Page, error) {
	var where []string
	var args []any
	if f.Site != "" {
		where, args = append(where, "site = ?"), append(args, f.Site)
	} else if len(f.Sites) > 0 {
		marks := strings.TrimRight(strings.Repeat("?,", len(f.Sites)), ",")
		where = append(where, "site IN ("+marks+")")
		for _, name := range f.Sites {
			args = append(args, name)
		}
	}
	if f.IP != "" {
		// The start of the address. The characters of an address are never wildcards of LIKE.
		where, args = append(where, "ip LIKE ?"), append(args, strings.ToLower(f.IP)+"%")
	}
	if f.Kind != "" {
		where, args = append(where, "kind = ?"), append(args, f.Kind)
	}
	if f.From != "" {
		if d, err := time.ParseInLocation("2006-01-02", f.From, loc); err == nil {
			where, args = append(where, "at >= ?"), append(args, d.Unix())
		}
	}
	if f.To != "" {
		if d, err := time.ParseInLocation("2006-01-02", f.To, loc); err == nil {
			where, args = append(where, "at < ?"), append(args, d.AddDate(0, 0, 1).Unix())
		}
	}
	cond := ""
	if len(where) > 0 {
		cond = " WHERE " + strings.Join(where, " AND ")
	}
	page := audit.Page{Entries: []audit.Entry{}}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM audit_log`+cond, args...).Scan(&page.Total); err != nil {
		return page, err
	}
	limit := f.Limit
	if limit <= 0 || limit > audit.MaxPage {
		limit = audit.MaxPage
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.Query(`SELECT id, at, ip, site, op, kind, detail, ok, error, danger FROM audit_log`+cond+` ORDER BY at DESC, id DESC LIMIT ? OFFSET ?`,
		append(args, limit, offset)...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var e audit.Entry
		if err := rows.Scan(&e.ID, &e.At, &e.IP, &e.Site, &e.Op, &e.Kind, &e.Detail, &e.OK, &e.Error, &e.Danger); err != nil {
			return page, err
		}
		page.Entries = append(page.Entries, e)
	}
	return page, rows.Err()
}

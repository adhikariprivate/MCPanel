package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"mpn/internal/audit"
	"mpn/internal/site"
	"mpn/internal/store"
)

func auditUsage() {
	fmt.Fprintln(os.Stderr, "usage: mpn audit <list|prune> [flags]")
	os.Exit(2)
}

// runAudit reads the activity log of the panel (see internal/audit). The root helper writes it; this command only reads it, and prune
// deletes what is older than the retention.
func runAudit(args []string) {
	if len(args) == 0 {
		auditUsage()
	}
	fs := flag.NewFlagSet("audit "+args[0], flag.ExitOnError)
	db := fs.String("db", defaultDB, "database path")
	var f audit.Filter
	var sites string
	fs.StringVar(&f.Site, "site", "", "list: only this website")
	fs.StringVar(&sites, "sites", "", "list: only these websites")
	fs.StringVar(&f.IP, "ip", "", "list: only addresses that start with this")
	fs.StringVar(&f.Kind, "kind", "", "list: only this kind of activity")
	fs.StringVar(&f.From, "from", "", "list: from this day (YYYY-MM-DD)")
	fs.StringVar(&f.To, "to", "", "list: up to this day, included")
	fs.IntVar(&f.Limit, "limit", 100, "list: most entries")
	fs.IntVar(&f.Offset, "offset", 0, "list: entries to skip")
	asJSON := fs.Bool("json", false, "list: print JSON")
	asCSV := fs.Bool("csv", false, "list: print CSV")
	_ = fs.Parse(args[1:])
	if sites != "" {
		f.Sites = strings.Split(sites, ",")
		for _, name := range f.Sites {
			if !site.ValidName(name) {
				fail(fmt.Errorf("invalid site %q", name))
			}
		}
	}
	switch args[0] {
	case "list":
		for _, err := range []error{audit.ValidKind(f.Kind), audit.ValidDay(f.From), audit.ValidDay(f.To), audit.ValidIPFilter(f.IP)} {
			fail(err)
		}
		st := openStore(*db)
		defer st.Close()
		page, err := st.ListAudit(f, time.Local)
		fail(err)
		switch {
		case *asJSON:
			printJSON(page)
		case *asCSV:
			w := csv.NewWriter(os.Stdout)
			_ = w.Write(audit.CSVHeader)
			for _, e := range page.Entries {
				_ = w.Write(audit.CSVRow(e, time.Local))
			}
			w.Flush()
			fail(w.Error())
		default:
			for _, e := range page.Entries {
				ok := "ok"
				if !e.OK {
					ok = "FAILED " + e.Error
				}
				fmt.Printf("%s\t%s\t%s\t%s\t%s\t%s\n", time.Unix(e.At, 0).Format("2006-01-02 15:04:05"), e.IP, e.Site, e.Op, e.Detail, ok)
			}
		}
	case "prune":
		st := openStore(*db)
		defer st.Close()
		n, err := st.PruneAudit(time.Now().Add(-audit.Retention))
		fail(err)
		fmt.Printf("%d entries older than %d days deleted\n", n, int(audit.Retention.Hours()/24))
	default:
		auditUsage()
	}
}

// auditWriter writes the entries of the root helper into the database. It opens the database for each entry, so the helper holds
// nothing open between requests, and a failure is said in the journal and never stops the request.
func auditWriter(path string) func(audit.Entry) {
	return func(e audit.Entry) {
		st, err := store.Open(path)
		if err == nil {
			err = st.AddAudit(e)
			st.Close()
		}
		if err != nil {
			log.Printf("activity log: %s %s could not be written: %v", e.Op, e.Site, err)
		}
	}
}

// pruneAuditDaily deletes old entries once at start and then every day.
func pruneAuditDaily(path string) {
	for {
		if st, err := store.Open(path); err == nil {
			if n, err := st.PruneAudit(time.Now().Add(-audit.Retention)); err != nil {
				log.Printf("activity log: pruning failed: %v", err)
			} else if n > 0 {
				log.Printf("activity log: %d old entries deleted", n)
			}
			st.Close()
		}
		time.Sleep(24 * time.Hour)
	}
}

package ctl

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"mpn/internal/audit"
	"mpn/internal/cloudprov"
)

// No secret a request carries may reach the log: not on any operation, whatever else is filled in.
func TestTheLogNeverHoldsASecret(t *testing.T) {
	const secret = "S3cret-Value-42"
	for op := range writes {
		r := Request{Op: op, Site: "blog", Password: secret, MailPassword: secret, CFToken: secret, Key: secret, Cert: secret, Chain: secret,
			Vhost: secret, Command: secret, Schedule: secret, CloudSecret: secret, CloudClientID: secret, AppAdmin: secret, AppEmail: secret,
			FwComment: secret, Query: secret, PHPIni: map[string]string{"memory_limit": secret},
			CloudStorage: &cloudprov.Settings{Type: "s3", Values: map[string]string{"secret_access_key": secret}, Bucket: secret}}
		if d := r.AuditDetail(); strings.Contains(d, secret) {
			t.Errorf("%s: the log would hold a secret: %s", op, d)
		}
	}
}

func TestTheLogSaysWhatWasDone(t *testing.T) {
	for _, c := range []struct {
		r    Request
		want string
	}{
		{Request{Op: "db.add", Site: "blog", Name: "blog_shop"}, "name=blog_shop"},
		{Request{Op: "site.remove", Site: "blog", Purge: true}, "purge=yes"},
		{Request{Op: "firewall.add", FwAction: "deny", FwProto: "tcp", FwPort: "22", FwSource: "1.2.3.4"}, "action=deny proto=tcp port=22 source=1.2.3.4"},
		{Request{Op: "security.waf", SecMode: "off"}, "sec_mode=off"},
		{Request{Op: "site.set", Site: "blog", PHPIni: map[string]string{"upload_max_filesize": "64M", "memory_limit": "1G"}}, "php_ini=memory_limit,upload_max_filesize"},
		{Request{Op: "cloud.configure", CloudStorage: &cloudprov.Settings{Type: "b2"}}, "storage=b2"},
	} {
		if got := c.r.AuditDetail(); !strings.Contains(got, c.want) {
			t.Errorf("%s: %q lacks %q", c.r.Op, got, c.want)
		}
	}
	if !(Request{Op: "db.remove"}).Dangerous() || !(Request{Op: "security.waf", SecMode: "off"}).Dangerous() ||
		(Request{Op: "security.waf", SecMode: "block"}).Dangerous() || (Request{Op: "db.add"}).Dangerous() {
		t.Error("Dangerous is wrong")
	}
}

type auditLog struct {
	mu sync.Mutex
	e  []audit.Entry
}

func (l *auditLog) add(e audit.Entry) { l.mu.Lock(); l.e = append(l.e, e); l.mu.Unlock() }

// The helper writes every change with the address the web side sent and its outcome, and nothing for a page that only reads.
func TestTheHelperLogsChangesNotReads(t *testing.T) {
	l := &auditLog{}
	s := newServer(&fakeRunner{})
	s.Audit = l.add
	ask(t, s, 997, `{"op":"site.list","actor":"1.1.1.1"}`)
	ask(t, s, 997, `{"op":"db.add","site":"blog","name":"blog_x","actor":"1.1.1.1"}`)
	ask(t, s, 997, `{"op":"db.pmadrop","user":"pma_abc","actor":"1.1.1.1"}`)
	if len(l.e) != 1 || l.e[0].Op != "db.add" || l.e[0].IP != "1.1.1.1" || l.e[0].Site != "blog" || !l.e[0].OK || l.e[0].Kind != "database" {
		t.Fatalf("%+v", l.e)
	}
	if r := ask(t, s, 997, `{"op":"db.add","site":"blog","actor":"not an ip"}`); r.OK || len(l.e) != 1 {
		t.Errorf("a false address must stop the request: %+v", r)
	}
}

func TestTheHelperLogsFailuresToo(t *testing.T) {
	l := &auditLog{}
	s := newServer(&fakeRunner{err: errExit})
	s.Audit = l.add
	ask(t, s, 997, `{"op":"site.remove","site":"blog","actor":"2.2.2.2"}`)
	if len(l.e) != 1 || l.e[0].OK || l.e[0].Error == "" || l.e[0].Danger {
		t.Fatalf("%+v", l.e)
	}
}

// A note of the web side is written as it is and runs nothing; a name that is not one of the events is refused.
func TestNotesRunNothingAndOnlyKnownEventsAreTaken(t *testing.T) {
	l := &auditLog{}
	f := &fakeRunner{}
	s := newServer(f)
	s.Audit = l.add
	if r := ask(t, s, 997, `{"op":"audit.note","event":"login.fail","actor":"3.3.3.3","note":"user=other","failed":true}`); !r.OK {
		t.Fatal(r)
	}
	if r := ask(t, s, 997, `{"op":"audit.note","event":"db.remove","actor":"3.3.3.3"}`); r.OK {
		t.Error("an operation must not be written as a note: it would fake a change")
	}
	if f.called() != 0 || len(l.e) != 1 || l.e[0].Op != "login.fail" || l.e[0].OK || l.e[0].Kind != "auth" {
		t.Errorf("runner %d, log %+v", f.called(), l.e)
	}
}

func TestReadingTheLogIsChecked(t *testing.T) {
	r := Request{Op: "audit.list", Site: "blog", AuditIP: "1.1.", AuditKind: "database", From: "2026-11-01", To: "2026-11-20", Limit: 50, Offset: 100}
	got, err := r.Argv()
	if err != nil || strings.Join(got, " ") != "audit list -json -site=blog -sites= -ip=1.1. -kind=database -from=2026-11-01 -to=2026-11-20 -limit=50 -offset=100" {
		t.Fatalf("%q %v", got, err)
	}
	for _, bad := range []Request{
		{Op: "audit.list", Site: "-x"}, {Op: "audit.list", AuditSites: []string{"-x"}}, {Op: "audit.list", AuditIP: "1.1.1.1 -db=/tmp/x"}, {Op: "audit.list", AuditKind: "x"},
		{Op: "audit.list", From: "yesterday"}, {Op: "audit.list", Limit: 5000}, {Op: "audit.list", Offset: -1},
	} {
		if _, err := bad.Argv(); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
}

var errExit = errors.New("exit status 1")

package web

import (
	"net/url"
	"strings"
	"testing"

	"mpn/internal/ctl"
)

const noDatabaseStatus = `[
 {"key":"nginx","label":"nginx","group":"service","installed":true,"unit":"nginx.service","state":"active","active":true,"actions":["start","restart","reload"]},
 {"key":"mariadb","label":"MariaDB","group":"database","installable":true,"series":["10.11","11.4","11.8","12.3"],"default_series":"11.8"},
 {"key":"mysql","label":"MySQL","group":"database","installable":true,"series":["8.0","8.4"],"default_series":"8.4"}
 ,{"key":"postgresql","label":"PostgreSQL","group":"database","installable":true,"series":["16","17","18","19"],"default_series":"18"}
]`

func TestTheServicesPageOffersTheVersionsOfEachDatabaseServer(t *testing.T) {
	h, _, c, _ := setup(t, byOp(map[string]string{"services.status": noDatabaseStatus}))
	page := do(h, "GET", "/services", nil, cookie(c)).Body.String()
	for _, want := range []string{
		`<select id="series-mariadb" name="series"`, `<option value="10.11">`, `<option value="11.8" selected>`, `<option value="12.3">`,
		`<select id="series-mysql" name="series"`, `<option value="8.0">`, `<option value="8.4" selected>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	if strings.Contains(page, `<option value="9.`) {
		t.Error("a version the release does not offer is shown")
	}
}

func TestPostgreSQLSeriesSelectionReachesHelper(t *testing.T) {
	h, f, c, csrf := setupWith(t, byOp(map[string]string{"services.status": noDatabaseStatus, "services.install": "ok"}), nil)
	w := do(h, "POST", "/services/postgresql/install", url.Values{"csrf": {csrf}, "series": {"18"}}, cookie(c))
	if w.Code != 303 {
		t.Fatalf("install with PostgreSQL series: %d", w.Code)
	}
	for _, q := range f.reqs {
		if q.Op == "services.install" && q.SvcKey == "postgresql" && q.DBSeries == "18" {
			return
		}
	}
	t.Fatalf("PostgreSQL series did not reach helper: %+v", f.reqs)
}

func TestTheChosenVersionReachesTheHelperAndAWrongOneDoesNot(t *testing.T) {
	h2, f2, c2, csrf2 := setupWith(t, byOp(map[string]string{"services.status": noDatabaseStatus, "services.install": "ok"}), nil)

	r := do(h2, "POST", "/services/mariadb/install", url.Values{"csrf": {csrf2}, "series": {"10.11"}}, cookie(c2))
	if r.Code != 303 {
		t.Fatalf("install with a version: %d", r.Code)
	}
	var got ctl.Request
	for _, q := range f2.reqs {
		if q.Op == "services.install" {
			got = q
		}
	}
	if got.SvcKey != "mariadb" || got.DBSeries != "10.11" {
		t.Errorf("the helper got %+v", got)
	}
	argv, err := got.Argv()
	if err != nil || !strings.Contains(strings.Join(argv, " "), "-series=10.11") {
		t.Errorf("argv %v, %v", argv, err)
	}

	n := len(f2.reqs)
	for name, form := range map[string]url.Values{
		"shell text":       {"series": {"11.8; id"}},
		"a path":           {"series": {"../8.4"}},
		"a version of php": {"series": {"8.4"}},
	} {
		form.Set("csrf", csrf2)
		key := "mariadb"
		if name == "a version of php" {
			key = "php-8.4"
		}
		do(h2, "POST", "/services/"+key+"/install", form, cookie(c2))
		for _, q := range f2.reqs[n:] {
			if q.Op == "services.install" {
				t.Errorf("%s reached the helper: %+v", name, q)
			}
		}
	}
}

func TestADatabaseInstallWithoutAVersionUsesTheDefault(t *testing.T) {
	h, f, c, csrf := setupWith(t, byOp(map[string]string{"services.install": "ok"}), nil)
	do(h, "POST", "/services/mysql/install", url.Values{"csrf": {csrf}}, cookie(c))
	for _, q := range f.reqs {
		if q.Op == "services.install" {
			if q.DBSeries != "" {
				t.Errorf("no version was chosen but %q was sent", q.DBSeries)
			}
			return
		}
	}
	t.Error("the install did not reach the helper")
}

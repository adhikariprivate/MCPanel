package web

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"mpn/internal/ctl"
)

const (
	cloudOff     = `{"connected":false,"email":"","pending":null,"enabled":false,"state":null}`
	testClientID = "1234567890-abcdefg.apps.googleusercontent.com"
	testSecret   = "GOCSPX-abcdef_123456"
)

func cloudConnected(enabled bool, state string) string {
	return `{"connected":true,"email":"me@example.com","pending":null,"enabled":` + map[bool]string{true: "true", false: "false"}[enabled] + `,"state":` + state + `}`
}

func cloudAnswers(status string) func(ctl.Request) (ctl.Response, error) {
	return byOp(map[string]string{"site.list": blogList, "backup.list": `[]`, "cloud.status": status, "cloud.connect": "open the address",
		"cloud.finish": "connected", "cloud.enable": "ok", "cloud.sync": "started", "cloud.disconnect": "gone"})
}

func TestTheBackupsTabLinksToSharedOffsiteSettings(t *testing.T) {
	h, _, c, _ := setupWith(t, cloudAnswers(cloudOff), nil)
	body := do(h, "GET", "/sites/blog/backups", nil, cookie(c)).Body.String()
	for _, want := range []string{"Website này", "Kết nối dùng chung cho mọi website", `href="/backups/offsite"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, not := range []string{`action="/cloud/connect"`, `action="/cloud/configure"`, `action="/cloud/disconnect"`} {
		if strings.Contains(body, not) {
			t.Errorf("site page exposes global action %s", not)
		}
	}
}

func TestWhileWaitingForGoogleTheCodeAndTheAddressAreShown(t *testing.T) {
	exp := time.Now().Add(20 * time.Minute).Unix()
	status := `{"connected":false,"email":"","pending":{"user_code":"ABCD-EFGH","url":"https://www.google.com/device","expires":` + itoa(exp) + `},"enabled":false,"state":null}`
	h, _, c, _ := setupWith(t, cloudAnswers(status), nil)
	body := do(h, "GET", "/backups/offsite", nil, cookie(c)).Body.String()
	for _, want := range []string{"ABCD-EFGH", `href="https://www.google.com/device"`, `rel="noopener noreferrer"`, `action="/cloud/finish"`, "Tôi đã cho phép"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(body, `name="client_secret"`) {
		t.Error("the secret is not asked for again while waiting")
	}
}

func itoa(n int64) string {
	return strings.TrimSpace(strings.Join(strings.Fields(time.Unix(n, 0).UTC().Format("20060102150405")), ""))
}

func TestConnectedStorageCanBeEnabledForAnIndividualSite(t *testing.T) {
	state := `{"at":1794167637,"ok":true,"message":"","uploaded":2,"present":7}`
	h, _, c, _ := setupWith(t, cloudAnswers(cloudConnected(true, state)), nil)
	body := do(h, "GET", "/sites/blog/backups", nil, cookie(c)).Body.String()
	for _, want := range []string{`action="/sites/blog/cloud/enable"`, `name="on" value="0"`, `action="/sites/blog/cloud/sync"`, `href="/backups/offsite"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(body, `action="/cloud/disconnect"`) {
		t.Error("site cannot disconnect shared storage")
	}
	h, _, c, _ = setupWith(t, cloudAnswers(cloudConnected(false, "null")), nil)
	body = do(h, "GET", "/sites/blog/backups", nil, cookie(c)).Body.String()
	if !strings.Contains(body, `name="on" value="1"`) || !strings.Contains(body, "Bật chép ra ngoài") {
		t.Error("a site can enable its own copying")
	}
}

func TestAFailedAndARevokedSyncAreExplained(t *testing.T) {
	failed := `{"at":1794167637,"ok":false,"message":"blog/20261101-030000/files.tar.gz: Google Drive: Quota exceeded","uploaded":0,"present":3}`
	h, _, c, _ := setupWith(t, cloudAnswers(cloudConnected(true, failed)), nil)
	body := do(h, "GET", "/sites/blog/backups", nil, cookie(c)).Body.String()
	if !strings.Contains(body, "Thất bại") || !strings.Contains(body, "Quota exceeded") {
		t.Error("a failure shows what Google said")
	}
	revoked := `{"at":1794167637,"ok":false,"message":"cloud-revoked: Google no longer accepts the saved permission","uploaded":0,"present":0}`
	h, _, c, _ = setupWith(t, cloudAnswers(cloudConnected(true, revoked)), nil)
	body = do(h, "GET", "/backups/offsite", nil, cookie(c)).Body.String()
	if !strings.Contains(body, "không còn chấp nhận quyền") || strings.Contains(body, "cloud-revoked") {
		t.Error("a revoked permission is explained in plain words")
	}
}

func TestConnectingSendsTheDetailsToTheHelper(t *testing.T) {
	h, f, c, tok := setupWith(t, cloudAnswers(cloudOff), nil)
	w := followed(h, do(h, "POST", "/cloud/connect", url.Values{"csrf": {tok}, "client_id": {"  " + testClientID + " "}, "client_secret": {testSecret}}, cookie(c)), c)
	req, ok := lastOp(f, "cloud.connect")
	if !ok || req.CloudClientID != testClientID || req.CloudSecret != testSecret {
		t.Fatalf("%+v\n%s", req, w.Body.String())
	}
	if backOf(t, w.Body.String()) != "/backups/offsite" {
		t.Errorf("back: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), testSecret) {
		t.Error("the result page must not repeat the secret")
	}
}

func TestBadClientDetailsNeverReachTheHelper(t *testing.T) {
	h, f, c, tok := setupWith(t, cloudAnswers(cloudOff), nil)
	for _, form := range []url.Values{{"client_id": {""}, "client_secret": {testSecret}}, {"client_id": {testClientID}, "client_secret": {""}},
		{"client_id": {"short"}, "client_secret": {testSecret}}, {"client_id": {testClientID}, "client_secret": {"x; reboot"}}} {
		form.Set("csrf", tok)
		if w := do(h, "POST", "/cloud/connect", form, cookie(c)); w.Code != 400 || !strings.Contains(w.Body.String(), "không đúng dạng") {
			t.Errorf("%v: %d", form, w.Code)
		}
	}
	for _, r := range f.reqs {
		if strings.HasPrefix(r.Op, "cloud.") {
			t.Errorf("a refused request reached the helper: %s", r.Op)
		}
	}
}

func TestEnableSyncAndDisconnectGoToTheHelper(t *testing.T) {
	h, f, c, tok := setupWith(t, cloudAnswers(cloudConnected(false, "null")), nil)
	followed(h, do(h, "POST", "/sites/blog/cloud/enable", url.Values{"csrf": {tok}, "on": {"1"}}, cookie(c)), c)
	if req, ok := lastOp(f, "cloud.enable"); !ok || req.Site != "blog" || !req.CloudOn {
		t.Errorf("enable: %+v", req)
	}
	followed(h, do(h, "POST", "/sites/blog/cloud/enable", url.Values{"csrf": {tok}, "on": {"0"}}, cookie(c)), c)
	if req, _ := lastOp(f, "cloud.enable"); req.CloudOn {
		t.Error("the off button turns it off")
	}
	followed(h, do(h, "POST", "/sites/blog/cloud/sync", url.Values{"csrf": {tok}}, cookie(c)), c)
	if req, ok := lastOp(f, "cloud.sync"); !ok || req.Site != "blog" {
		t.Errorf("sync: %+v", req)
	}
	followed(h, do(h, "POST", "/cloud/finish", url.Values{"csrf": {tok}}, cookie(c)), c)
	followed(h, do(h, "POST", "/cloud/disconnect", url.Values{"csrf": {tok}}, cookie(c)), c)
	for _, op := range []string{"cloud.finish", "cloud.disconnect"} {
		if _, ok := lastOp(f, op); !ok {
			t.Errorf("%s was not sent", op)
		}
	}
}

func TestCloudActionsNeedTheTokenASessionAndARealSite(t *testing.T) {
	h, f, c, tok := setupWith(t, cloudAnswers(cloudOff), nil)
	paths := []string{"connect", "finish", "enable", "sync", "disconnect"}
	for _, p := range paths {
		u := "/sites/blog/cloud/" + p
		if w := do(h, "POST", u, url.Values{"csrf": {"wrong"}}, cookie(c)); w.Code != 403 {
			t.Errorf("%s with a wrong token: %d", p, w.Code)
		}
		if w := do(h, "POST", u, url.Values{"csrf": {tok}}); w.Code != 303 {
			t.Errorf("%s without a session: %d", p, w.Code)
		}
		if w := do(h, "POST", "/sites/nope/cloud/"+p, url.Values{"csrf": {tok}, "client_id": {testClientID}, "client_secret": {testSecret}}, cookie(c)); w.Code != 404 {
			t.Errorf("%s for a site that does not exist: %d", p, w.Code)
		}
	}
	for _, r := range f.reqs {
		if strings.HasPrefix(r.Op, "cloud.") && r.Op != "cloud.status" {
			t.Errorf("a refused request reached the helper: %+v", r)
		}
	}
}

const cloudRemote = `[{"stamp":"20261108-033000","size_mb":412.5,"local":true},{"stamp":"20261107-033000","size_mb":410.2,"local":false}]`

func restoreAnswers(list string) func(ctl.Request) (ctl.Response, error) {
	return byOp(map[string]string{"site.list": blogList, "backup.list": `[]`, "cloud.status": cloudConnected(true, "null"), "cloud.list": list, "cloud.fetch": "started"})
}

func TestThePageOfTheDriveListsCopiesAndOffersOnlyTheOnesNotHere(t *testing.T) {
	h, f, c, _ := setupWith(t, restoreAnswers(cloudRemote), nil)
	body := do(h, "GET", "/sites/blog/cloud/backups", nil, cookie(c)).Body.String()
	for _, want := range []string{"Các bản sao lưu của blog ở nơi lưu trữ ngoài", "412.5 MB", "410.2 MB", "Đã có", "Chưa có", `action="/sites/blog/cloud/fetch"`,
		`name="backup" value="20261107-033000"`, `href="/sites/blog/backups"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(body, `name="backup" value="20261108-033000"`) {
		t.Error("a backup that is on this server needs no download button")
	}
	if req, ok := lastOp(f, "cloud.list"); !ok || req.Site != "blog" {
		t.Errorf("%+v", req)
	}
}

func TestThePageOfTheDriveWhenThereIsNothingOrAnError(t *testing.T) {
	h, _, c, _ := setupWith(t, restoreAnswers(`[]`), nil)
	if body := do(h, "GET", "/sites/blog/cloud/backups", nil, cookie(c)).Body.String(); !strings.Contains(body, "chưa có bản sao lưu nào") {
		t.Error("an empty drive is said plainly")
	}
	h, _, c, _ = setupWith(t, func(r ctl.Request) (ctl.Response, error) {
		if r.Op == "cloud.list" {
			return ctl.Response{Output: "cloud-not-connected: connect to Google Drive first", Error: "exit status 2"}, nil
		}
		return restoreAnswers(`[]`)(r)
	}, nil)
	body := do(h, "GET", "/sites/blog/cloud/backups", nil, cookie(c)).Body.String()
	if !strings.Contains(body, "Chưa kết nối nơi lưu trữ") && !strings.Contains(body, "exit status") {
		t.Errorf("an error must be shown:\n%s", body)
	}
	if w := do(h, "GET", "/sites/nope/cloud/backups", nil, cookie(c)); w.Code != 404 {
		t.Errorf("a site that does not exist: %d", w.Code)
	}
	if w := do(h, "GET", "/sites/blog/cloud/backups", nil); w.Code != 303 {
		t.Errorf("without a session: %d", w.Code)
	}
}

func TestFetchingStartsTheDownloadOfThatBackup(t *testing.T) {
	h, f, c, tok := setupWith(t, restoreAnswers(cloudRemote), nil)
	w := followed(h, do(h, "POST", "/sites/blog/cloud/fetch", url.Values{"csrf": {tok}, "backup": {"20261107-033000"}}, cookie(c)), c)
	if req, ok := lastOp(f, "cloud.fetch"); !ok || req.Site != "blog" || req.Backup != "20261107-033000" {
		t.Fatalf("%+v", req)
	}
	if backOf(t, w.Body.String()) != "/sites/blog/backups" {
		t.Errorf("back: %s", w.Body.String())
	}
}

func TestABadBackupNameNeverReachesTheHelper(t *testing.T) {
	h, f, c, tok := setupWith(t, restoreAnswers(cloudRemote), nil)
	for _, b := range []string{"", "latest", "../../etc", "20261107-033000; reboot", "2026"} {
		if w := do(h, "POST", "/sites/blog/cloud/fetch", url.Values{"csrf": {tok}, "backup": {b}}, cookie(c)); w.Code != 400 {
			t.Errorf("%q: %d", b, w.Code)
		}
	}
	if w := do(h, "POST", "/sites/blog/cloud/fetch", url.Values{"csrf": {"wrong"}, "backup": {"20261107-033000"}}, cookie(c)); w.Code != 403 {
		t.Errorf("wrong token: %d", w.Code)
	}
	if w := do(h, "POST", "/sites/blog/cloud/fetch", url.Values{"backup": {"20261107-033000"}}); w.Code != 303 {
		t.Errorf("no session: %d", w.Code)
	}
	for _, r := range f.reqs {
		if r.Op == "cloud.fetch" {
			t.Errorf("a refused request reached the helper: %+v", r)
		}
	}
}

func TestTheCardShowsADownloadThatRunsFinishedOrFailed(t *testing.T) {
	for state, wants := range map[string][]string{
		`{"stamp":"20261107-033000","at":1794167637,"running":true,"ok":false}`:                                           {"Đang tải bản", `href="/sites/blog/cloud/backups"`},
		`{"stamp":"20261107-033000","at":1794167637,"running":false,"ok":true}`:                                           {"Đã tải bản", "bấm Khôi phục"},
		`{"stamp":"20261107-033000","at":1794167637,"running":false,"ok":false,"message":"cloud-nospace: needs 900 MB"}`:  {"Tải bản", "không đủ chỗ trống"},
		`{"stamp":"20261107-033000","at":1794167637,"running":false,"ok":false,"message":"files.tar.gz arrived damaged"}`: {"bị hỏng"},
		`{"stamp":"20261107-033000","at":1794167637,"running":false,"ok":false,"message":"cloud-interrupted: stopped"}`:   {"bị dừng giữa chừng"},
	} {
		status := `{"connected":true,"email":"me@example.com","pending":null,"enabled":true,"state":null,"fetch":` + state + `}`
		h, _, c, _ := setupWith(t, restoreAnswers(`[]`), nil)
		_ = status
		h, _, c, _ = setupWith(t, byOp(map[string]string{"site.list": blogList, "backup.list": `[]`, "cloud.status": status}), nil)
		body := do(h, "GET", "/sites/blog/backups", nil, cookie(c)).Body.String()
		for _, want := range wants {
			if !strings.Contains(body, want) {
				t.Errorf("%s: missing %q", state, want)
			}
		}
	}
}

func TestKnownRefusalsOfTheCopyAreExplained(t *testing.T) {
	for output, want := range map[string]string{
		"cloud-pending: Google has not been told yet": "Google chưa nhận được sự cho phép",
		"cloud-denied: access was refused at Google":  "từ chối cấp quyền",
		"cloud-expired: the code has expired":         "Mã đã hết hạn",
		"cloud-busy: a copy is already running":       "đang chạy",
		"cloud-not-connected: connect first":          "Chưa kết nối nơi lưu trữ",
		"Google refused the client: invalid_client ":  "Google không nhận Client ID",
	} {
		h, _, c, tok := setupWith(t, func(r ctl.Request) (ctl.Response, error) {
			if r.Op == "cloud.finish" {
				return ctl.Response{Output: output, Error: "exit status 2"}, nil
			}
			return cloudAnswers(cloudOff)(r)
		}, nil)
		body := followed(h, do(h, "POST", "/cloud/finish", url.Values{"csrf": {tok}}, cookie(c)), c).Body.String()
		if !strings.Contains(body, want) {
			t.Errorf("%q: missing %q", output, want)
		}
	}
}

func TestTheCardOffersEveryKindOfStorageWhenNothingIsConnected(t *testing.T) {
	h, _, c, _ := setupWith(t, cloudAnswers(cloudOff), nil)
	body := do(h, "GET", "/backups/offsite", nil, cookie(c)).Body.String()
	for _, want := range []string{`action="/cloud/configure"`, `name="type" value="s3"`, `name="type" value="b2"`, `name="type" value="sftp"`,
		`name="type" value="webdav"`, `name="type" value="ftp"`, `name="v_secret_access_key" type="password"`, `name="v_host_keys"`, `name="bucket"`, "Cloudflare R2"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, not := range []string{`name="v_ssh"`, `name="v_key_file"`, `name="v_md5sum_command"`, `name="v_bearer_token_command"`} {
		if strings.Contains(body, not) {
			t.Errorf("the form must not offer %s", not)
		}
	}
}

func TestConfiguringSendsOnlyTheFieldsOfTheChosenType(t *testing.T) {
	h, f, c, tok := setupWith(t, cloudAnswers(cloudOff), nil)
	form := url.Values{"csrf": {tok}, "type": {"s3"}, "bucket": {"my-backups"}, "folder": {"/srv1/"}, "v_provider": {"Cloudflare"}, "v_access_key_id": {"AKIA123"},
		"v_secret_access_key": {"TOPSECRET"}, "v_endpoint": {"https://x.r2.cloudflarestorage.com"}, "v_shared_credentials_file": {"/root/.aws/credentials"}, "v_ssh": {"id"}}
	w := followed(h, do(h, "POST", "/cloud/configure", form, cookie(c)), c)
	req, ok := lastOp(f, "cloud.configure")
	if !ok || req.CloudStorage == nil {
		t.Fatalf("%+v\n%s", req, w.Body.String())
	}
	s := req.CloudStorage
	if s.Type != "s3" || s.Bucket != "my-backups" || s.Folder != "srv1" || s.Values["secret_access_key"] != "TOPSECRET" {
		t.Errorf("%+v", s)
	}
	for _, k := range []string{"shared_credentials_file", "ssh"} {
		if _, ok := s.Values[k]; ok {
			t.Errorf("%s was passed on", k)
		}
	}
	if strings.Contains(w.Body.String(), "TOPSECRET") {
		t.Error("the result page must not repeat the secret")
	}
}

func TestBadStorageSettingsNeverReachTheHelper(t *testing.T) {
	h, f, c, tok := setupWith(t, cloudAnswers(cloudOff), nil)
	for _, form := range []url.Values{
		{"type": {"local"}},
		{"type": {"s3"}, "bucket": {"b"}, "v_access_key_id": {"a"}, "v_secret_access_key": {"s"}, "v_endpoint": {"https://e.example.com"}},
		{"type": {"sftp"}, "v_host": {"h.example.com"}, "v_user": {"u"}, "v_pass": {"p"}},
		{"type": {"sftp"}, "v_host": {"h.example.com\nssh = id"}, "v_user": {"u"}, "v_pass": {"p"}, "v_host_keys": {"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl"}},
		{"type": {"webdav"}, "v_url": {"file:///etc"}},
	} {
		form.Set("csrf", tok)
		if w := do(h, "POST", "/cloud/configure", form, cookie(c)); w.Code != 400 {
			t.Errorf("%v: %d", form, w.Code)
		}
	}
	if _, ok := lastOp(f, "cloud.configure"); ok {
		t.Error("a refused form reached the helper")
	}
}

func TestAConnectedStorageShowsWhereTheBackupsGo(t *testing.T) {
	status := `{"connected":true,"kind":"s3","where":"S3 Cloudflare: my-backups/mpn-backups","pending":null,"enabled":true,"state":null}`
	h, _, c, _ := setupWith(t, cloudAnswers(status), nil)
	body := do(h, "GET", "/backups/offsite", nil, cookie(c)).Body.String()
	for _, want := range []string{"S3 (Cloudflare R2", "my-backups/mpn-backups", `action="/cloud/disconnect"`, "xóa thông tin đăng nhập"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(body, `action="/cloud/configure"`) {
		t.Error("no form to connect while connected")
	}
}

func TestAFailedTestOfTheStorageIsExplained(t *testing.T) {
	for output, want := range map[string]string{
		"cloud-test-write: AccessDenied":        "không ghi được tệp thử",
		"cloud-test-delete: AccessDenied":       "quyền xóa",
		"cloud-test-connect: dial tcp: timeout": "Không kết nối được",
		"cloud-connected: disconnect first":     "Ngắt kết nối nó trước",
	} {
		h, _, c, tok := setupWith(t, func(r ctl.Request) (ctl.Response, error) {
			if r.Op == "cloud.configure" {
				return ctl.Response{Output: output, Error: "exit status 1"}, nil
			}
			return cloudAnswers(cloudOff)(r)
		}, nil)
		form := url.Values{"csrf": {tok}, "type": {"ftp"}, "v_host": {"ftp.example.com"}, "v_user": {"u"}, "v_pass": {"p"}}
		body := followed(h, do(h, "POST", "/cloud/configure", form, cookie(c)), c).Body.String()
		if !strings.Contains(body, want) {
			t.Errorf("%q: missing %q", output, want)
		}
	}
}

package web

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"mpn/internal/ctl"
	"mpn/internal/site"
	"mpn/internal/webauth"
)

// siteInfo is the answer of "mpn site info": what the overview of a site shows.
type siteInfo struct {
	Name        string            `json:"name"`
	Kind        string            `json:"kind"`
	User        string            `json:"user"`
	Hostnames   []string          `json:"hostnames"`
	Root        string            `json:"root"`
	RootSub     string            `json:"root_sub"`
	RootOutside bool              `json:"root_outside"`
	TLS         bool              `json:"tls"`
	CertExpiry  int64             `json:"cert_expiry"`
	Running     bool              `json:"running"`
	RunDetail   string            `json:"run_detail"`
	DiskBytes   int64             `json:"disk_bytes"`
	DiskCut     bool              `json:"disk_cut"`
	LastBackup  string            `json:"last_backup"`
	PHPVersion  string            `json:"php_version"`
	PHPValues   map[string]string `json:"php_values"`
	PHPSet      map[string]string `json:"php_set"`
	PHPMemory   int               `json:"php_memory_default"`
	Entry       string            `json:"entry"`
	Port        int               `json:"port"`
	MemMB       int               `json:"mem_mb"`
	MiseTask    string            `json:"mise_task"`
	MiseTasks   []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"mise_tasks"`
	MiseError    string   `json:"mise_error"`
	Upstream     string   `json:"upstream"`
	InstalledPHP []string `json:"installed_php"`
	Nginx        string   `json:"nginx"`
}

// phpField is one PHP setting as the form shows it.
type phpField struct {
	Name, Value, Default, Hint string
	Common, Changed            bool
	Choices                    []string
}

// overview is what the overview tab draws, worked out from the answer of the helper.
type overview struct {
	siteInfo
	DiskText, CertText, BackupText string
	CertSoon                       bool // fewer than 14 days left
	MiseTaskMissing                bool // the saved task is no longer present in mise.toml
	PHPVersions                    []string
	Fields                         []phpField
	More                           []phpField // settings that are not one of the six common ones
	Home                           string     // the folder every web root lies in
}

func humanBytes(n int64) string { return humanSize(n) }
func (s *Server) loadOverview(row siteRow) (*overview,
	string) {
	return s.loadOverviewText(T, row)
}

func (s *Server) loadOverviewText(T translator, row siteRow) (*overview, string) {
	var in siteInfo
	if e := s.listText(T, ctl.Request{Op: "site.info", Site: row.Name}, &in); e != "" {
		return nil, e
	}
	o := &overview{siteInfo: in, Home: site.HtdocsOf(row.User) + "/"}
	if in.MiseTask != "" {
		found := false
		for _, task := range in.MiseTasks {
			if task.Name == in.MiseTask {
				found = true
				break
			}
		}
		o.MiseTaskMissing = !found
	}
	if in.DiskCut {
		o.DiskText = T("ov.disk.big")
	} else {
		o.DiskText = humanBytes(in.DiskBytes)
	}
	if in.CertExpiry > 0 {
		exp := time.Unix(in.CertExpiry, 0)
		days := int(time.Until(exp).Hours() / 24)
		o.CertText, o.CertSoon = T("ov.cert.until", exp.Local().Format("02/01/2006"), days), days < 14
	}
	if in.LastBackup != "" {
		o.BackupText = stampText(in.LastBackup)
	}
	// Only versions that are installed are offered, and the one in use is always among them.
	o.PHPVersions = withCurrent(in.InstalledPHP, in.PHPVersion)
	if row.Kind == string(site.PHP) {
		for _, d := range site.PHPSettings(strconv.Itoa(in.PHPMemory) + "M") {
			f := phpField{Name: d.Name, Default: d.Default, Hint: hintFor(T, d.Name, d.Hint), Common: d.Common, Choices: d.Choices, Value: in.PHPValues[d.Name]}
			_, f.Changed = in.PHPSet[d.Name]
			if f.Common {
				o.Fields = append(o.Fields, f)
			} else {
				o.More = append(o.More, f)
			}
		}
	}
	return o, ""
}

// hintFor is the description of a PHP setting in the request's language; the English one of the site package is the
// fallback.
func hintFor(T translator, name, fallback string) string {
	if s := T("ph." + name); s != "ph."+name {
		return s
	}
	return fallback
}

func withCurrent(installed []string, cur string) []string {
	out := slices.Clone(installed)
	if cur != "" && !slices.Contains(out, cur) {
		out = append(out, cur)
	}
	return out
}

// settingsRequest reads the settings form of a site into a request for the helper. The kind comes from the stored site, never
// from the form. Every value is checked here too, so that a mistake is explained in Vietnamese next to its name.
func (s *Server) settingsRequest(r *http.Request, row siteRow, info siteInfo) (ctl.Request, string) {
	T := translatorForRequest(r)
	req := ctl.Request{Op: "site.set", Site: row.Name, Kind: row.Kind}
	get := func(k string) string { return strings.TrimSpace(r.PostFormValue(k)) }
	kind := site.Kind(row.Kind)
	if kind != site.Proxy && kind != site.Mise {
		if info.RootOutside {
			return req, T("set.err.rootoutside")
		}
		req.RootSub = strings.Trim(get("root_sub"), "/")
		if !site.ValidSubdir(req.RootSub) {
			return req, T("set.err.root")
		}
	}
	switch kind {
	case site.PHP:
		req.PHP = get("php")
		if !site.ValidPHPVersion(req.PHP) || !slices.Contains(withCurrent(info.InstalledPHP, info.PHPVersion), req.PHP) {
			return req, T("set.err.php", req.PHP)
		}
		req.PHPIni = map[string]string{}
		for _, d := range site.PHPSettings(strconv.Itoa(info.PHPMemory) + "M") {
			v := get("ini_" + d.Name)
			if v == "" || v == d.Default {
				continue // what the machine does anyway is not stored
			}
			if hint, ok := site.CheckPHPSetting(d.Name, v); !ok {
				return req, T("set.err.ini", d.Name, v, hintFor(T, d.Name, hint))
			}
			req.PHPIni[d.Name] = v
		}
		if site.UploadLargerThanPost(req.PHPIni) {
			return req, T("set.err.upload")
		}
	case site.Binary:
		req.Entry = get("entry")
		if !site.ValidBinaryEntry(req.Entry) {
			return req, T("set.err.entry")
		}
		var err error
		if req.Port, err = appPortField(get("port")); err != nil || req.Port == 0 {
			return req, T("set.err.port")
		}
		if m := get("mem"); m != "" {
			if req.Mem, err = strconv.Atoi(m); err != nil || !site.ValidMemMB(req.Mem) {
				return req, T("set.err.mem")
			}
		}
	case site.Mise:
		// Mise task changes have their own form and route. An old or crafted generic settings request must never
		// treat a missing mise_task field as a request to stop the running service.
		req.MiseTask = info.MiseTask
		req.Port, req.Mem = info.Port, info.MemMB
	case site.Proxy:
		req.Upstream = get("upstream")
		if !site.ValidUpstream(req.Upstream) {
			return req, T("set.err.upstream")
		}
	}
	return req, ""
}

// appPortField reads the port of an app from the form: empty means a Unix socket (0), a number must be a port an app may use.
func appPortField(v string) (int, error) {
	if strings.TrimSpace(v) == "" {
		return 0, nil
	}
	p, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || !site.ValidAppPort(p) {
		return 0, fmt.Errorf("invalid port %q", v)
	}
	return p, nil
}

func (s *Server) saveSettings(w http.ResponseWriter, r *http.Request, ss webauth.Session) {
	T := translatorForWriter(w)
	row, status, msg := s.findSiteText(T, r.PathValue("name"))
	if status != 0 {
		http.Error(w, msg, status)
		return
	}
	back := siteTab(row.Name, "")
	o, e := s.loadOverviewText(T, row)
	if e != "" {
		s.refuseTo(w, ss, back, T("set.fail"), e)
		return
	}
	req, problem := s.settingsRequest(r, row, o.siteInfo)
	if problem != "" {
		s.refuseTo(w, ss, back, T("set.fail"), problem)
		return
	}
	res := s.runText(r, T, T("act.site.set", row.Name), req)
	res.Back = back
	if !res.OK {
		switch {
		case strings.Contains(res.Output, "is already used by site"):
			res.Error = T("set.err.portused")
		case strings.Contains(res.Output, "is not installed on this machine"):
			res.Error = T("set.err.notinstalled")
		}
		res.Error = fmt.Sprintf("%s %s", res.Error, T("set.restored"))
	}
	s.finish(w, r, res)
}

// saveMiseTask changes only the supervised task. The current port and memory setting come from site.info and are
// carried through site.set unchanged so selecting a task cannot reset the other Mise service fields.
func (s *Server) saveMiseTask(w http.ResponseWriter, r *http.Request, ss webauth.Session) {
	T := translatorForWriter(w)
	row, status, msg := s.findSiteText(T, r.PathValue("name"))
	if status != 0 {
		http.Error(w, msg, status)
		return
	}
	if row.Kind != string(site.Mise) {
		http.Error(w, T("err.nosite"), http.StatusNotFound)
		return
	}
	var info siteInfo
	if problem := s.listText(T, ctl.Request{Op: "site.info", Site: row.Name}, &info); problem != "" {
		s.refuseTo(w, ss, siteTab(row.Name, ""), T("set.fail"), problem)
		return
	}
	task := strings.TrimSpace(r.PostFormValue("mise_task"))
	if task != "" {
		if !site.ValidMiseTask(task) || info.MiseError != "" {
			s.refuseTo(w, ss, siteTab(row.Name, ""), T("set.fail"), T("set.err.misetask"))
			return
		}
		found := false
		for _, item := range info.MiseTasks {
			if item.Name == task {
				found = true
				break
			}
		}
		if !found {
			s.refuseTo(w, ss, siteTab(row.Name, ""), T("set.fail"), T("set.err.misetask"))
			return
		}
	}
	res := s.runText(r, T, T("act.mise.task", row.Name), ctl.Request{
		Op: "site.set", Site: row.Name, Kind: row.Kind, Port: info.Port, Mem: info.MemMB, MiseTask: task,
	})
	res.Back = siteTab(row.Name, "")
	if !res.OK {
		res.Error = fmt.Sprintf("%s %s", res.Error, T("set.restored"))
	}
	s.finish(w, r, res)
}

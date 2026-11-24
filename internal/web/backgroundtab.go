package web

import (
	"net/http"
	"strconv"
	"strings"

	"mpn/internal/bgservice"
	"mpn/internal/ctl"
	"mpn/internal/webauth"
)

type backgroundView struct {
	Services    []bgservice.Service
	Error, Logs string
	LogID       int64
	CanManage   bool
	Kind        string
}

func (s *Server) loadBackground(T translator, row siteRow) *backgroundView {
	v := &backgroundView{CanManage: true, Kind: row.Kind}
	v.Error = s.listText(T, ctl.Request{Op: "site.bglist", Site: row.Name}, &v.Services)
	return v
}

func (s *Server) backgroundPost(w http.ResponseWriter, r *http.Request, ss webauth.Session) {
	T := translatorForWriter(w)
	row, status, msg := s.findSiteText(T, r.PathValue("name"))
	if status != 0 {
		http.Error(w, msg, status)
		return
	}
	action := r.PathValue("action")
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	req := ctl.Request{Site: row.Name, BackgroundID: id, PHP: row.PHPVersion()}
	title := "act.bg." + action
	switch action {
	case "":
		action = r.PostFormValue("action")
		title = "act.bg." + action
		if action == "add" {
			req.Op = "site.bgadd"
		} else if action == "edit" {
			req.Op = "site.bgedit"
			req.BackgroundID, _ = strconv.ParseInt(r.PostFormValue("id"), 10, 64)
		} else {
			http.Error(w, "invalid operation", http.StatusBadRequest)
			return
		}
		req.BackgroundName = strings.TrimSpace(r.PostFormValue("name"))
		req.BackgroundKind = r.PostFormValue("kind")
		req.BackgroundCommand = strings.TrimSpace(r.PostFormValue("command"))
		req.BackgroundTask = strings.TrimSpace(r.PostFormValue("task"))
		req.BackgroundMemory, _ = strconv.Atoi(r.PostFormValue("memory"))
	case "start", "stop", "restart", "delete":
		req.Op = map[string]string{"start": "site.bgstart", "stop": "site.bgstop", "restart": "site.bgrestart", "delete": "site.bgdel"}[action]
	default:
		http.Error(w, "invalid operation", http.StatusBadRequest)
		return
	}
	res := s.runText(r, T, T(title, row.Name), req)
	res.Back = siteTab(row.Name, "background")
	s.finish(w, r, res)
}

// wscheck drives the panel's web terminal the way a browser does, over real HTTPS and a real WebSocket, and prints what
// it found. It is a test tool for a server you own, not part of the panel.
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/coder/websocket"
)

var passed, failed int

func check(ok bool, what string, detail ...any) {
	if ok {
		passed++
		fmt.Println("  ok  ", what)
		return
	}
	failed++
	fmt.Println("  FAIL", what, fmt.Sprint(detail...))
}

func main() {
	base := flag.String("base", "https://127.0.0.1:2610", "address of the panel")
	pass := flag.String("pass", "", "password of the admin")
	site := flag.String("site", "", "website whose terminal to open")
	account := flag.String("account", "", "account name the terminal should be logged in as")
	flag.Parse()
	u, _ := url.Parse(*base)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second,
		Transport:     &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	origin := u.Scheme + "://" + u.Host

	req, _ := http.NewRequest("POST", *base+"/login", strings.NewReader(url.Values{"user": {"admin"}, "password": {*pass}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", origin)
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != 303 {
		fmt.Println("login failed:", err, resp)
		os.Exit(2)
	}
	page, _ := client.Get(*base + "/sites/" + *site + "/terminal")
	body, _ := io.ReadAll(page.Body)
	page.Body.Close()
	csp := page.Header.Get("Content-Security-Policy")
	m := regexp.MustCompile(`data-csrf="([^"]+)"`).FindStringSubmatch(string(body))
	check(page.StatusCode == 200 && m != nil, "the terminal page opens and carries a form token", page.StatusCode)
	check(strings.Contains(csp, "connect-src 'self'") && regexp.MustCompile(`style-src 'self' 'nonce-[A-Za-z0-9_-]+'`).MatchString(csp), "the page has its own policy with a nonce", csp)
	check(strings.Contains(string(body), "xterm.js?v=") && strings.Contains(string(body), "terminal.js?v="), "the page loads xterm.js and terminal.js")
	if m == nil {
		os.Exit(1)
	}
	ws := strings.Replace(*base, "http", "ws", 1) + "/sites/" + *site + "/terminal/ws?csrf=" + url.QueryEscape(m[1]) + "&cols=100&rows=30"
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// refused first
	_, r1, e1 := websocket.Dial(ctx, ws, &websocket.DialOptions{HTTPClient: client, HTTPHeader: http.Header{"Origin": {"https://evil.example"}}})
	check(e1 != nil && r1 != nil && r1.StatusCode == 403, "another Origin is refused", e1)
	_, r2, e2 := websocket.Dial(ctx, strings.Split(ws, "?")[0]+"?csrf=wrong", &websocket.DialOptions{HTTPClient: client, HTTPHeader: http.Header{"Origin": {origin}}})
	check(e2 != nil && r2 != nil && r2.StatusCode == 403, "a wrong form token is refused", e2)

	c, _, err := websocket.Dial(ctx, ws, &websocket.DialOptions{HTTPClient: client, HTTPHeader: http.Header{"Origin": {origin}}})
	if err != nil {
		check(false, "the WebSocket opens through nginx", err)
		os.Exit(1)
	}
	defer c.CloseNow()
	check(true, "the WebSocket opens through nginx")
	var seen strings.Builder
	waitFor := func(what, want string) bool {
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			if strings.Contains(seen.String(), want) {
				return true
			}
			rc, cancel2 := context.WithTimeout(ctx, 2*time.Second)
			_, data, err := c.Read(rc)
			cancel2()
			if err == nil {
				seen.Write(data)
			} else if rc.Err() == nil {
				break
			}
		}
		check(false, what, fmt.Sprintf("never saw %q in %q", want, tail(seen.String())))
		return false
	}
	send := func(s string) { _ = c.Write(ctx, websocket.MessageBinary, []byte(s)) }

	if waitFor("the login banner appears", "MCPN") {
		time.Sleep(300 * time.Millisecond)
		check(!strings.Contains(seen.String(), "Welcome to") && !strings.Contains(seen.String(), "Last login"), "no system welcome text and no \"Last login\"")
		check(true, "the login banner appears")
	}
	send("echo MARK-$((6*7))\n")
	if waitFor("a command runs", "MARK-42\r\n") {
		check(true, "a typed command runs and its output comes back")
	}
	send("id -un; pwd\n")
	if waitFor("the shell belongs to the website's account", *account+"\r\n/home/"+*account) {
		check(true, "the shell is the website's own account, in its home")
	}
	send("stty size\n")
	if waitFor("the size asked for is used", "30 100\r\n") {
		check(true, "the terminal has the size the browser asked for (30x100)")
	}
	_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"resize","cols":120,"rows":40}`))
	time.Sleep(400 * time.Millisecond)
	send("stty size\n")
	if waitFor("a resize reaches the shell", "40 120\r\n") {
		check(true, "a resize from the browser reaches the shell (40x120)")
	}
	send("exit\n")
	var after strings.Builder
	var lastErr error
	start := time.Now()
	for time.Now().Before(start.Add(15 * time.Second)) {
		rc, cancel2 := context.WithTimeout(ctx, 4*time.Second)
		_, data, err := c.Read(rc)
		cancel2()
		if err == nil {
			after.Write(data)
			continue
		}
		lastErr = err
		break
	}
	// A read that ran out of time would also end the loop, so the way it ended has to be looked at, not only that it ended.
	check(websocket.CloseStatus(lastErr) == websocket.StatusNormalClosure, "the socket is closed normally by the panel when the shell exits",
		fmt.Sprintf("error=%v status=%d after %v; received after exit: %q", lastErr, websocket.CloseStatus(lastErr), time.Since(start).Round(time.Millisecond), tail(after.String())))
	fmt.Printf("RESULT pass=%d fail=%d\n", passed, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

func tail(s string) string {
	if len(s) > 200 {
		return "..." + s[len(s)-200:]
	}
	return s
}

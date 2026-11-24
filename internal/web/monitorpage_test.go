package web

import (
	"encoding/json"
	"errors"
	"html"
	"math"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"mpn/internal/monitor"
)

func noProc(string) ([]byte, error) { return nil, errors.New("not on this machine") }

// demoCollector holds a day of invented measurements: a daily rhythm in the processor, memory that creeps up, a quiet gap while
// the panel was restarted, and traffic in bursts.
func demoCollector(now time.Time) *monitor.Collector {
	c := monitor.New(noProc, "")
	c.SetMachine(monitor.Machine{Cores: 2, MemTotalMB: 961, SwapTotalMB: 1023})
	for i := 24 * 120; i >= 0; i-- { // every 30 seconds for a day
		at := now.Add(-time.Duration(i) * 30 * time.Second)
		if i < 24*120-8*120 && i > 24*120-8*120-30 { // a gap of 15 minutes
			continue
		}
		h := float64(at.Hour()) + float64(at.Minute())/60
		cpu := 8 + 6*math.Sin(h/24*2*math.Pi) + float64(i%7)
		burst := 0.0
		if i%97 < 6 {
			burst = 90000
		}
		c.Add(monitor.Sample{T: at.Unix(), CPU: math.Max(cpu, 1), Load: 0.2 + cpu/60, MemMB: 380 + float64(24*120-i)/40, SwapMB: 90,
			Rx: 2500 + burst + float64(i%13)*300, Tx: 1200 + burst/3 + float64(i%11)*150})
	}
	return c
}

func fakeUnits(unit string) (string, string) {
	switch unit {
	case "nginx.service", "cron.service", "ssh.service", "mpn-web.service", "mpn-root.service", "php8.4-fpm.service":
		return unit, "active"
	case "sshd.service":
		return "ssh.service", "active" // another name of the same unit
	case "mariadb.service":
		return unit, "failed"
	case "mysql.service":
		return "mariadb.service", "failed" // another name of the same unit
	case "php8.3-fpm.service":
		return unit, "inactive"
	}
	return "", "" // the firewall and every PHP that is not installed
}

// monitored gives a server a day of invented measurements and a systemd that answers from a fixed list.
func monitored(s *Server) {
	s.Mon, s.UnitState, s.InstalledPHP = demoCollector(time.Now()), fakeUnits, nil
}

func TestTheMonitorPageShowsTheMachineChartsServicesAndATable(t *testing.T) {
	h, _, c, _ := setupWith(t, byOp(nil), monitored)
	body := do(h, "GET", "/monitor", nil, cookie(c)).Body.String()
	for _, want := range []string{"Giám sát máy chủ", `href="/monitor" class="nav-link" aria-current="page"`, "Bộ xử lý", "Bộ nhớ", "Tải trung bình", "Thời gian chạy", "Mạng",
		"2 nhân", "Nhận", "Gửi", "nginx", "MariaDB", "Đang chạy", "Lỗi", "PHP-FPM 8.4", "Xem dạng bảng", "monitor.js?v=", `class="viz-meter"`, `role="img"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if n := strings.Count(body, `data-chart="`); n != 4 {
		t.Errorf("four charts: %d", n)
	}
	services := body[strings.LastIndex(body, ">Dịch vụ<"):] // the menu has a link of that name too, before the page
	if end := strings.Index(services, "</section>"); end > 0 {
		services = services[:end]
	}
	if strings.Contains(services, "MySQL") || strings.Contains(services, "Tường lửa") {
		t.Errorf("a service that does not exist on the machine is left out, and one reached by two names is shown once:\n%s", services)
	}
	if strings.Count(services, ">SSH<") != 1 || strings.Count(services, ">MariaDB<") != 1 {
		t.Error("each service once")
	}
	if !strings.Contains(services, "MCPN (trình hỗ trợ)") || strings.Contains(services, "svc.unit.") {
		t.Errorf("the panel's own services are named in the reader's language:\n%s", services)
	}
	en := do(h, "GET", "/monitor", nil, cookie(c), cookie(&http.Cookie{Name: "mpn_language", Value: "en"})).Body.String()
	if !strings.Contains(en, "MCPN (root helper)") || !strings.Contains(en, "MCPN (web interface)") || strings.Contains(en, "trình hỗ trợ") {
		t.Error("the panel's own services are not translated to English")
	}
	if strings.Count(body, "viz-key viz-key-2") < 1 || strings.Count(body, `<ul class="flex gap-4`) != 1 {
		t.Error("only the chart with two series has a legend")
	}
	if strings.Contains(body, ` style="`) {
		t.Error("the page's policy forbids a style attribute")
	}
}

func TestEveryChartCarriesItsDataInAnAttributeThatParses(t *testing.T) {
	h, _, c, _ := setupWith(t, byOp(nil), monitored)
	body := do(h, "GET", "/monitor?range=24h", nil, cookie(c)).Body.String()
	m := regexp.MustCompile(`data-chart="([^"]*)"`).FindAllStringSubmatch(body, -1)
	if len(m) != 4 {
		t.Fatalf("%d charts", len(m))
	}
	for i, x := range m {
		var d chartJSON
		if err := json.Unmarshal([]byte(html.UnescapeString(x[1])), &d); err != nil {
			t.Fatalf("chart %d: %v", i, err)
		}
		if len(d.T) < 20 || len(d.T) != len(d.X) || len(d.S) == 0 || len(d.S[0].V) != len(d.T) {
			t.Errorf("chart %d: %d times, %d positions, %d series", i, len(d.T), len(d.X), len(d.S))
		}
		for j := 1; j < len(d.X); j++ {
			if d.X[j] < d.X[j-1] || d.X[j] < padL-1 || d.X[j] > chartW-padR+1 {
				t.Errorf("chart %d: point %d is at %v, outside the plot or out of order", i, j, d.X[j])
				break
			}
		}
	}
}

func TestTheRangeIsChosenByTheAddressAndAnUnknownOneIsTheDefault(t *testing.T) {
	h, _, c, _ := setupWith(t, byOp(nil), monitored)
	for q, active := range map[string]string{"": "1 giờ", "?range=6h": "6 giờ", "?range=24h": "24 giờ", "?range=evil": "1 giờ", "?range=1h%22%3E": "1 giờ"} {
		body := do(h, "GET", "/monitor"+q, nil, cookie(c)).Body.String()
		re := regexp.MustCompile(`aria-current="true">([^<]*)<`)
		if got := re.FindStringSubmatch(body); got == nil || got[1] != active {
			t.Errorf("%q: %v want %q", q, got, active)
		}
	}
}

func TestWithoutMeasurementsThePageSaysSoAndStillWorks(t *testing.T) {
	h, _, c, _ := setupWith(t, byOp(nil), nil)
	if w := do(h, "GET", "/monitor", nil, cookie(c)); w.Code != 200 || !strings.Contains(w.Body.String(), "Chưa có số liệu") {
		t.Errorf("no collector: %d", w.Code)
	}
	h, _, c, _ = setupWith(t, byOp(nil), func(s *Server) { s.Mon = monitor.New(noProc, ""); s.UnitState = fakeUnits })
	if body := do(h, "GET", "/monitor", nil, cookie(c)).Body.String(); !strings.Contains(body, "Chưa có số liệu") || !strings.Contains(body, "nginx") {
		t.Error("a collector that has not measured yet: the page says so, and the services still show")
	}
	if w := do(h, "GET", "/monitor", nil); w.Code != 303 {
		t.Errorf("a page that needs a login: %d", w.Code)
	}
}

func TestAChartIsDrawnFromZeroWithGapsAndNoMarkup(t *testing.T) {
	from := time.Unix(1_000_000, 0)
	to := from.Add(time.Hour)
	times := []int64{from.Unix() + 60, from.Unix() + 120, from.Unix() + 180, from.Unix() + 2400, from.Unix() + 2460}
	svg := string(renderChart(chartSpec{ID: "x", Label: `a "quoted" <b>label</b>`, From: from, To: to, Times: times, MinMax: 10, Cap: 100, Format: fmtPercent,
		Series: []chartSeries{{Name: "<script>alert(1)</script>", Class: "1", Values: []float64{5, 50, 20, 30, 120}}}}))
	if strings.Contains(svg, "<script>") || strings.Contains(svg, "<b>label") {
		t.Errorf("a text must not become markup:\n%s", svg)
	}
	if strings.Count(svg, `class="viz-line viz-line-1" d="M`) != 1 || strings.Count(svg, "M") < 2 {
		t.Error("one line")
	}
	line := regexp.MustCompile(`class="viz-line viz-line-1" d="([^"]*)"`).FindStringSubmatch(svg)
	if line == nil || strings.Count(line[1], "M") != 2 {
		t.Errorf("the quiet stretch of 37 minutes breaks the line in two: %v", line)
	}
	for _, want := range []string{">0%<", ">50%<", ">100%<", `class="viz-dot viz-dot-1"`, `class="viz-area viz-area-1"`, "hidden"} {
		if !strings.Contains(svg, want) {
			t.Errorf("missing %q", want)
		}
	}
	two := string(renderChart(chartSpec{From: from, To: to, Times: times[:2], Format: fmtRate, MinMax: 1024,
		Series: []chartSeries{{Name: "a", Class: "1", Values: []float64{1, 2}}, {Name: "b", Class: "2", Values: []float64{3, 4}}}}))
	if strings.Contains(two, "viz-area") {
		t.Error("an area under two lines hides one of them: only a lone series gets a wash")
	}
}

func TestAxisAndNumberFormats(t *testing.T) {
	for in, want := range map[float64]float64{0: 1, 0.7: 1, 1.2: 2, 2.3: 5, 3: 5, 7: 10, 11: 20, 480: 500, 961: 1000, 5000: 5000} {
		if got := niceMax(in); got != want {
			t.Errorf("niceMax(%v) = %v, want %v", in, got, want)
		}
	}
	s := chartSpec{MinMax: 10, Cap: 100, Series: []chartSeries{{Values: []float64{3, 250}}}}
	if s.axisMax() != 100 {
		t.Error("a percentage axis stops at 100")
	}
	for peak, want := range map[float64]float64{98_000: 100 * 1024, 90_000: 100 * 1024, 3000: 5 * 1024, 700: 1024, 5 << 20: 5 << 20, 1.3e6: 2 << 20} {
		if got := (chartSpec{MinMax: 1024, Binary: true, Series: []chartSeries{{Values: []float64{peak}}}}).axisMax(); got != want {
			t.Errorf("a network axis for %v B/s ends at %v, want %v", peak, got, want)
		}
	}
	if (chartSpec{Fixed: 961}).axisMax() != 961 {
		t.Error("memory is drawn against the size of the machine")
	}
	for got, want := range map[string]string{fmtPercent(0): "0%", fmtPercent(4.26): "4.3%", fmtPercent(37.4): "37%", fmtMB(512): "512 MB", fmtMB(1536): "1.5 GB", fmtLoad(0.456): "0.46", fmtLoad(12.4): "12",
		fmtRate(500): "500 B/s", fmtRate(2048): "2 KB/s", fmtRate(5 << 20): "5.0 MB/s"} {
		if got != want {
			t.Errorf("%q, want %q", got, want)
		}
	}
	for pct, want := range map[float64]string{0: "ok", 74.9: "ok", 75: "warn", 89: "warn", 90: "crit", 100: "crit"} {
		if got := level(pct); got != want {
			t.Errorf("level(%v) = %s, want %s", pct, got, want)
		}
	}
}

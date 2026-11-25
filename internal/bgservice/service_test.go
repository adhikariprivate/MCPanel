package bgservice

import (
	"strings"
	"testing"

	"mpn/internal/site"
)

func TestArgsAllowsQuotedArgumentsButNoShell(t *testing.T) {
	got, err := Args(`artisan queue:work --queue="mail queue" --tries=3`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "|") != "artisan|queue:work|--queue=mail queue|--tries=3" {
		t.Fatalf("args = %#v", got)
	}
	for _, bad := range []string{"worker; id", "${HOME}/worker", "worker | cat", "worker\nnext", "worker 'unfinished"} {
		if _, err := Args(bad); err == nil {
			t.Errorf("accepted unsafe command %q", bad)
		}
	}
}

func TestUnitNamesAndRenderingAreSiteScoped(t *testing.T) {
	s := site.Site{Name: "shop", Kind: site.PHP, User: "shop", Root: "/home/shop/htdocs", Hostnames: []string{"shop.example.com"}, PHPVersion: "8.4"}
	v := Service{ID: 17, Site: "shop", Name: "Queue worker", Kind: PHP, Command: `artisan queue:work --queue="mail queue"`, MemoryMB: 256}
	text, err := Render(s, v)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"app-shop-bg-17.service", "User=shop", "WorkingDirectory=/home/shop/htdocs", "ExecStart=\"/usr/bin/php8.4\" \"artisan\" \"queue:work\" \"--queue=mail queue\"", "MemoryMax=256M", "NoNewPrivileges=yes", "PrivateTmp=yes"} {
		if want == "app-shop-bg-17.service" {
			if UnitName(s.Name, v.ID) != want {
				t.Errorf("unit name = %q", UnitName(s.Name, v.ID))
			}
			continue
		}
		if !strings.Contains(text, want) {
			t.Errorf("unit does not contain %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "PORT=") {
		t.Fatal("background process got web PORT")
	}
}

func TestBinaryMustStayRelativeAndMiseRequiresTask(t *testing.T) {
	for _, cmd := range []string{"/usr/bin/worker", "../outside", "bin/../outside"} {
		if err := Valid(Service{Name: "worker", Kind: Binary, Command: cmd}); err == nil {
			t.Errorf("accepted binary path %q", cmd)
		}
	}
	if err := Valid(Service{Name: "worker", Kind: Mise, Task: "bad task"}); err == nil {
		t.Fatal("accepted invalid mise task")
	}
}

func TestBinaryAndMiseUnitsAreSeparateFromWebUnits(t *testing.T) {
	binarySite := site.Site{Name: "workerbin", Kind: site.Binary, User: "workerbin", Root: "/home/workerbin/app", Hostnames: []string{"worker.example.com"}, AppPort: 24001, Entry: "bin/server"}
	binary, err := Render(binarySite, Service{ID: 2, Name: "binary worker", Kind: Binary, Command: "bin/worker --queue default"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(binary, `ExecCondition=/usr/bin/test -x "/home/workerbin/bin/worker"`) || strings.Contains(binary, "PORT=") {
		t.Fatalf("unexpected binary unit:\n%s", binary)
	}
	s := site.Site{Name: "worker", Kind: site.Mise, User: "worker", Root: "/home/worker/htdocs", Hostnames: []string{"worker-mise.example.com"}, AppPort: 24002}
	mise, err := Render(s, Service{ID: 3, Name: "mise worker", Kind: Mise, Task: "queue:work"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ExecStartPre=+/usr/local/bin/mpn site mise-install -name worker", `ExecStart="/usr/local/bin/mise" "run" "queue:work"`, "MISE_TRUSTED_CONFIG_PATHS=/home/worker/htdocs"} {
		if !strings.Contains(mise, want) {
			t.Errorf("mise unit missing %q:\n%s", want, mise)
		}
	}
	if _, err := Render(s, Service{ID: 4, Name: "wrong type", Kind: Binary, Command: "bin/worker"}); err == nil {
		t.Fatal("rendered Binary worker for mise site")
	}
}

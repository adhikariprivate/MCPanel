package installer

import (
	"strings"
	"testing"
)

func TestRenewalTimerIsInstalledWithTheBinary(t *testing.T) {
	o := opts("", 1024)
	o.SelfPath = "/root/mpn"
	p, err := Build(o)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := names(p), "base packages,journald limit,nginx,nginx pagespeed,mise,renewal and backup timers"; got != want {
		t.Errorf("steps=%s want %s", got, want)
	}
	out := p.String()
	for _, want := range []string{
		"run   install -m 0755 /root/mpn /usr/local/bin/mpn",
		"write /etc/systemd/system/mpn-renew.service", "write /etc/systemd/system/mpn-renew.timer",
		"run   systemctl enable --now mpn-renew.timer",
		"write /etc/systemd/system/mpn-backup.timer", "run   systemctl enable --now mpn-backup.timer",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(renewService, "ExecStart=/usr/local/bin/mpn site ssl-renew -apply") {
		t.Error("the service must run the renewal command")
	}
	if !strings.Contains(renewTimer, "Persistent=true") || !strings.Contains(renewTimer, "OnCalendar=daily") {
		t.Error("the timer must run daily and catch up after downtime")
	}
}

func TestBackupTimerRunsNightlyAtLowPriority(t *testing.T) {
	if !strings.Contains(backupService, "ExecStart=/usr/local/bin/mpn backup create -all -keep 7 -apply") {
		t.Error("the service must back up every site and keep 7")
	}
	for _, want := range []string{"Nice=10", "IOSchedulingClass=idle"} {
		if !strings.Contains(backupService, want) {
			t.Errorf("missing %s", want)
		}
	}
	if !strings.Contains(backupTimer, "Persistent=true") || !strings.Contains(backupTimer, "03:30:00") {
		t.Error("the timer must run at night and catch up after downtime")
	}
}

func TestBinaryAlreadyInPlaceIsNotCopiedOntoItself(t *testing.T) {
	o := opts("", 1024)
	o.SelfPath = mpnBinary
	p, _ := Build(o)
	if strings.Contains(p.String(), "run   install -m 0755") {
		t.Errorf("install of a file onto itself would fail:\n%s", p)
	}
}

func TestNoTimerWithoutSelfPath(t *testing.T) {
	p, _ := Build(opts("", 1024))
	if strings.Contains(names(p), "renewal") {
		t.Errorf("unexpected step: %s", names(p))
	}
}

func TestCatchAllHTTPSServerUsesSelfSignedCertificate(t *testing.T) {
	p, _ := Build(opts("", 1024))
	out := p.String()
	if !strings.Contains(defaultServer, "listen 443 ssl default_server;") || !strings.Contains(defaultServer, "/etc/mpn/certs/_default/fullchain.pem") {
		t.Errorf("default server lacks the HTTPS block:\n%s", defaultServer)
	}
	if !strings.Contains(defaultCertScript, "openssl req -x509") {
		t.Error("certificate script must create a self-signed certificate")
	}
	// The certificate must exist before nginx -t reads the config that points to it.
	if strings.Index(out, "run   sh -c d=/etc/mpn/certs/_default") > strings.Index(out, "run   nginx -t") {
		t.Errorf("certificate must be created before nginx -t:\n%s", out)
	}
	if !strings.Contains(out, "install -y ca-certificates curl xz-utils openssl") {
		t.Errorf("openssl must be installed first:\n%s", out)
	}
}

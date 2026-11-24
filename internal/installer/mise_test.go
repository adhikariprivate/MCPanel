package installer

import (
	"strings"
	"testing"
)

func TestMiseAssetPinsSupportedArchitectures(t *testing.T) {
	for _, tc := range []struct{ arch, asset, sum string }{
		{"amd64", "mise-v2026.11.13-linux-x64.tar.gz", "22a2c32160a152d170261586dc124644d2ac2ab36c0afb48ef2b5d275c485a60"},
		{"arm64", "mise-v2026.11.13-linux-arm64.tar.gz", "31361f75c9b4ad38bfd4f8ca1a9e1cd8e8f40c5cdba432cc552adb48ac21a683"},
	} {
		asset, sum, err := miseAsset(tc.arch)
		if err != nil || asset != tc.asset || sum != tc.sum {
			t.Errorf("miseAsset(%q) = %q, %q, %v; want %q, %q", tc.arch, asset, sum, err, tc.asset, tc.sum)
		}
	}
	if _, _, err := miseAsset("386"); err == nil {
		t.Error("unsupported architecture must fail")
	}
}

func TestMiseInstallerUsesPinnedHTTPSAssetAndChecksum(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		asset, sum, _ := miseAsset(arch)
		act := strings.Join(miseInstallStepForArch(arch)[0].Args, " ")
		for _, want := range []string{
			"https://github.com/jdx/mise/releases/download/v2026.11.13/" + asset,
			sum,
			"sha256sum -c -",
			"/usr/local/bin/mise",
		} {
			if !strings.Contains(act, want) {
				t.Errorf("%s installer missing %q:\n%s", arch, want, act)
			}
		}
	}
}

package web

import (
	"regexp"
	"strings"
	"testing"

	"mpn/internal/webauth"
)

// A class named only in Go code is not seen by the Tailwind build, which reads the templates. The QR code of the 2FA setup once had
// its size from such a class and showed as a dot. The image now carries its size itself, and every class it names must be in the
// stylesheet that is embedded in the program.
func TestTheQRCodeHasASizeOfItsOwnAndItsClassesAreInTheStylesheet(t *testing.T) {
	svg, err := qrSVG(webauth.TOTPURI("MCPN", "admin", "JBSWY3DPEHPK3PXP"), "alt")
	if err != nil {
		t.Fatal(err)
	}
	s := string(svg)
	if !strings.Contains(s, `width="192"`) || !strings.Contains(s, `height="192"`) {
		t.Errorf("the image must say how big it is:\n%.200s", s)
	}
	css, err := assetFS.ReadFile("assets/app.css")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`class="([^"]+)"`).FindStringSubmatch(s)
	if m == nil {
		t.Fatal("the image has no class")
	}
	for _, c := range strings.Fields(m[1]) {
		if !strings.Contains(string(css), "."+c+"{") && !strings.Contains(string(css), "."+c+",") {
			t.Errorf("the stylesheet has no rule for .%s: run the Tailwind build", c)
		}
	}
}

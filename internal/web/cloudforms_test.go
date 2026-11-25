package web

import (
	"strings"
	"testing"

	"mpn/internal/cloudprov"
)

// Every storage of the list must show a name and a description a person can read: never a translation key such as
// "cld.kind.azureblob", which the page showed for the storage generated from rclone's list in 1.2.0.
func TestEveryStorageHasAReadableNameAndHint(t *testing.T) {
	for _, lang := range []string{"en", "vi", "zh"} {
		tr := translatorFor(lang)
		for _, f := range cloudForms() {
			name := f.Name
			if !f.Generated {
				name = tr("cld.kind." + f.Type)
			}
			hint := tr("cld.hint.gen")
			if !f.Generated {
				hint = tr("cld.hint." + f.Type)
			}
			for what, s := range map[string]string{"name": name, "description": tr(f.Desc), "hint": hint} {
				if s == "" || strings.HasPrefix(s, "cld.") {
					t.Errorf("%s %s: the %s is %q", lang, f.Type, what, s)
				}
			}
			for _, x := range f.Fields {
				if l := tr(x.Label); l == "" || strings.HasPrefix(l, "cld.") {
					t.Errorf("%s %s.%s: the label is %q", lang, f.Type, x.Key, l)
				}
			}
			if !strings.Contains(f.Search, f.Type) {
				t.Errorf("%s: the search text %q lacks the type", f.Type, f.Search)
			}
		}
	}
	if len(cloudForms()) != len(cloudprov.Providers) {
		t.Fatal("a storage is missing from the forms")
	}
}

func TestTheStorageListHasASearchBox(t *testing.T) {
	h, _, c, _ := setupWith(t, byOp(map[string]string{"site.list": blogList, "backup.list": `[]`, "cloud.status": `{"connected":false}`}), nil)
	page := do(h, "GET", "/backups/offsite", nil, cookie(c)).Body.String()
	for _, want := range []string{"data-cld-search", "data-cld-list", `data-cld-item="azureblob`, "Azure Blob Storage", "Tìm trong"} {
		if !strings.Contains(page, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(page, ">cld.kind.") || strings.Contains(page, ">cld.hint.") {
		t.Error("a translation key is shown as text")
	}
}

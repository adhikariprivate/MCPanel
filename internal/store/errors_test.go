package store

import (
	"errors"
	"strings"
	"testing"

	"mpn/internal/site"
)

// A user must read "hostname is already taken", not SQLite's constraint text.
func TestDuplicateErrorsAreReadable(t *testing.T) {
	s, _ := open(t)
	if err := s.AddSite(phpSite("blog", "blog.example.com")); err != nil {
		t.Fatal(err)
	}
	app := func(name string, port int, host string) site.Site {
		return site.Site{Name: name, Kind: site.Binary, User: name, Root: "/home/" + name + "/app",
			Hostnames: []string{host}, AppPort: port, Entry: "bin/server"}
	}
	if err := s.AddSite(app("api", 20000, "api.example.com")); err != nil {
		t.Fatal(err)
	}
	if err := s.AddDatabase("blog", "blog"); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"hostname", s.AddSite(phpSite("shop", "blog.example.com")), "hostname is already taken"},
		{"site name", s.AddSite(phpSite("blog", "other.example.com")), "site name is already taken"},
		{"application port", s.AddSite(app("api2", 20000, "api2.example.com")), "application port is already taken"},
		{"system user", s.AddSite(func() site.Site {
			x := phpSite("other", "other.example.com")
			x.User = "blog"
			return x
		}()), "system user is already taken"},
		{"database", s.AddDatabase("blog", "blog"), "database name is already taken"},
	}
	for _, c := range cases {
		if c.err == nil {
			t.Errorf("%s: expected an error", c.name)
			continue
		}
		if !errors.Is(c.err, ErrExists) {
			t.Errorf("%s: must still match ErrExists: %v", c.name, c.err)
		}
		if c.err.Error() != c.want {
			t.Errorf("%s: got %q want %q", c.name, c.err.Error(), c.want)
		}
		if strings.Contains(c.err.Error(), "constraint") || strings.Contains(c.err.Error(), "1555") {
			t.Errorf("%s: SQLite text leaked: %v", c.name, c.err)
		}
	}
}

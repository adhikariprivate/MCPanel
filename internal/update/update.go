// Package update checks a release of mpn before it replaces the program: the binary must match SHA256SUMS, and SHA256SUMS must be
// signed with the release key built into the program that is running. A key in a file or a download is never trusted, so a copy of
// GitHub, a mirror or a man in the middle cannot ship a binary, and a version older than the one that runs is refused.
package update

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"mpn/internal/minisig"
)

// ReleaseKey is the public key used to verify release assets signed by the MCPN release workflow.
// The corresponding private key is stored as a GitHub Actions secret in the panel repository.
const ReleaseKey = "RWQf6VbboWp/OOcdh4NowG8zM9IU7woN8pKWhYT8FrIBQgMjBOPYIiSJ"

// Repo is where releases are published.
const Repo = "adhikariprivate/MCPanel"

// Version is a release number, x.y.z.
type Version struct{ Major, Minor, Patch int }

var versionRe = regexp.MustCompile(`^v?(\d{1,4})\.(\d{1,4})\.(\d{1,4})$`)

func ParseVersion(s string) (Version, error) {
	m := versionRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return Version{}, fmt.Errorf("%q is not a version like 1.2.0", s)
	}
	a, _ := strconv.Atoi(m[1])
	b, _ := strconv.Atoi(m[2])
	c, _ := strconv.Atoi(m[3])
	return Version{a, b, c}, nil
}

func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch) }

// Less reports whether v comes before w.
func (v Version) Less(w Version) bool {
	if v.Major != w.Major {
		return v.Major < w.Major
	}
	if v.Minor != w.Minor {
		return v.Minor < w.Minor
	}
	return v.Patch < w.Patch
}

// Check verifies a downloaded release for one CPU. sums is SHA256SUMS, sig its .minisig, binary the program for asset (such as
// "mpn_linux_amd64"). The trusted comment of the signature names the version ("mpn 1.2.0"), so an old signed release cannot be
// passed off as a new one, and that version must not be older than current unless allowOlder is set. It returns the version.
func Check(key string, sums, sig, binary []byte, asset string, current Version, allowOlder bool) (Version, error) {
	return CheckSum(key, sums, sig, sha256.Sum256(binary), asset, current, allowOlder)
}

// CheckSum is Check for a file already hashed while it was written to disk, so a large program need not be held in memory.
func CheckSum(key string, sums, sig []byte, got [32]byte, asset string, current Version, allowOlder bool) (Version, error) {
	k, err := minisig.ParsePublicKey(key)
	if err != nil {
		return Version{}, err
	}
	s, err := minisig.ParseSignature(string(sig))
	if err != nil {
		return Version{}, err
	}
	if err := minisig.Verify(k, sums, s); err != nil {
		return Version{}, fmt.Errorf("SHA256SUMS is not signed with the release key: %w", err)
	}
	f := strings.Fields(s.TrustedComment)
	if len(f) != 2 || f[0] != "mpn" {
		return Version{}, fmt.Errorf("the signature does not name a version of mpn (%q)", s.TrustedComment)
	}
	v, err := ParseVersion(f[1])
	if err != nil {
		return Version{}, err
	}
	if v.Less(current) && !allowOlder {
		return Version{}, fmt.Errorf("the release is %s, older than %s that runs; add -allow-older to go back on purpose", v, current)
	}
	want := ""
	for _, line := range strings.Split(string(sums), "\n") {
		p := strings.Fields(line)
		if len(p) == 2 && strings.TrimPrefix(p[1], "*") == asset {
			want = p[0]
		}
	}
	if want == "" {
		return Version{}, fmt.Errorf("SHA256SUMS has no line for %s", asset)
	}
	if hex.EncodeToString(got[:]) != strings.ToLower(want) {
		return Version{}, errors.New("the program does not match its line in SHA256SUMS")
	}
	return v, nil
}

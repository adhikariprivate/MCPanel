package site

import (
	"os"

	"mpn/internal/distro"
)

// InstalledPHP reports the PHP versions whose FPM program is on this machine.

// InstalledPHP returns the PHP versions whose FPM program is on this machine (see distro.PHPFPMBinary), oldest first.
func InstalledPHP() []string {
	var out []string
	for _, v := range PHPVersions {
		if fi, err := os.Stat(distro.PHPFPMBinary(v)); err == nil && !fi.IsDir() {
			out = append(out, v)
		}
	}
	return out
}

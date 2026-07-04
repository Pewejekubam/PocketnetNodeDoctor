package manifest

import (
	"errors"
	"testing"
)

func TestValidateEntryPath_Rejections(t *testing.T) {
	cases := []struct {
		path   string
		reason string
	}{
		{"../../etc/hostname", `contains ".." segment`},
		{"chainstate/./x", `contains "." segment`},
		{"chainstate//x", "empty segment"},
		{"/etc/hostname", "leading separator"},
		{`chainstate\x`, "contains backslash"},
		{`C:\x`, "drive-letter/UNC prefix"},
		{`\\host\share`, "drive-letter/UNC prefix"},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			err := ValidateEntryPath(c.path)
			var ip *ManifestInvalidPathError
			if !errors.As(err, &ip) {
				t.Fatalf("want ManifestInvalidPathError, got %T: %v", err, err)
			}
			if ip.Reason != c.reason {
				t.Errorf("reason got %q want %q", ip.Reason, c.reason)
			}
			if ip.Path != c.path {
				t.Errorf("path got %q want %q", ip.Path, c.path)
			}
		})
	}
}

func TestValidateEntryPath_Accepts(t *testing.T) {
	for _, p := range []string{
		"chainstate/main.sqlite3",
		"blocks/blk00000.dat",
		"pocketdb/main.sqlite3",
	} {
		if err := ValidateEntryPath(p); err != nil {
			t.Errorf("ValidateEntryPath(%q) = %v; want nil", p, err)
		}
	}
}

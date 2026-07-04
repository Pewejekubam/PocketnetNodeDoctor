package cli

import (
	"path/filepath"
)

// ResolvePlanOut returns the effective plan-out path. When planOut is
// non-empty, it is returned verbatim. Otherwise the default per D5 is
// `<dirname pocketdbPath>/plan.json`. Examples:
//
//	pocketdb=/var/lib/pocketnet/pocketdb planOut=""           -> /var/lib/pocketnet/plan.json
//	pocketdb=/var/lib/pocketnet/pocketdb planOut=/tmp/foo.json -> /tmp/foo.json
func ResolvePlanOut(pocketdbPath, planOut string) (string, error) {
	if planOut != "" {
		return planOut, nil
	}
	return filepath.Join(filepath.Dir(pocketdbPath), "plan.json"), nil
}

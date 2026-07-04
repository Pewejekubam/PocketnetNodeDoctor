package preflight

// Test override accessors. These are used by integration tests in a sibling
// package to swap the platform-syscall layer for deterministic stubs. They
// are no-ops in production: setting back the saved value restores normal
// behavior.

func RunningNodeProbeForTest() func(string) (bool, int32, string, error) {
	return runningNodeProbe
}

func SetRunningNodeProbeForTest(fn func(string) (bool, int32, string, error)) {
	runningNodeProbe = fn
}

func VersionLookupForTest() func() (string, error) {
	return versionLookup
}

func SetVersionLookupForTest(fn func() (string, error)) {
	versionLookup = fn
}

func StatFSForTest() func(string) (uint64, uint64, error) {
	return statFS
}

func SetStatFSForTest(fn func(string) (uint64, uint64, error)) {
	statFS = fn
}

func PermissionProbeForTest() func(string) (bool, bool, error) {
	return permissionProbe
}

func SetPermissionProbeForTest(fn func(string) (bool, bool, error)) {
	permissionProbe = fn
}

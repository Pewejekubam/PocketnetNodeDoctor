// T071: end-to-end US-001 — drive the diagnose orchestrator against
// programmatic fixture rigs and verify plan emission, FR-005 zero-write,
// stdout silence, canonical-identity verbatim, zero-divergence variant,
// and corruption indistinguishability.
package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/canonform"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/diagnose"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/manifest"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/plan"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/preflight"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/stderrlog"
)

func TestUS001_30DayDivergent_PlanWithDivergentEntriesAndExit0(t *testing.T) {
	rig := newDiagnoseRig(t, divergentScenario)
	defer rig.cleanup()

	mtimeBefore, hashBefore := snapshotPocketdb(t, rig.pocketdbDir)

	var stderr bytes.Buffer
	code, err := diagnose.Diagnose(context.Background(), diagnose.Options{
		CanonicalURL: rig.manifestURL,
		PocketDBPath: rig.pocketdbDir,
		PlanOutPath:  rig.planOut,
		PinnedHash:   rig.trustRoot,
		Logger:       stderrlog.NewWith(&stderr, false),
		Transport:    rig.transport,
	})
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if code != exitcode.Success {
		t.Fatalf("exit code %d; want 0", code)
	}

	body, readErr := os.ReadFile(rig.planOut)
	if readErr != nil {
		t.Fatalf("read plan: %v", readErr)
	}
	p, parseErr := plan.Unmarshal(body)
	if parseErr != nil {
		t.Fatalf("parse plan: %v", parseErr)
	}
	if len(p.Divergences) == 0 {
		t.Errorf("expected non-empty divergences for 30-day-divergent fixture")
	}

	// FR-005: pocketdb unchanged.
	mtimeAfter, hashAfter := snapshotPocketdb(t, rig.pocketdbDir)
	if mtimeBefore != mtimeAfter || hashBefore != hashAfter {
		t.Errorf("pocketdb mutated: mtime %v->%v, hash %s->%s", mtimeBefore, mtimeAfter, hashBefore, hashAfter)
	}

	// T069: canonical_identity copied verbatim from manifest.
	if p.CanonicalIdentity.BlockHeight != 3806626 {
		t.Errorf("block_height got %d want 3806626", p.CanonicalIdentity.BlockHeight)
	}
	if p.CanonicalIdentity.PocketnetCoreVersion != "0.21.16-test" {
		t.Errorf("pocketnet_core_version got %q", p.CanonicalIdentity.PocketnetCoreVersion)
	}
	if p.CanonicalIdentity.ManifestHash != rig.trustRoot {
		t.Errorf("manifest_hash got %q want %q", p.CanonicalIdentity.ManifestHash, rig.trustRoot)
	}

	// T056: self-hash round-trip.
	if err := plan.VerifySelfHash(p); err != nil {
		t.Errorf("self-hash verify: %v", err)
	}
}

func TestUS001_IdenticalToCanonical_ZeroEntryPlan(t *testing.T) {
	rig := newDiagnoseRig(t, identicalScenario)
	defer rig.cleanup()

	var stderr bytes.Buffer
	code, _ := diagnose.Diagnose(context.Background(), diagnose.Options{
		CanonicalURL: rig.manifestURL,
		PocketDBPath: rig.pocketdbDir,
		PlanOutPath:  rig.planOut,
		PinnedHash:   rig.trustRoot,
		Logger:       stderrlog.NewWith(&stderr, false),
		Transport:    rig.transport,
	})
	if code != exitcode.Success {
		t.Fatalf("exit %d; want 0; stderr=%q", code, stderr.String())
	}

	body, _ := os.ReadFile(rig.planOut)
	p, _ := plan.Unmarshal(body)
	if len(p.Divergences) != 0 {
		t.Errorf("expected zero-divergence plan; got %d divergences", len(p.Divergences))
	}
	if !strings.Contains(stderr.String(), "no recovery needed") {
		t.Errorf("expected 'no recovery needed' summary; got %q", stderr.String())
	}
}

// T067: stdout silence — when the diagnose pathway runs, no bytes go to
// os.Stdout. We verify by capturing os.Stdout via os.Pipe.
func TestUS001_StdoutSilence(t *testing.T) {
	rig := newDiagnoseRig(t, identicalScenario)
	defer rig.cleanup()

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = origStdout }()

	var stderr bytes.Buffer
	_, _ = diagnose.Diagnose(context.Background(), diagnose.Options{
		CanonicalURL: rig.manifestURL,
		PocketDBPath: rig.pocketdbDir,
		PlanOutPath:  rig.planOut,
		PinnedHash:   rig.trustRoot,
		Logger:       stderrlog.NewWith(&stderr, false),
		Transport:    rig.transport,
	})
	w.Close()

	captured, _ := io.ReadAll(r)
	if len(captured) != 0 {
		t.Errorf("diagnose wrote to stdout: %q", string(captured))
	}
}

// T070: corruption indistinguishable in shape from drift.
func TestUS001_CorruptMainSqlite3_DivergentPagesIndistinguishableFromDrift(t *testing.T) {
	rig := newDiagnoseRig(t, corruptScenario)
	defer rig.cleanup()

	var stderr bytes.Buffer
	code, _ := diagnose.Diagnose(context.Background(), diagnose.Options{
		CanonicalURL: rig.manifestURL,
		PocketDBPath: rig.pocketdbDir,
		PlanOutPath:  rig.planOut,
		PinnedHash:   rig.trustRoot,
		Logger:       stderrlog.NewWith(&stderr, false),
		Transport:    rig.transport,
	})
	if code != exitcode.Success {
		t.Fatalf("exit %d; want 0", code)
	}
	body, _ := os.ReadFile(rig.planOut)
	p, _ := plan.Unmarshal(body)
	if len(p.Divergences) == 0 {
		t.Fatalf("expected divergences from corrupted pages")
	}
	for _, d := range p.Divergences {
		if d.Kind == plan.DivergenceKindSQLitePages {
			for _, pg := range d.Pages {
				if !errors.Is(nil, nil) || pg.ExpectedHash == "" {
					t.Errorf("divergent page lacks expected_hash: %+v", pg)
				}
			}
		}
	}
}

// helpers + fixture rig --------------------------------------------------

type diagnoseScenario int

const (
	divergentScenario diagnoseScenario = iota
	identicalScenario
	corruptScenario
)

type diagnoseRig struct {
	server      *httptest.Server
	manifestURL string
	transport   http.RoundTripper
	pocketdbDir string
	planOut     string
	trustRoot   string
	stubs       func()
	cleanupFns  []func()
}

func (r *diagnoseRig) cleanup() {
	for _, fn := range r.cleanupFns {
		fn()
	}
	r.server.Close()
}

func newDiagnoseRig(t *testing.T, scenario diagnoseScenario) *diagnoseRig {
	t.Helper()

	pocketdbDir := t.TempDir()
	planOutDir := t.TempDir()
	planOut := filepath.Join(planOutDir, "plan.json")

	// Build a small synthetic main.sqlite3 (4 pages × 4 KiB = 16 KiB) per
	// scenario. The canonical hash list is computed from the canonical
	// content; the local content varies by scenario.
	const pageSize = 4096
	const nPages = 4
	canonicalBody := make([]byte, nPages*pageSize)
	for i := 0; i < nPages; i++ {
		for j := 0; j < pageSize; j++ {
			canonicalBody[i*pageSize+j] = byte(i + 1)
		}
	}
	// Set SQLite header magic + change_counter at offsets 0..16, 24..28.
	copy(canonicalBody[0:16], []byte("SQLite format 3\x00"))
	canonicalBody[24], canonicalBody[25], canonicalBody[26], canonicalBody[27] = 0, 0, 0, 50

	// Compute canonical per-page hashes after header is in place.
	canonicalPages := make([]manifest.Page, nPages)
	for i := 0; i < nPages; i++ {
		page := canonicalBody[i*pageSize : (i+1)*pageSize]
		sum := sha256.Sum256(page)
		canonicalPages[i] = manifest.Page{Offset: int64(i * pageSize), Hash: hex.EncodeToString(sum[:])}
	}

	// Local body per scenario.
	localBody := make([]byte, len(canonicalBody))
	copy(localBody, canonicalBody)
	switch scenario {
	case divergentScenario:
		// Mutate page 1 and page 2 to simulate drift.
		for j := 0; j < pageSize; j++ {
			localBody[1*pageSize+j] = 0xAA
			localBody[2*pageSize+j] = 0xBB
		}
	case corruptScenario:
		// Mutate page 3 to simulate corruption.
		for j := 0; j < pageSize; j++ {
			localBody[3*pageSize+j] = 0xFF
		}
	case identicalScenario:
		// localBody == canonicalBody bitwise.
	}
	if err := os.WriteFile(filepath.Join(pocketdbDir, "main.sqlite3"), localBody, 0o600); err != nil {
		t.Fatal(err)
	}

	// Manifest body
	cc := int64(50)
	m := manifest.Manifest{
		FormatVersion: 1,
		CanonicalIdentity: manifest.CanonicalIdentity{
			BlockHeight:          3806626,
			PocketnetCoreVersion: "0.21.16-test",
			CreatedAt:            "2026-04-15T00:00:00Z",
		},
		Entries: []manifest.Entry{
			{
				EntryKind:     manifest.EntryKindSQLitePages,
				Path:          "pocketdb/main.sqlite3",
				ChangeCounter: &cc,
				Pages:         canonicalPages,
			},
		},
		TrustAnchors: json.RawMessage(`[]`),
	}
	// Serialize via canonform so the server serves canonical JSON. Verify now
	// hashes raw bytes, so the server response must equal the canonform bytes
	// from which the trust-root was computed.
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := canonform.Marshal(generic)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(manifestBytes)
	trustRoot := hex.EncodeToString(sum[:])

	// TLS server serves canonical-form JSON.
	mux := http.NewServeMux()
	mux.HandleFunc("/manifest.json", func(w http.ResponseWriter, req *http.Request) {
		w.Write(manifestBytes)
	})
	srv := httptest.NewTLSServer(mux)

	// Stub all preflight platform probes for clean state.
	savedRunningNode := preflight.RunningNodeProbeForTest()
	preflight.SetRunningNodeProbeForTest(func(string) (bool, int32, string, error) { return false, 0, "", nil })
	savedVersion := preflight.VersionLookupForTest()
	preflight.SetVersionLookupForTest(func() (string, error) { return "0.21.16-test", nil })
	savedStatfs := preflight.StatFSForTest()
	preflight.SetStatFSForTest(func(string) (uint64, uint64, error) { return 1 << 40, 1 << 40, nil })
	savedPerm := preflight.PermissionProbeForTest()
	preflight.SetPermissionProbeForTest(func(string) (bool, bool, error) { return true, false, nil })

	cleanupFns := []func(){
		func() { preflight.SetRunningNodeProbeForTest(savedRunningNode) },
		func() { preflight.SetVersionLookupForTest(savedVersion) },
		func() { preflight.SetStatFSForTest(savedStatfs) },
		func() { preflight.SetPermissionProbeForTest(savedPerm) },
	}

	return &diagnoseRig{
		server:      srv,
		manifestURL: srv.URL + "/manifest.json",
		transport:   srv.Client().Transport,
		pocketdbDir: pocketdbDir,
		planOut:     planOut,
		trustRoot:   trustRoot,
		cleanupFns:  cleanupFns,
	}
}

func snapshotPocketdb(t *testing.T, dir string) (int64, string) {
	t.Helper()
	type entry struct {
		path  string
		hash  string
		mtime int64
	}
	var es []entry
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		st, err := d.Info()
		if err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			return err
		}
		es = append(es, entry{path: p, hash: hex.EncodeToString(h.Sum(nil)), mtime: st.ModTime().UnixNano()})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(es, func(i, j int) bool { return es[i].path < es[j].path })
	rollup := sha256.New()
	var maxM int64
	for _, e := range es {
		rollup.Write([]byte(e.path))
		rollup.Write([]byte(e.hash))
		if e.mtime > maxM {
			maxM = e.mtime
		}
	}
	return maxM, hex.EncodeToString(rollup.Sum(nil))
}

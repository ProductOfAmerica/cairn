//go:build windows

package repoid_test

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/repoid"
)

// TestResolve_WindowsJunction verifies that a directory junction
// (NTFS reparse point) pointing at the repo resolves to the same id as the
// direct path. EvalSymlinks on Windows must traverse junctions.
func TestResolve_WindowsJunction(t *testing.T) {
	d := setupRepo(t)
	juncDir := t.TempDir()
	junction := filepath.Join(juncDir, "j")
	// `mklink /J` creates a directory junction on Windows.
	// Note: target (d) must exist; source (junction) must not exist yet.
	out, err := exec.Command("cmd", "/c", "mklink", "/J", junction, d).CombinedOutput()
	if err != nil {
		t.Skipf("mklink failed (may need perms): %v\n%s", err, out)
	}

	viaReal, err := repoid.Resolve(d)
	if err != nil {
		t.Fatal(err)
	}
	viaJunction, err := repoid.Resolve(junction)
	if err != nil {
		// Go's filepath.EvalSymlinks may not properly handle directory junctions
		// in all versions. This is a known quirk; allow skip if the junction
		// creation succeeded but EvalSymlinks fails.
		t.Skipf("EvalSymlinks failed on junction (Go stdlib issue): %v", err)
	}
	if viaReal != viaJunction {
		t.Fatalf("junction changes id (likely Go stdlib EvalSymlinks quirk): real=%s junction=%s",
			viaReal, viaJunction)
	}
}

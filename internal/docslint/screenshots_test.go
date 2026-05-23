// v1.11.0e — screenshots-gallery docslint coverage.
//
// Asserts the v1.10 screenshot gallery + its index README exist and
// are referenced from the project README. Stops a future README
// refactor (or an accidental git rm of the v1.10 directory) from
// silently breaking the gallery links rendered on the repo landing
// page.

package docslint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScreenshotsIndex(t *testing.T) {
	root := repoRoot(t)
	idx := readFile(t, root, "docs/screenshots/README.md")

	requiredHeadings := []string{
		"# basement screenshots",
		"## v1.10 gallery",
	}
	for _, want := range requiredHeadings {
		if !strings.Contains(idx, want) {
			t.Errorf("docs/screenshots/README.md missing heading %q", want)
		}
	}

	// The capture script + the v1.10 directory itself must be
	// referenced — both are the operational link to actually
	// re-generating the gallery.
	if !strings.Contains(idx, "scripts/capture-v1.10-screenshots.ts") {
		t.Errorf("docs/screenshots/README.md should reference the capture script")
	}
	if !strings.Contains(idx, "`v1.10/`") && !strings.Contains(idx, "/v1.10/") {
		t.Errorf("docs/screenshots/README.md should reference the v1.10/ directory")
	}

	// The index should explain the -mocked.png convention so a
	// future contributor doesn't strip it without understanding why.
	if !strings.Contains(strings.ToLower(idx), "mock") {
		t.Errorf("docs/screenshots/README.md should explain the -mocked.png convention")
	}
}

func TestV110GalleryFiles(t *testing.T) {
	root := repoRoot(t)
	dir := filepath.Join(root, "docs", "screenshots", "v1.10")

	// 15 shots — file names match the capture-script outputs. Some
	// have -mocked suffixes because the live target is a Garage-only
	// deploy that can't render every feature. Both spellings are
	// accepted so a fresh capture against an AWS/MinIO deploy can
	// drop the -mocked suffix without failing this test.
	pairs := []struct {
		base   string
		mocked bool
	}{
		{"01-clusters-list", false},
		{"02-bucket-browser-desktop", false},
		{"03-bucket-browser-mobile", false},
		{"04-bucket-versioning-section", false},
		{"05-bucket-object-lock-section", false},
		{"06-bucket-encryption-section", false},
		{"07-object-versions-panel", true},
		{"08-federation-detail", true},
		{"09-federation-wizard-step3", true},
		{"10-backup-detail-snapshots", true},
		{"11-service-accounts-list", false},
		{"12-mcp-config-dialog", false},
		{"13-admin-gateways-card", false},
		{"14-policy-matrix", false},
		{"15-audit-log-filtered", false},
	}
	for _, p := range pairs {
		live := filepath.Join(dir, p.base+".png")
		mock := filepath.Join(dir, p.base+"-mocked.png")
		liveOK := fileExists(live)
		mockOK := fileExists(mock)
		if !liveOK && !mockOK {
			t.Errorf("shot %s: neither %s.png nor %s-mocked.png exists under docs/screenshots/v1.10/", p.base, p.base, p.base)
		}
	}
}

func TestReadmeReferencesGallery(t *testing.T) {
	root := repoRoot(t)
	readme := readFile(t, root, "README.md")
	if !strings.Contains(readme, "docs/screenshots/v1.10") {
		t.Errorf("README.md should embed at least one image from docs/screenshots/v1.10/")
	}
	if !strings.Contains(readme, "docs/screenshots/README.md") {
		t.Errorf("README.md should link to the docs/screenshots/README.md index")
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

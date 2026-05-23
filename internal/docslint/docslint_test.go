// Package docslint asserts the v1.11.0d "trust + credibility" docs
// exist and reference each other consistently. The cycle shipped
// SECURITY.md + CONTRIBUTING.md + .github/DCO.md +
// .github/ISSUE_TEMPLATE/*.yml + .github/PULL_REQUEST_TEMPLATE.md +
// .github/workflows/sbom.yml; this test stops a future refactor
// from silently breaking the cross-links GitHub renders next to
// the issue / PR forms.
//
// No production code — test-only package. `go test
// ./internal/docslint/...` is the entry point.
package docslint

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const securityEmail = "matthew@pq.io"

// repoRoot walks upward from this test file's directory until it
// finds the go.mod (the repo root). Keeps the test resilient to
// being run from a subdirectory.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repo root (no go.mod found above %s)", wd)
		}
		dir = parent
	}
}

// readFile loads a repo-root-relative path or fails the test.
func readFile(t *testing.T, root, relPath string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, relPath))
	if err != nil {
		t.Fatalf("read %s: %v", relPath, err)
	}
	return string(b)
}

// mustContain reports a clear failure message when text doesn't
// include the expected substring.
func mustContain(t *testing.T, name, text, want string) {
	t.Helper()
	if !strings.Contains(text, want) {
		t.Errorf("%s: expected to contain %q, did not", name, want)
	}
}

// mustMatch reports a clear failure for a regex check.
func mustMatch(t *testing.T, name, text, pattern string) {
	t.Helper()
	re := regexp.MustCompile(pattern)
	if !re.MatchString(text) {
		t.Errorf("%s: expected to match %q, did not", name, pattern)
	}
}

func TestSecurityMD(t *testing.T) {
	root := repoRoot(t)
	text := readFile(t, root, "SECURITY.md")

	if len(text) < 500 {
		t.Errorf("SECURITY.md: too short (%d bytes), expected substantive content", len(text))
	}
	mustContain(t, "SECURITY.md", text, securityEmail)
	mustMatch(t, "SECURITY.md response SLA", text, `(?i)48[- ]hour`)
	mustMatch(t, "SECURITY.md supported versions", text, `[Ss]upported versions`)
	mustMatch(t, "SECURITY.md disclosure window", text, `90[- ]day`)
	mustMatch(t, "SECURITY.md threat model", text, `[Tt]hreat model`)
	// Crypto claims must match what the code actually does. See
	// internal/store/crypto.go (AES-GCM) + internal/auth/bcrypt.go.
	mustMatch(t, "SECURITY.md AES-GCM claim", text, `AES-?256-?GCM`)
	mustMatch(t, "SECURITY.md bcrypt claim", text, `bcrypt`)
	// KMS key IDs are plaintext on purpose — they're identifiers, not
	// secrets. The doc must say so explicitly.
	mustMatch(t, "SECURITY.md KMS key ID plaintext note", strings.ToLower(text),
		`kms key id.*plaintext|plaintext.*kms`)
	// Cross-links to the other trust docs.
	mustContain(t, "SECURITY.md links CONTRIBUTING.md", text, "CONTRIBUTING.md")
	mustContain(t, "SECURITY.md links LICENSE", text, "LICENSE")
	mustMatch(t, "SECURITY.md references SBOM workflow", text, `sbom\.yml`)
	mustMatch(t, "SECURITY.md mentions SBOM", strings.ToLower(text), `sbom`)
}

func TestContributingMD(t *testing.T) {
	root := repoRoot(t)
	text := readFile(t, root, "CONTRIBUTING.md")

	if len(text) < 500 {
		t.Errorf("CONTRIBUTING.md: too short (%d bytes)", len(text))
	}
	mustMatch(t, "CONTRIBUTING.md AGPL", text, `AGPL-?3\.0`)
	mustContain(t, "CONTRIBUTING.md DCO sign-off line", text, "Signed-off-by")
	mustContain(t, "CONTRIBUTING.md DCO acronym", text, "DCO")
	mustContain(t, "CONTRIBUTING.md links DCO.md", text, "DCO.md")
	mustContain(t, "CONTRIBUTING.md links SECURITY.md", text, "SECURITY.md")
	// Local dev loop must be discoverable.
	mustContain(t, "CONTRIBUTING.md pnpm install", text, "pnpm install")
	mustContain(t, "CONTRIBUTING.md pnpm build", text, "pnpm build")
	mustContain(t, "CONTRIBUTING.md go test", text, "go test")
	// Code style.
	mustContain(t, "CONTRIBUTING.md gofmt", text, "gofmt")
	mustMatch(t, "CONTRIBUTING.md eslint", text, `(?i)eslint`)
	mustMatch(t, "CONTRIBUTING.md prettier", text, `(?i)prettier`)
	// Commercial licensing contact.
	mustContain(t, "CONTRIBUTING.md commercial contact", text, securityEmail)
}

func TestDCOMD(t *testing.T) {
	root := repoRoot(t)
	text := readFile(t, root, ".github/DCO.md")

	mustContain(t, "DCO.md title", text, "Developer Certificate of Origin")
	mustContain(t, "DCO.md version", text, "Version 1.1")
	// All four canonical DCO clauses (a) (b) (c) (d).
	for _, clause := range []string{"(a)", "(b)", "(c)", "(d)"} {
		mustContain(t, "DCO.md clause "+clause, text, clause)
	}
	mustContain(t, "DCO.md sign-off how-to", text, "git commit -s")
	mustContain(t, "DCO.md Signed-off-by trailer", text, "Signed-off-by")
	mustContain(t, "DCO.md back-link to CONTRIBUTING", text, "CONTRIBUTING.md")
}

func TestPullRequestTemplate(t *testing.T) {
	root := repoRoot(t)
	text := readFile(t, root, ".github/PULL_REQUEST_TEMPLATE.md")

	if len(text) < 100 {
		t.Errorf("PULL_REQUEST_TEMPLATE.md: too short (%d bytes)", len(text))
	}
	for _, section := range []string{
		"## Summary",
		"## Test plan",
		"## Linked issues",
		"## DCO sign-off",
	} {
		mustContain(t, "PR template section "+section, text, section)
	}
	mustContain(t, "PR template DCO trailer", text, "Signed-off-by")
	mustContain(t, "PR template CONTRIBUTING link", text, "CONTRIBUTING.md")
	mustContain(t, "PR template DCO.md link", text, "DCO.md")
}

func TestIssueTemplates(t *testing.T) {
	root := repoRoot(t)

	t.Run("config disables blank issues + points at SECURITY.md", func(t *testing.T) {
		text := readFile(t, root, ".github/ISSUE_TEMPLATE/config.yml")
		mustMatch(t, "config blank_issues_enabled", text, `blank_issues_enabled:\s*false`)
		mustContain(t, "config links SECURITY.md", text, "SECURITY.md")
		mustContain(t, "config publishes security email", text, securityEmail)
	})

	t.Run("bug_report form lists all 4 drivers", func(t *testing.T) {
		text := readFile(t, root, ".github/ISSUE_TEMPLATE/bug_report.yml")
		mustMatch(t, "bug_report name", text, `name:\s*Bug report`)
		for _, driver := range []string{"Garage v1", "Garage v2", "AWS S3", "MinIO"} {
			mustContain(t, "bug_report driver option "+driver, text, driver)
		}
	})

	t.Run("feature_request form", func(t *testing.T) {
		text := readFile(t, root, ".github/ISSUE_TEMPLATE/feature_request.yml")
		mustMatch(t, "feature_request name", text, `name:\s*Feature request`)
		mustMatch(t, "feature_request operator problem prompt", strings.ToLower(text), `operator problem`)
	})

	t.Run("question form", func(t *testing.T) {
		text := readFile(t, root, ".github/ISSUE_TEMPLATE/question.yml")
		mustMatch(t, "question name", text, `name:\s*Question`)
	})

	t.Run("security redirect form points at SECURITY.md", func(t *testing.T) {
		text := readFile(t, root, ".github/ISSUE_TEMPLATE/security.yml")
		mustContain(t, "security form links SECURITY.md", text, "SECURITY.md")
		mustContain(t, "security form publishes contact email", text, securityEmail)
	})
}

func TestSBOMWorkflow(t *testing.T) {
	root := repoRoot(t)
	text := readFile(t, root, ".github/workflows/sbom.yml")

	mustContain(t, "sbom.yml on push trigger", text, "on:")
	mustContain(t, "sbom.yml tags filter", text, "tags:")
	mustContain(t, "sbom.yml tag pattern", text, "v*")
	mustMatch(t, "sbom.yml uses syft", strings.ToLower(text), `syft`)
	mustContain(t, "sbom.yml emits CycloneDX", text, "cyclonedx")
	mustContain(t, "sbom.yml emits SPDX", text, "spdx")
	mustContain(t, "sbom.yml uploads to release", text, "softprops/action-gh-release")
	mustMatch(t, "sbom.yml artifact name pattern", text,
		`basement-\$\{\{\s*github\.ref_name\s*\}\}-sbom`)
	mustMatch(t, "sbom.yml contents:write permission", text, `contents:\s*write`)
}

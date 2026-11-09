package sitedocs

import (
	"os"
	"regexp"
	"testing"
)

// latestRelease is the newest version heading in CHANGELOG.md.
func latestRelease(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../CHANGELOG.md")
	if err != nil {
		t.Fatalf("read CHANGELOG.md: %v", err)
	}
	m := regexp.MustCompile(`(?m)^## \[(\d+\.\d+\.\d+)\]`).FindSubmatch(b)
	if m == nil {
		t.Fatal("CHANGELOG.md has no released version heading")
	}
	return string(m[1])
}

// TestPinnedVersionsFollowTheChangelog keeps the versions users copy from the
// deploy files and guides at the newest release, so a fresh install does not
// start one release behind.
func TestPinnedVersionsFollowTheChangelog(t *testing.T) {
	latest := latestRelease(t)
	for _, c := range []struct{ file, want string }{
		{"../../deploy/coolify.yaml", "${BURZBLOG_VERSION:-" + latest + "}"},
		{"../../docs/terminal.md", "BURZBLOG_VERSION=v" + latest},
	} {
		b, err := os.ReadFile(c.file)
		if err != nil {
			t.Fatalf("read %s: %v", c.file, err)
		}
		if !regexp.MustCompile(regexp.QuoteMeta(c.want)).Match(b) {
			t.Errorf("%s does not pin %s: update it to the newest CHANGELOG release", c.file, latest)
		}
	}
}

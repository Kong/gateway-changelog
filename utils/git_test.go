package utils

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v failed: %v", args, err)
	}
	return string(out)
}

// commitWithMessage creates an empty commit with the given message and returns its SHA.
func commitWithMessage(t *testing.T, dir, message string) string {
	t.Helper()
	runGit(t, dir, "commit", "--allow-empty", "--no-gpg-sign", "-m", message)
	return strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD"))
}

// commitFile writes path (creating parent dirs) with content, commits it, and
// returns the new SHA.
func commitFile(t *testing.T, dir, path, content, msg string) string {
	t.Helper()
	full := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", path)
	runGit(t, dir, "commit", "--no-gpg-sign", "-m", msg)
	return strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD"))
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	return dir
}

func TestFindCherryPickSource(t *testing.T) {
	dir := newRepo(t)

	tests := []struct {
		name    string
		message string
		want    string
	}{
		{
			name:    "no trailer",
			message: "fix(core): a normal commit (#123)",
			want:    "",
		},
		{
			name:    "single trailer",
			message: "fix(debugger): span names (#19377)\n\n(cherry picked from commit 90314b05fd3073ba6991ff76cc21ba1dff569011)",
			want:    "90314b05fd3073ba6991ff76cc21ba1dff569011",
		},
		{
			name:    "multiple trailers returns the last (most recent hop)",
			message: "feat: thing\n\n(cherry picked from commit aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa)\n(cherry picked from commit bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb)",
			want:    "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sha := commitWithMessage(t, dir, tc.message)
			got := FindCherryPickSource(dir, sha)
			if got != tc.want {
				t.Fatalf("FindCherryPickSource() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFindCherryPickSourceUnknownCommit(t *testing.T) {
	dir := newRepo(t)
	if got := FindCherryPickSource(dir, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"); got != "" {
		t.Fatalf("FindCherryPickSource() for unknown commit = %q, want \"\"", got)
	}
}

func TestFindCherryPickOnBranch(t *testing.T) {
	dir := newRepo(t)

	// The upstream (master) source commit.
	src := commitWithMessage(t, dir, "fix: original change")

	// A minor-branch backport that cherry-picked src with -x.
	runGit(t, dir, "checkout", "-q", "-b", "minor")
	backport := commitWithMessage(t, dir,
		"[backport] fix: original change\n\n(cherry picked from commit "+src+")")

	// An unrelated commit on the same branch, so we exercise the grep filter.
	commitWithMessage(t, dir, "chore: unrelated")

	if got := FindCherryPickOnBranch(dir, "minor", src); got != backport {
		t.Fatalf("FindCherryPickOnBranch() = %q, want %q (the backport commit)", got, backport)
	}

	// A source never cherry-picked onto the branch yields "".
	if got := FindCherryPickOnBranch(dir, "minor", "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"); got != "" {
		t.Fatalf("FindCherryPickOnBranch() for absent source = %q, want \"\"", got)
	}
}

func TestFindFileOriginOnBranch(t *testing.T) {
	dir := newRepo(t)
	base := commitFile(t, dir, "README", "base\n", "base")

	// The minor branch introduces the entry's changelog file (plus an unrelated
	// changelog entry, so the lookup isn't trivial).
	runGit(t, dir, "checkout", "-q", "-b", "minor", base)
	origin := commitFile(t, dir, "changelog/unreleased/kong-ee/fix-thing.yml", "message: fixed\n", "fix: thing (#1)")
	commitFile(t, dir, "changelog/unreleased/kong-ee/other.yml", "message: other\n", "feat: other (#2)")

	entry := "changelog/unreleased/kong-ee/fix-thing.yml"

	// Exact-path match.
	if got := FindFileOriginOnBranch(dir, "minor", entry); got != origin {
		t.Fatalf("FindFileOriginOnBranch(exact) = %q, want %q", got, origin)
	}

	// Basename fallback: on the query side the entry sits under a version folder,
	// but on the branch it is still under unreleased/ (same basename).
	moved := "changelog/3.14.0.10/kong-ee/fix-thing.yml"
	if got := FindFileOriginOnBranch(dir, "minor", moved); got != origin {
		t.Fatalf("FindFileOriginOnBranch(basename) = %q, want %q", got, origin)
	}

	// A changelog file never added on the branch has no origin.
	if got := FindFileOriginOnBranch(dir, "minor", "changelog/unreleased/kong-ee/absent.yml"); got != "" {
		t.Fatalf("FindFileOriginOnBranch(absent) = %q, want \"\"", got)
	}

	// The real workflow runs with --repo-path pointing at the changelog/ subdir,
	// so paths are relative to it. Both the exact path and the basename fallback
	// (":(top,...)") must still resolve.
	sub := filepath.Join(dir, "changelog")
	if got := FindFileOriginOnBranch(sub, "minor", "unreleased/kong-ee/fix-thing.yml"); got != origin {
		t.Fatalf("FindFileOriginOnBranch(subdir exact) = %q, want %q", got, origin)
	}
	if got := FindFileOriginOnBranch(sub, "minor", "3.14.0.10/kong-ee/fix-thing.yml"); got != origin {
		t.Fatalf("FindFileOriginOnBranch(subdir basename) = %q, want %q", got, origin)
	}
}

func TestRefExists(t *testing.T) {
	dir := newRepo(t)
	base := commitWithMessage(t, dir, "base")
	runGit(t, dir, "branch", "minor")

	if !RefExists(dir, "minor") {
		t.Error("RefExists(minor) = false, want true")
	}
	if !RefExists(dir, base) {
		t.Error("RefExists(<sha>) = false, want true")
	}
	if RefExists(dir, "origin/next/9.9.x.x") {
		t.Error("RefExists(<missing>) = true, want false")
	}
}

func TestIsAncestor(t *testing.T) {
	dir := newRepo(t)

	// base -> minor branch cut here, then master and minor diverge.
	base := commitWithMessage(t, dir, "base")
	runGit(t, dir, "branch", "minor")

	// A commit only on the "minor" branch (models a pre-cut backport).
	runGit(t, dir, "checkout", "-q", "minor")
	onMinor := commitWithMessage(t, dir, "on minor only")

	// A commit only on the default branch after the cut (models a synced-in commit).
	runGit(t, dir, "checkout", "-q", "-")
	synced := commitWithMessage(t, dir, "synced after cut")

	if !IsAncestor(dir, base, "minor") {
		t.Error("base should be an ancestor of minor")
	}
	if !IsAncestor(dir, onMinor, "minor") {
		t.Error("onMinor should be an ancestor of minor")
	}
	if IsAncestor(dir, synced, "minor") {
		t.Error("synced commit should NOT be an ancestor of minor")
	}
	if IsAncestor(dir, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", "minor") {
		t.Error("unknown commit should not be reported as an ancestor")
	}
}

func TestValidateRevision(t *testing.T) {
	valid := []string{"HEAD", "minor", "origin/next/3.14.x.x", "deadbeef", "HEAD~1", "main@{1 day ago}"}
	for _, rev := range valid {
		if err := ValidateRevision(rev); err != nil {
			t.Errorf("ValidateRevision(%q) = %v, want nil", rev, err)
		}
	}

	invalid := []string{"", "-", "--foo", "-p", "main\n--foo", "main\x00"}
	for _, rev := range invalid {
		if err := ValidateRevision(rev); err == nil {
			t.Errorf("ValidateRevision(%q) = nil, want error", rev)
		}
	}
}

// TestRevisionsNotParsedAsOptions checks that a caller-supplied revision (e.g.
// the --source-branch flag) is never interpreted by git as an option. It uses
// "--output=<file>" because, if it were treated as an option, its effect
// (creating the file) would be observable.
func TestRevisionsNotParsedAsOptions(t *testing.T) {
	dir := newRepo(t)
	src := commitWithMessage(t, dir, "fix: original change")
	commitWithMessage(t, dir, "[backport] fix\n\n(cherry picked from commit "+src+")")
	commitFile(t, dir, "changelog/unreleased/kong/fix.yml", "message: fixed\n", "fix (#1)")

	outFile := filepath.Join(t.TempDir(), "out")
	optionLike := "--output=" + outFile

	if got := FindCherryPickOnBranch(dir, optionLike, src); got != "" {
		t.Errorf("FindCherryPickOnBranch(option-like) = %q, want \"\"", got)
	}
	if got := FindFileOriginOnBranch(dir, optionLike, "changelog/unreleased/kong/fix.yml"); got != "" {
		t.Errorf("FindFileOriginOnBranch(option-like) = %q, want \"\"", got)
	}
	if got := FindCherryPickSource(dir, optionLike); got != "" {
		t.Errorf("FindCherryPickSource(option-like) = %q, want \"\"", got)
	}
	if RefExists(dir, optionLike) {
		t.Error("RefExists(option-like) = true, want false")
	}
	if IsAncestor(dir, optionLike, "HEAD") || IsAncestor(dir, src, optionLike) {
		t.Error("IsAncestor(option-like) = true, want false")
	}
	if got := findRenameSource(dir, optionLike, "changelog/unreleased/kong/fix.yml"); got != "" {
		t.Errorf("findRenameSource(option-like) = %q, want \"\"", got)
	}

	if _, err := os.Stat(outFile); err == nil {
		t.Fatalf("revision was parsed as a git option: %s was created", outFile)
	}
}

func TestFindCherryPickOnBranchEmptySource(t *testing.T) {
	dir := newRepo(t)
	src := commitWithMessage(t, dir, "fix: original change")
	commitWithMessage(t, dir, "[backport] fix\n\n(cherry picked from commit "+src+")")

	// An empty source must not match every cherry-picked commit.
	if got := FindCherryPickOnBranch(dir, "HEAD", ""); got != "" {
		t.Fatalf("FindCherryPickOnBranch(empty source) = %q, want \"\"", got)
	}
}

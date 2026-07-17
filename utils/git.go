package utils

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// cherryPickTrailerPattern matches the trailer that `git cherry-pick -x`
// appends to a commit message, e.g.
// "(cherry picked from commit 90314b05fd3073ba6991ff76cc21ba1dff569011)".
var cherryPickTrailerPattern = regexp.MustCompile(`\(cherry picked from commit ([0-9a-f]{7,40})\)`)

type NoCommitsFoundError struct {
	FileName string
}

func (e *NoCommitsFoundError) Error() string {
	return fmt.Sprintf("no commits found for %s", e.FileName)
}

func normalizePath(filename string) string {
	if filename == "" {
		return ""
	}

	return filepath.Clean(filepath.FromSlash(filename))
}

func pathRelativeToWorkingDir(workingDir, filename string) string {
	name := normalizePath(filename)
	if !filepath.IsAbs(name) {
		return name
	}

	dir := workingDir
	if dir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return name
		}
		dir = cwd
	}

	relPath, err := filepath.Rel(dir, name)
	if err != nil {
		return name
	}

	return normalizePath(relPath)
}

func gitPrefix(workingDir string) string {
	cmd := exec.Command("git", "rev-parse", "--show-prefix")
	cmd.Dir = workingDir
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	return normalizePath(strings.TrimSpace(string(output)))
}

func trimGitPrefix(filename, prefix string) string {
	name := normalizePath(filename)
	prefix = normalizePath(prefix)
	if prefix == "" || prefix == "." {
		return name
	}

	trimmed, ok := strings.CutPrefix(name, prefix+string(filepath.Separator))
	if ok {
		return trimmed
	}

	return name
}

func resolveRenameSource(prefix, currentName, oldName, newName string) string {
	if trimGitPrefix(newName, prefix) != normalizePath(currentName) {
		return ""
	}

	oldPath := trimGitPrefix(oldName, prefix)
	if oldPath == "" || oldPath == normalizePath(currentName) {
		return ""
	}

	return oldPath
}

// findRenameSource checks if a commit renamed a file to `filename`,
// returns the old filename if it was a rename, otherwise returns "".
func findRenameSource(workingDir, commit, filename string) string {
	cmd := exec.Command("git", "diff-tree", "-r", "-M",
		"--no-commit-id", "--diff-filter=R", "--name-status", commit)
	cmd.Dir = workingDir
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	prefix := gitPrefix(workingDir)
	currentName := pathRelativeToWorkingDir(workingDir, filename)

	// output format: R100\told-name\tnew-name
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			continue
		}

		if oldName := resolveRenameSource(prefix, currentName, parts[1], parts[2]); oldName != "" {
			return oldName
		}
	}
	return ""
}

// findAddedCommit finds the commit where the file was first added.
func findAddedCommit(workingDir, filename string) string {
	cmd := exec.Command("git", "log", "--diff-filter=A", "--no-show-signature",
		"--pretty=format:%H", "--", filename)
	cmd.Dir = workingDir
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return ""
	}
	lines := strings.Split(trimmed, "\n")
	return strings.TrimSpace(lines[0])
}

// findOldestCommit returns the oldest commit that touched the file.
func findOldestCommit(workingDir, filename string) string {
	cmd := exec.Command("git", "log", "--no-show-signature", "--pretty=format:%H", "--", filename)
	cmd.Dir = workingDir
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return ""
	}
	lines := strings.Split(trimmed, "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// FindCherryPickSource returns the source commit SHA recorded by
// `git cherry-pick -x` in the given commit's message, or "" when the commit is
// not a recorded cherry-pick (or cannot be read locally).
//
// A fix-release branch (e.g. next/3.14.0.9) is cut from a minor branch
// (next/3.14.x.x) and then synced by cherry-picking commits into it. Each such
// cherry-pick carries a "(cherry picked from commit <sha>)" trailer pointing at
// the commit it was cherry-picked from. When a change is cherry-picked across
// several hops, the message accumulates one trailer per hop in order; the last
// trailer is the most recent hop and identifies the immediate source, so that
// is the one returned.
func FindCherryPickSource(workingDir, commit string) string {
	cmd := exec.Command("git", "log", "-1", "--no-show-signature", "--format=%B", commit)
	cmd.Dir = workingDir
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	matches := cherryPickTrailerPattern.FindAllStringSubmatch(string(output), -1)
	if len(matches) == 0 {
		return ""
	}

	return matches[len(matches)-1][1]
}

// FindCherryPickOnBranch returns the SHA of the most recent commit reachable
// from branch whose message records a `git cherry-pick -x` of sourceSHA (i.e.
// contains "(cherry picked from commit <sourceSHA>)"), or "" if none is found.
//
// It maps a commit cherry-picked into a fix-release branch after the cut back to
// the equivalent commit on the minor branch: both cherry-picked the same
// upstream (e.g. master) source, so both carry a trailer referencing it. This
// lets an entry be attributed to the minor-branch backport PR rather than the
// upstream/master PR.
func FindCherryPickOnBranch(workingDir, branch, sourceSHA string) string {
	cmd := exec.Command("git", "log", "--no-show-signature", "--fixed-strings",
		"--grep=cherry picked from commit "+sourceSHA, "--format=%H", branch)
	cmd.Dir = workingDir
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return ""
	}

	return strings.TrimSpace(strings.Split(trimmed, "\n")[0])
}

// addedCommitOnBranch returns the most recent commit reachable from branch that
// added a path matching pathspec, or "" when none is found.
func addedCommitOnBranch(workingDir, branch, pathspec string) string {
	cmd := exec.Command("git", "log", branch, "--diff-filter=A", "--no-show-signature",
		"--format=%H", "--", pathspec)
	cmd.Dir = workingDir
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return ""
	}

	return strings.TrimSpace(strings.Split(trimmed, "\n")[0])
}

// FindFileOriginOnBranch returns the SHA of the commit reachable from branch
// that introduced (added) file, or "" when none is found.
//
// A changelog entry is identified by its YAML file, which is cherry-picked
// verbatim when a change is synced from the minor branch into a fix-release
// branch. The commit that added that same file on the minor branch is therefore
// the entry's release-line origin — a signal that, unlike a whole-commit
// patch-id, is unaffected by conflict resolution or a differing set of files
// touched by the sync. The file is matched first at its exact path, then (in
// case it lives under a different changelog subdirectory on the branch — e.g. a
// version folder rather than unreleased) by basename anywhere under changelog/.
func FindFileOriginOnBranch(workingDir, branch, file string) string {
	if sha := addedCommitOnBranch(workingDir, branch, file); sha != "" {
		return sha
	}

	base := filepath.Base(file)
	if base == "" || base == "." || base == string(filepath.Separator) {
		return ""
	}

	// ":(top,glob)" anchors to the repo root (regardless of workingDir) and lets
	// "**" span directories, so the basename is matched anywhere under changelog/.
	return addedCommitOnBranch(workingDir, branch, ":(top,glob)changelog/**/"+base)
}

// RefExists reports whether ref resolves to a commit in the repository.
func RefExists(workingDir, ref string) bool {
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	cmd.Dir = workingDir
	return cmd.Run() == nil
}

// IsAncestor reports whether commit is an ancestor of (i.e. reachable from) ref.
// Returns false when either revision is unknown or the check cannot be run, so
// callers should validate ref with RefExists first when a missing ref must not
// be treated as "not an ancestor".
func IsAncestor(workingDir, commit, ref string) bool {
	cmd := exec.Command("git", "merge-base", "--is-ancestor", commit, ref)
	cmd.Dir = workingDir
	return cmd.Run() == nil
}

// FindOriginalCommit traces back through renames to find
// the commit that originally created the changelog file.
func FindOriginalCommit(workingDir, filename string) (string, error) {
	return findOriginalCommit(workingDir, filename, make(map[string]bool))
}

func findOriginalCommit(workingDir, filename string, visited map[string]bool) (string, error) {
	key := normalizePath(pathRelativeToWorkingDir(workingDir, filename))
	if visited[key] {
		return "", fmt.Errorf("cycle detected for %s", filename)
	}
	visited[key] = true

	commit := findAddedCommit(workingDir, filename)
	if commit == "" {
		commit = findOldestCommit(workingDir, filename)
		if commit == "" {
			return "", &NoCommitsFoundError{FileName: filename}
		}
	}

	oldName := findRenameSource(workingDir, commit, filename)
	if oldName == "" {
		return commit, nil
	}

	result, err := findOriginalCommit(workingDir, oldName, visited)
	if err != nil || result == "" {
		return commit, nil
	}
	return result, nil
}

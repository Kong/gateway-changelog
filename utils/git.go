package utils

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

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
	filename = pathRelativeToWorkingDir(workingDir, filename)
	cmd := exec.Command("git", "log", "--diff-filter=A",
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
	return strings.TrimSpace(lines[len(lines)-1])
}

// findOldestCommit returns the oldest commit that touched the file.
func findOldestCommit(workingDir, filename string) string {
	filename = pathRelativeToWorkingDir(workingDir, filename)
	cmd := exec.Command("git", "log", "--pretty=format:%H", "--", filename)
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
			return "", fmt.Errorf("no commits found for %s", filename)
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

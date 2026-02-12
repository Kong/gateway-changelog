package utils

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

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

	// output format: R100\told-name\tnew-name
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) == 3 && filepath.Base(parts[2]) == filepath.Base(filename) {
			oldName := filepath.Join(filepath.Dir(filename), filepath.Base(parts[1]))
			return oldName
		}
	}
	return ""
}

// findAddedCommit finds the commit where the file was first added.
func findAddedCommit(workingDir, filename string) string {
	cmd := exec.Command("git", "log", "--diff-filter=A",
		"--pretty=format:%H", "--", filename)
	cmd.Dir = workingDir
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// findOldestCommit returns the oldest commit that touched the file.
func findOldestCommit(workingDir, filename string) string {
	cmd := exec.Command("git", "log", "--pretty=format:%H", "--", filename)
	cmd.Dir = workingDir
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[len(lines)-1])
}

// FindOriginalCommit traces back through renames to find
// the commit that originally created the changelog file.
func FindOriginalCommit(workingDir, filename string) (string, error) {
	commit := findAddedCommit(workingDir, filename)
	if commit == "" {
		commit = findOldestCommit(workingDir, filename)
		if commit == "" {
			return "", fmt.Errorf("no commits found for %s", filename)
		}
		return commit, nil
	}

	oldName := findRenameSource(workingDir, commit, filename)
	if oldName == "" {
		return commit, nil
	}

	result, err := FindOriginalCommit(workingDir, oldName)
	if err != nil || result == "" {
		return commit, nil
	}
	return result, nil
}
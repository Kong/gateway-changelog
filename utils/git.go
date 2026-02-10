package utils

import (
	"os/exec"
	"strings"
)

// commitActuallyModifiedFile checks if a commit actually modified the file
// (not just a rename in the history)
func commitActuallyModifiedFile(workingDir, commit, filename string) bool {
	// git diff-tree --no-commit-id --name-only -r <commit> -- <filename>
	cmd := exec.Command("git", "diff-tree", "--no-commit-id", "--name-only", "-r", commit, "--", filename)
	cmd.Dir = workingDir
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) != ""
}

func ListCommits(workingDir, filename string) (commits []string, err error) {
	// execute command git log --follow --pretty=format:%H  -- <path>
	cmd := exec.Command("git", "log", "--follow", "--pretty=format:%H", "--", filename)
	cmd.Dir = workingDir
	output, err := cmd.Output()
	if err != nil {
		return
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		commit := strings.TrimSpace(line)
		if commit != "" && commitActuallyModifiedFile(workingDir, commit, filename) {
			commits = append(commits, commit)
		}
	}
	return
}

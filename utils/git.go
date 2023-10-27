package utils

import (
	"os/exec"
	"strings"
)

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
		commits = append(commits, strings.TrimSpace(line))
	}
	return
}

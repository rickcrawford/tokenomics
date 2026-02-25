package ledger

import (
	"os/exec"
	"strings"
)

// GitInfo holds git repository context for a session.
type GitInfo struct {
	Branch      string `json:"branch,omitempty"`
	CommitStart string `json:"commit_start,omitempty"`
	CommitEnd   string `json:"commit_end,omitempty"`
	RepoRoot    string `json:"repo_root,omitempty"`
}

// snapshotGit captures the current git branch, HEAD commit, and repo root.
// Returns an empty GitInfo (no error) if not in a git repo.
func snapshotGit() GitInfo {
	info := GitInfo{}
	info.Branch = gitOutput("rev-parse", "--abbrev-ref", "HEAD")
	info.CommitStart = gitOutput("rev-parse", "--short", "HEAD")
	info.RepoRoot = gitOutput("rev-parse", "--show-toplevel")
	return info
}

// snapshotGitEnd captures the current HEAD commit for the end of a session.
func snapshotGitEnd() string {
	return gitOutput("rev-parse", "--short", "HEAD")
}

// gitOutput runs a git command and returns trimmed stdout, or "" on error.
func gitOutput(args ...string) string {
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

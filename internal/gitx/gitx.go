// Package gitx wraps the few git queries port-keeper needs. Every function
// degrades to "unknown" when git is missing or the directory is not a repository.
package gitx

import (
	"os/exec"
	"strings"
)

// Toplevel returns the worktree root of dir, or "" if dir is not inside a git worktree.
func Toplevel(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Tracked reports whether path (inside dir's repository) is tracked by git.
func Tracked(dir, path string) bool {
	err := exec.Command("git", "-C", dir, "ls-files", "--error-unmatch", "--", path).Run()
	return err == nil
}

// Ignored reports whether path would be ignored by git. Returns false when unknown.
func Ignored(dir, path string) bool {
	err := exec.Command("git", "-C", dir, "check-ignore", "-q", "--", path).Run()
	return err == nil
}

// InRepo reports whether dir is inside a git repository.
func InRepo(dir string) bool { return Toplevel(dir) != "" }

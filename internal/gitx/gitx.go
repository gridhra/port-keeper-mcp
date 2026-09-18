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

// Branch returns the current branch name, or "" when HEAD is detached or dir is
// not a repository. symbolic-ref (unlike rev-parse --abbrev-ref) names an unborn
// branch in a fresh repository and fails, rather than printing "HEAD", when detached.
func Branch(dir string) string {
	out, err := exec.Command("git", "-C", dir, "symbolic-ref", "--short", "-q", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// TrackedFiles lists the git-tracked files under dir that match the pathspecs,
// relative to dir. It returns nil when git is missing or dir is not a repository.
func TrackedFiles(dir string, pathspecs ...string) []string {
	args := append([]string{"-C", dir, "ls-files", "-z", "--"}, pathspecs...)
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return nil
	}
	var files []string
	for _, f := range strings.Split(string(out), "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files
}

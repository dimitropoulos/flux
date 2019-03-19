package git

/*

The purpose of this package is to provide a thin wrapper around the `git` CLI. Logic that is provided directly to the Git CLI is appropriate to deal with in this package. Similarly, at this layer of abstraction it is generally inappropriate to add logic that Git would otherwise be aware of.  For example, whether or not a repo is readonly is not something you have to supply to `git push`. If you attempt to `git push` on a readonly repo, Git will return an error. And so, carrying that same behavior to this module, if you try to use the `push` function on a readonly repo, that function should return an error just as it would with the CLI.

*/

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	"context"

	"github.com/pkg/errors"
)

// trace is a flag that, if set to true, will echo every git invocation to stdout.
// This is useful for debugging and day-to-day development.
const trace = false

// Env vars that are allowed to be inherited from the parent process.
var allowedEnvVars = []string{"http_proxy", "https_proxy", "no_proxy", "HOME", "GNUPGHOME"}

type gitCmdConfig struct {
	dir string
	env []string
	out io.Writer
}

// config runs `git config` with the supplied arguments.
// See https://git-scm.com/docs/git-config for more info.
func config(ctx context.Context, workingDir, user, email string) error {
	for k, v := range map[string]string{
		"user.name":  user,
		"user.email": email,
	} {
		args := []string{"config", k, v}
		if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir}); err != nil {
			return errors.Wrap(err, "setting git config")
		}
	}
	return nil
}

// clone runs `git clone` with the supplied arguments.
// See https://git-scm.com/docs/git-clone for more info.
func clone(ctx context.Context, workingDir, repoURL, repoBranch string) (path string, err error) {
	repoPath := workingDir
	args := []string{"clone"}
	if repoBranch != "" {
		args = append(args, "--branch", repoBranch)
	}
	args = append(args, repoURL, repoPath)
	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir}); err != nil {
		return "", errors.Wrap(err, "git clone")
	}
	return repoPath, nil
}

// mirror runs downloads a given repo url to a local working directory.
// Compared to a regular clone, this also maps all refs (including remote-traking brances, notes, etc.).
// See https://git-scm.com/docs/git-clone for more info.
func mirror(ctx context.Context, workingDir, repoURL string) (path string, err error) {
	repoPath := workingDir
	args := []string{"clone", "--mirror"}
	args = append(args, repoURL, repoPath)
	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir}); err != nil {
		return "", errors.Wrap(err, "git clone --mirror")
	}
	return repoPath, nil
}

// checkout updates files in the working directory to match the given ref.
// See https://git-scm.com/docs/git-checkout for more info.
func checkout(ctx context.Context, workingDir, ref string) error {
	args := []string{"checkout", ref, "--"}
	return execGitCmd(ctx, args, gitCmdConfig{dir: workingDir})
}

// checkPush sanity-checks that we can write to the upstream repo
// (being able to `clone` is an adequate check that we can read the
// upstream).
// See https://git-scm.com/docs/git-tag and https://git-scm.com/docs/git-push for more info.
func checkPush(ctx context.Context, workingDir, upstream string) error {
	checkPushTag := "flux-write-check"

	// --force just in case we fetched the tag from upstream when cloning
	args := []string{"tag", "--force", checkPushTag}
	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir}); err != nil {
		return errors.Wrap(err, "tag for write check")
	}
	args = []string{"push", "--force", upstream, "tag", checkPushTag}
	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir}); err != nil {
		return errors.Wrap(err, "attempt to push tag")
	}
	args = []string{"push", "--delete", upstream, "tag", checkPushTag}
	return execGitCmd(ctx, args, gitCmdConfig{dir: workingDir})
}

// commit records the changes (represented by a CommitAction) to the repo.
// See https://git-scm.com/docs/git-commit for more info.
func commit(ctx context.Context, workingDir string, commitAction CommitAction) error {
	args := []string{"commit", "--no-verify", "--all", "--message", commitAction.Message}
	var env []string
	if commitAction.Author != "" {
		args = append(args, "--author", commitAction.Author)
	}
	if commitAction.SigningKey != "" {
		args = append(args, fmt.Sprintf("--gpg-sign=%s", commitAction.SigningKey))
	}
	args = append(args, "--")
	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir, env: env}); err != nil {
		return errors.Wrap(err, "git commit")
	}
	return nil
}

// push updates the remote refs using local refs.
// See https://git-scm.com/docs/git-push for more info.
func push(ctx context.Context, workingDir, upstream string, refs []string) error {
	args := append([]string{"push", upstream}, refs...)
	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir}); err != nil {
		return errors.Wrap(err, fmt.Sprintf("git push %s %s", upstream, refs))
	}
	return nil
}

// fetch updates refs from the upstream repository.
// The format of a <refspec> parameter is an optional plus +, followed by the
// source <src>, followed by a colon :, followed by the destination ref <dst>.
// The colon can be omitted when <dst> is empty. <src> is typically a ref, but
// it can also be a fully spelled hex object name.
// See https://git-scm.com/docs/git-fetch for more info.
// fetch updates refs from the upstream.
func fetch(ctx context.Context, workingDir, upstream string, refspec ...string) error {
	args := append([]string{"fetch", "--tags", upstream}, refspec...)
	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir}); err != nil &&
		!strings.Contains(err.Error(), "Couldn't find remote ref") {
		return errors.Wrap(err, fmt.Sprintf("git fetch --tags %s %s", upstream, refspec))
	}
	return nil
}

// refExists validates that a certain ref exists in the local git tree.
// See https://git-scm.com/docs/git-rev-list for more info.
func refExists(ctx context.Context, workingDir, ref string) (bool, error) {
	args := []string{"rev-list", ref, "--"}
	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir}); err != nil {
		if strings.Contains(err.Error(), "bad revision") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Get the full ref for a shorthand notes ref.
// See https://git-scm.com/docs/git-notes for more info.
func getNotesRef(ctx context.Context, workingDir, ref string) (string, error) {
	out := &bytes.Buffer{}
	args := []string{"notes", "--ref", ref, "get-ref"}
	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir, out: out}); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

// addNote creates a note at a certain note ref.
// See https://git-scm.com/docs/git-notes for more info.
func addNote(ctx context.Context, workingDir, rev, notesRef string, note interface{}) error {
	b, err := json.Marshal(note)
	if err != nil {
		return err
	}
	args := []string{"notes", "--ref", notesRef, "add", "--message", string(b), rev}
	return execGitCmd(ctx, args, gitCmdConfig{dir: workingDir})
}

// getNote returns a note held at a certain git ref.
// See https://git-scm.com/docs/git-notes for more info.
func getNote(ctx context.Context, workingDir, notesRef, rev string, note interface{}) (ok bool, err error) {
	out := &bytes.Buffer{}
	args := []string{"notes", "--ref", notesRef, "show", rev}
	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir, out: out}); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no note found for object") {
			return false, nil
		}
		return false, err
	}
	if err := json.NewDecoder(out).Decode(note); err != nil {
		return false, err
	}
	return true, nil
}

// Get all revisions with a note (NB: DO NOT RELY ON THE ORDERING)
// It appears to be ordered by ascending git object ref, not by time.
// Return a map to make it easier to do "if in" type queries.
// See https://git-scm.com/docs/git-notes for more info.
func noteRevList(ctx context.Context, workingDir, notesRef string) (map[string]struct{}, error) {
	out := &bytes.Buffer{}
	args := []string{"notes", "--ref", notesRef, "list"}
	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir, out: out}); err != nil {
		return nil, err
	}
	noteList := splitList(out.String())
	result := make(map[string]struct{}, len(noteList))
	for _, l := range noteList {
		split := strings.Fields(l)
		if len(split) > 0 {
			result[split[1]] = struct{}{} // First field contains the object ref (commit id in our case)
		}
	}
	return result, nil
}

// Get the commit hash for a reference.
// See https://git-scm.com/docs/git-rev-list for more info.
func refRevision(ctx context.Context, workingDir, ref string) (string, error) {
	out := &bytes.Buffer{}
	args := []string{"rev-list", "--max-count", "1", ref, "--"}
	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir, out: out}); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

// onelinelog returns the revisions and one-line log commit messages.
// See https://git-scm.com/docs/git-log for more info.
func onelinelog(ctx context.Context, workingDir, refspec string, subdirs []string) ([]Commit, error) {
	out := &bytes.Buffer{}
	args := []string{"log", "--pretty=format:%GK|%H|%s", refspec}
	args = append(args, "--")
	if len(subdirs) > 0 {
		args = append(args, subdirs...)
	}

	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir, out: out}); err != nil {
		return nil, err
	}

	return splitLog(out.String())
}

func splitLog(s string) ([]Commit, error) {
	lines := splitList(s)
	commits := make([]Commit, len(lines))
	for i, m := range lines {
		parts := strings.SplitN(m, "|", 3)
		commits[i].SigningKey = parts[0]
		commits[i].Revision = parts[1]
		commits[i].Message = parts[2]
	}
	return commits, nil
}

func splitList(s string) []string {
	outStr := strings.TrimSpace(s)
	if outStr == "" {
		return []string{}
	}
	return strings.Split(outStr, "\n")
}

// Move the tag to the ref given and push that tag upstream
func moveTagAndPush(ctx context.Context, workingDir, tag, upstream string, tagAction TagAction) error {
	args := []string{"tag", "--force", "-a", "-m", tagAction.Message}
	var env []string
	if tagAction.SigningKey != "" {
		args = append(args, fmt.Sprintf("--local-user=%s", tagAction.SigningKey))
	}
	args = append(args, tag, tagAction.Revision)
	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir, env: env}); err != nil {
		return errors.Wrap(err, "moving tag "+tag)
	}
	args = []string{"push", "--force", upstream, "tag", tag}
	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir}); err != nil {
		return errors.Wrap(err, "pushing tag to origin")
	}
	return nil
}

// verifyTag checks the GPG signature of a tag.
// See https://git-scm.com/docs/git-verify-tag for more info.
func verifyTag(ctx context.Context, workingDir, tag string) error {
	args := []string{"verify-tag", tag}
	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir}); err != nil {
		return errors.Wrap(err, "verifying tag "+tag)
	}
	return nil
}

// changed shows the names of changed files.
// See https://git-scm.com/docs/git-verify-tag for more info.
func changed(ctx context.Context, workingDir, ref string, subPaths []string) ([]string, error) {
	out := &bytes.Buffer{}
	// This uses --diff-filter to only look at changes for file _in the working dir_; i.e, we do not report on things that no longer appear.
	// Specifically this filter selects files that were Added (A), Changed (C), Modified (M), Renamed (R), have their Type (i.e. regular file, symlink, submodule, ...) changed (T).  By process of elimination this ignores files that are Deleted (D), Unmerged (U), Unknown (X), or have had their paring Broken (B).
	args := []string{"diff", "--name-only", "--diff-filter=ACMRT", ref}
	args = append(args, "--")
	if len(subPaths) > 0 {
		args = append(args, subPaths...)
	}

	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir, out: out}); err != nil {
		return nil, err
	}
	return splitList(out.String()), nil
}

func execGitCmd(ctx context.Context, args []string, config gitCmdConfig) error {
	if trace {
		print("TRACE: git")
		for _, arg := range args {
			print(` "`, arg, `"`)
		}
		println()
	}

	c := exec.CommandContext(ctx, "git", args...)

	if config.dir != "" {
		c.Dir = config.dir
	}
	c.Env = append(env(), config.env...)
	c.Stdout = ioutil.Discard
	if config.out != nil {
		c.Stdout = config.out
	}
	errOut := &bytes.Buffer{}
	c.Stderr = errOut

	err := c.Run()
	if err != nil {
		msg := findErrorMessage(errOut)
		if msg != "" {
			err = errors.New(msg)
		}
	}
	if ctx.Err() == context.DeadlineExceeded {
		return errors.Wrap(ctx.Err(), fmt.Sprintf("running git command: %s %v", "git", args))
	} else if ctx.Err() == context.Canceled {
		return errors.Wrap(ctx.Err(), fmt.Sprintf("context was unexpectedly cancelled when running git command: %s %v", "git", args))
	}
	return err
}

func env() []string {
	env := []string{"GIT_TERMINAL_PROMPT=0"}

	// include allowed env vars from os
	for _, k := range allowedEnvVars {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}

	return env
}

// check returns true if there are changes locally.
// See https://git-scm.com/docs/git-diff for more info.
func check(ctx context.Context, workingDir string, subdirs []string) bool {
	// `--quiet` means "exit with 1 if there are changes"
	args := []string{"diff", "--quiet"}
	args = append(args, "--")
	if len(subdirs) > 0 {
		args = append(args, subdirs...)
	}
	return execGitCmd(ctx, args, gitCmdConfig{dir: workingDir}) != nil
}

func findErrorMessage(output io.Reader) string {
	sc := bufio.NewScanner(output)
	for sc.Scan() {
		switch {
		case strings.HasPrefix(sc.Text(), "fatal: "):
			return sc.Text()
		case strings.HasPrefix(sc.Text(), "ERROR fatal: "): // Saw this error on ubuntu systems
			return sc.Text()
		case strings.HasPrefix(sc.Text(), "error:"):
			return strings.Trim(sc.Text(), "error: ")
		}
	}
	return ""
}

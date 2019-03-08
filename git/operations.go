package git

/*

The purpose of this package is to provide a thin wrapper around the `git` CLI.

At this layer of abstraction, it is generally inappropriate to add logic to this file that git would otherwise be aware of.  For example, whether or not a repo is readonly is not something you have to supply to `git push`.  If you attempt to `git push` on a readonly repo, git will return an error.  And so, carrying that same behavior to this module, if you try to use the `push` function on a readonly repo, that function should return an error.

*/

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"context"

	"github.com/pkg/errors"
)

// trace is a flag that, if set to true, will echo every git invocation to stdout.
// This is useful for debugging and day-to-day development.
const trace = true

// Env vars that are allowed to be inherited from the os
var allowedEnvVars = []string{"http_proxy", "https_proxy", "no_proxy", "HOME", "GNUPGHOME"}

type gitCmdConfig struct {
	dir string
	env []string
	out io.Writer
}

func config(ctx context.Context, workingDir string, user string, email string) error {
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

func clone(ctx context.Context, workingDir string, repoURL string, repoBranch GitRef) (path string, err error) {
	repoPath := workingDir
	args := []string{"clone"}
	if repoBranch != "" {
		args = append(args, "--branch", string(repoBranch))
	}
	args = append(args, repoURL, repoPath)
	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir}); err != nil {
		return "", errors.Wrap(err, "git clone")
	}
	return repoPath, nil
}

func mirror(ctx context.Context, workingDir string, repoURL string) (path string, err error) {
	repoPath := workingDir
	args := []string{"clone", "--mirror"}
	args = append(args, repoURL, repoPath)
	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir}); err != nil {
		return "", errors.Wrap(err, "git clone --mirror")
	}
	return repoPath, nil
}

func checkout(ctx context.Context, workingDir string, ref GitRef) error {
	args := []string{"checkout", string(ref)}
	return execGitCmd(ctx, args, gitCmdConfig{dir: workingDir})
}

// checkPush sanity-checks that we can write to the upstream repo
// (being able to `clone` is an adequate check that we can read the
// upstream).
func checkPush(ctx context.Context, workingDir string, upstreamURL string) error {
	checkPushTag := "flux-write-check"

	// --force just in case we fetched the tag from upstream when cloning
	args := []string{"tag", "--force", checkPushTag}
	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir}); err != nil {
		return errors.Wrap(err, "tag for write check")
	}
	args = []string{"push", "--force", upstreamURL, "tag", checkPushTag}
	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir}); err != nil {
		return errors.Wrap(err, "attempt to push tag")
	}
	args = []string{"push", "--delete", upstreamURL, "tag", checkPushTag}
	return execGitCmd(ctx, args, gitCmdConfig{dir: workingDir})
}

func commit(ctx context.Context, workingDir string, commitAction CommitAction) error {
	args := []string{"commit", "--no-verify", "--all", "--message", commitAction.Message}
	var env []string
	if commitAction.Author != "" {
		args = append(args, "--author", commitAction.Author)
	}
	if commitAction.SigningKey != "" {
		args = append(args, fmt.Sprintf("--gpg-sign=%s", commitAction.SigningKey))
	}
	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir, env: env}); err != nil {
		return errors.Wrap(err, "git commit")
	}
	return nil
}

// push the refs given to the upstream repo
func push(ctx context.Context, workingDir, upstream string, refs []GitRef) error {
	args := []string{"push", upstream}
	for _, ref := range refs {
		args = append(args, string(ref))
	}

	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir}); err != nil {
		return errors.Wrap(err, fmt.Sprintf("git push %s %s", upstream, refs))
	}
	return nil
}

// fetch updates refs from the upstream.
func fetch(ctx context.Context, workingDir, upstream string, refspec ...string) error {
	args := append([]string{"fetch", "--tags", upstream}, refspec...)
	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir}); err != nil &&
		!strings.Contains(err.Error(), "Couldn't find remote ref") {
		return errors.Wrap(err, fmt.Sprintf("git fetch --tags %s %s", upstream, refspec))
	}
	return nil
}

func refExists(ctx context.Context, workingDir string, ref GitRef) (bool, error) {
	args := []string{"rev-list", string(ref)}
	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir}); err != nil {
		if strings.Contains(err.Error(), "unknown revision") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

type GitRef string

// Get the full ref for a shorthand notes ref.
func getNotesRef(ctx context.Context, workingDir string, notesRef GitRef) (GitRef, error) {
	out := &bytes.Buffer{}
	args := []string{"notes", "--ref", string(notesRef), "get-ref"}
	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir, out: out}); err != nil {
		return "", err
	}
	result := GitRef(strings.TrimSpace(out.String()))
	fmt.Println("GIT COMMAND getNotesRef: " + result)
	return result, nil
}

func addNote(ctx context.Context, workingDir string, rev GitRef, notesRef GitRef, note interface{}) error {
	b, err := json.Marshal(note)
	if err != nil {
		return err
	}
	args := []string{"notes", "--ref", string(notesRef), "add", "--message", string(b), string(rev)}
	out := &bytes.Buffer{}
	err = execGitCmd(ctx, args, gitCmdConfig{dir: workingDir, out: out})
	fmt.Println("GIT COMMAND addNote: " + strings.TrimSpace(out.String()))
	return err
}

func getNote(ctx context.Context, workingDir string, notesRef GitRef, rev GitRef, note interface{}) (ok bool, err error) {
	out := &bytes.Buffer{}
	args := []string{"notes", "--ref", string(notesRef), "show", string(rev)}
	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir, out: out}); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no note found for object") {
			return false, nil
		}
		return false, err
	}
	fmt.Println("GIT COMMAND getNote: " + strings.TrimSpace(out.String()))
	if err := json.NewDecoder(out).Decode(note); err != nil {
		return false, err
	}
	return true, nil
}

// Get all revisions with a note (NB: DO NOT RELY ON THE ORDERING)
// It appears to be ordered by ascending git object ref, not by time.
// Return a map to make it easier to do "if in"-type queries.
func noteRevList(ctx context.Context, workingDir string, notesRef GitRef) (map[GitRef]struct{}, error) {
	out := &bytes.Buffer{}
	args := []string{"notes", "--ref", string(notesRef), "list"}
	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir, out: out}); err != nil {
		return nil, err
	}
	noteList := splitList(out.String())
	result := make(map[GitRef]struct{}, len(noteList))
	for _, l := range noteList {
		split := strings.Fields(l)
		if len(split) > 0 {
			result[GitRef(split[1])] = struct{}{} // First field contains the object ref (commit id in our case)
		}
	}
	return result, nil
}

// Get the commit hash for a reference
func refRevision(ctx context.Context, workingDir string, ref GitRef) (GitRef, error) {
	out := &bytes.Buffer{}
	args := []string{"rev-list", "--max-count", "1", string(ref)}
	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir, out: out}); err != nil {
		return "", err
	}
	return GitRef(strings.TrimSpace(out.String())), nil
}

func revlist(ctx context.Context, workingDir string, ref GitRef) ([]GitRef, error) {
	out := &bytes.Buffer{}
	args := []string{"rev-list", string(ref)}
	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir, out: out}); err != nil {
		return nil, err
	}
	resultStrings := strings.TrimSpace(out.String())
	result := []GitRef{}
	for _, s := range resultStrings {
		result = append(result, GitRef(s))
	}
	return result, nil
}

// Return the revisions and one-line log commit messages
func onelinelog(ctx context.Context, workingDir string, refspec GitRef, subdirs []string) ([]Commit, error) {
	out := &bytes.Buffer{}
	args := []string{
		"log",
		"--pretty=format:%GK|%H|%s",
		string(refspec),
	}
	if len(subdirs) > 0 {
		args = append(args, "--")
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
		commits[i].Revision = GitRef(parts[1])
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
func moveTagAndPush(ctx context.Context, workingDir string, tag GitRef, upstream string, syncMarkerAction SyncMarkerAction) error {
	args := []string{"tag", "--force", "--annotate", "--message", syncMarkerAction.Message}
	var env []string
	if syncMarkerAction.SigningKey != "" {
		args = append(args, fmt.Sprintf("--local-user=%s", syncMarkerAction.SigningKey))
	}
	args = append(args, string(tag), string(syncMarkerAction.Revision))
	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir, env: env}); err != nil {
		return errors.Wrap(err, "moving tag "+string(tag))
	}
	args = []string{"push", "--force", upstream, "tag", string(tag)}
	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir}); err != nil {
		return errors.Wrap(err, "pushing tag to origin")
	}
	return nil
}

func verifyTag(ctx context.Context, workingDir string, tag GitRef) error {
	var env []string
	args := []string{"verify-tag", string(tag)}
	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir, env: env}); err != nil {
		return errors.Wrap(err, "verifying tag "+string(tag))
	}
	return nil
}

func changed(ctx context.Context, workingDir string, ref GitRef, subPaths []string) ([]string, error) {
	out := &bytes.Buffer{}
	// This uses --diff-filter to only look at changes for file _in
	// the working dir_; i.e, we do not report on things that no
	// longer appear.
	args := []string{"diff", "--name-only", "--diff-filter=ACMRT", string(ref)}
	if len(subPaths) > 0 {
		args = append(args, "--")
		args = append(args, subPaths...)
	}

	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir, out: out}); err != nil {
		return nil, err
	}
	result := splitList(out.String())
	return result, nil
}

func execGitCmd(ctx context.Context, args []string, config gitCmdConfig) error {
	if trace {
		print("TRACE: git")
		for _, arg := range args {
			print(` "`, arg, `"`)
		}
		println()
	}

	// READONLY-NOTE: disabling notes commands for now (temporarily) DO NOT MERGE
	// if args[0] == "notes" {
	// 	print("DEBUG: not running `notes` commands\n")
	// 	return nil
	// }

	c := exec.CommandContext(ctx, "git", args...)

	if config.dir != "" {
		c.Dir = config.dir
	}
	c.Env = append(env(), config.env...)

	c.Stdout = &bytes.Buffer{}
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
func check(ctx context.Context, workingDir string, subdirs []string) bool {
	// `--quiet` means "exit with 1 if there are changes"
	args := []string{"diff", "--quiet"}
	if len(subdirs) > 0 {
		args = append(args, "--")
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

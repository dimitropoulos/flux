package git

import (
	"context"
	"os"
	"path/filepath"
)

// Config holds some values we use when working in the working clone of
// a repo.
type Config struct {
	Branch         GitRef   // branch we're syncing to
	Paths          []string // paths within the repo containing files we care about
	ReadOnly       bool     // Flux can read but not write to the git repo
	SyncMarkerName GitRef
	NotesRef       GitRef
	UserName       string
	UserEmail      string
	SigningKey     string
	SetAuthor      bool
	SkipMessage    string
}

// Checkout is a local working clone of the remote repo. It is
// intended to be used for one-off "transactions", e.g,. committing
// changes then pushing upstream. It has no locking.
type Checkout struct {
	dir          string
	config       Config
	upstream     Remote
	realNotesRef GitRef // cache the notes ref, since we use it to push as well
}

// Commit refers to a git commit
type Commit struct {
	SigningKey string
	Revision   GitRef
	Message    string
}

// CommitAction - struct holding commit information
type CommitAction struct {
	Author     string
	Message    string
	SigningKey string
}

// SyncMarkerAction - struct holding tag information
type SyncMarkerAction struct {
	Revision   GitRef
	Message    string
	SigningKey string
}

// Clean a Checkout up (remove the clone)
func (c *Checkout) Clean() {
	if c.dir != "" {
		os.RemoveAll(c.dir)
	}
}

// Dir returns the path to the repo
func (c *Checkout) Dir() string {
	return c.dir
}

// ManifestDirs returns the paths to the manifests files. It ensures
// that at least one path is returned, so that it can be used with
// `Manifest.LoadManifests`.
func (c *Checkout) ManifestDirs() []string {
	if len(c.config.Paths) == 0 {
		return []string{c.dir}
	}

	paths := make([]string, len(c.config.Paths), len(c.config.Paths))
	for i, p := range c.config.Paths {
		paths[i] = filepath.Join(c.dir, p)
	}
	return paths
}

// CommitAndPush commits changes made in this checkout, along with any
// extra data as a note, and pushes the commit and note to the remote repo.
func (c *Checkout) CommitAndPush(ctx context.Context, commitAction CommitAction, note interface{}) error {
	if !check(ctx, c.dir, c.config.Paths) {
		return ErrNoChanges
	}

	commitAction.Message += c.config.SkipMessage
	if commitAction.SigningKey == "" {
		commitAction.SigningKey = c.config.SigningKey
	}

	if err := commit(ctx, c.dir, commitAction); err != nil {
		return err
	}

	if note != nil {
		rev, err := c.HeadRevision(ctx)
		if err != nil {
			return err
		}
		if err := addNote(ctx, c.dir, rev, c.config.NotesRef, note); err != nil {
			return err
		}
	}

	refs := []GitRef{
		GitRef(c.config.Branch), // READONLY-NOTE: TODO: is this a bug?
	}
	ok, err := refExists(ctx, c.dir, c.realNotesRef)
	if ok {
		refs = append(refs, c.realNotesRef)
	} else if err != nil {
		return err
	}

	if err := push(ctx, c.dir, c.upstream.URL, refs); err != nil {
		return PushError(c.upstream.URL, err)
	}
	return nil
}

// GetNote gets a note for the revision specified, or nil if there is no such note.
func (c *Checkout) GetNote(ctx context.Context, rev GitRef, note interface{}) (bool, error) {
	return getNote(ctx, c.dir, c.realNotesRef, rev, note)
}

// HeadRevision returns the revision of the current git HEAD
func (c *Checkout) HeadRevision(ctx context.Context) (GitRef, error) {
	return refRevision(ctx, c.dir, "HEAD")
}

// SyncMarkerRevision returns the revision of the SyncMarker
func (c *Checkout) SyncMarkerRevision(ctx context.Context) (GitRef, error) {
	if c.config.ReadOnly {
		return getConfigMapSyncMarkerRevision(ctx)
	}

	return refRevision(ctx, c.dir, c.config.SyncMarkerName)
}

// UpdateSyncMarker updates the state that Flux relies on for keeping track of the so-called "High Water Mark" for which commits Flux has reconciled.
func (c *Checkout) UpdateSyncMarker(ctx context.Context, syncMarkerAction SyncMarkerAction) error {
	if syncMarkerAction.SigningKey == "" {
		syncMarkerAction.SigningKey = c.config.SigningKey
	}
	if c.config.ReadOnly {
		return updateConfigMapState(ctx, c.config.SyncMarkerName, syncMarkerAction)
	}
	return moveTagAndPush(ctx, c.dir, c.config.SyncMarkerName, c.upstream.URL, syncMarkerAction)
}

// VerifySyncTag validates the gpg signature created by git tag.
func (c *Checkout) VerifySyncTag(ctx context.Context) error {
	return verifyTag(ctx, c.dir, c.config.SyncMarkerName)
}

// ChangedFiles does a git diff listing changed files
func (c *Checkout) ChangedFiles(ctx context.Context, ref GitRef) ([]string, error) {
	list, err := changed(ctx, c.dir, ref, c.config.Paths)
	if err == nil {
		for i, file := range list {
			list[i] = filepath.Join(c.dir, string(file))
		}
	}
	return list, err
}

func (c *Checkout) NoteRevList(ctx context.Context) (map[GitRef]struct{}, error) {
	return noteRevList(ctx, c.dir, c.realNotesRef)
}

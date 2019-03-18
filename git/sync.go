package git

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	fluxsync "github.com/weaveworks/flux/sync"
)

type GitTagSyncProvider struct {
	workingDir  string
	syncTag     string
	upstreamURL string
	signingKey  string
	userName    string
	userEmail   string
}

// NewGitTagSyncProvider creates a new git tag sync provider
func NewGitTagSyncProvider(
	workingDir string,
	syncTag string,
	upstreamURL string,
	signingKey string,
	userName string,
	userEmail string,
) GitTagSyncProvider {
	return GitTagSyncProvider{
		workingDir:  workingDir,
		syncTag:     syncTag,
		upstreamURL: upstreamURL,
		signingKey:  signingKey,
		userName:    userName,
		userEmail:   userEmail,
	}
}

// GetRevision returns the revision of the git commit where the flux sync tag is currently positioned
func (p GitTagSyncProvider) GetRevision(ctx context.Context) (string, error) {
	return refRevision(ctx, p.workingDir, p.syncTag)
}

// UpdateMarker updates the state that Flux relies on for keeping track of the so-called "High Water Mark" for which commits Flux has reconciled.
func (p GitTagSyncProvider) UpdateMarker(ctx context.Context, syncMarkerAction fluxsync.SyncMarkerAction) error {
	workingDir := p.workingDir
	tag := p.syncTag
	upstream := p.upstreamURL

	if syncMarkerAction.SigningKey == "" {
		syncMarkerAction.SigningKey = p.signingKey
	}

	config(ctx, p.workingDir, p.userName, p.userEmail)

	args := []string{"tag", "--force", "--annotate", "--message", syncMarkerAction.Message}
	var env []string
	if syncMarkerAction.SigningKey != "" {
		args = append(args, fmt.Sprintf("--local-user=%s", syncMarkerAction.SigningKey))
	}
	args = append(args, tag, syncMarkerAction.Revision)
	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir, env: env}); err != nil {
		return errors.Wrap(err, "moving tag "+tag)
	}
	args = []string{"push", "--force", upstream, "tag", tag}
	if err := execGitCmd(ctx, args, gitCmdConfig{dir: workingDir}); err != nil {
		return errors.Wrap(err, "pushing tag to origin")
	}

	return nil
}

// DeleteMarker removes the Git Tag used for syncing
func (p GitTagSyncProvider) DeleteMarker(ctx context.Context) error {
	return deleteTag(ctx, p.workingDir, p.upstreamURL, p.syncTag)
}

// VerifySyncTag validates the gpg signature created by git tag.
func (p GitTagSyncProvider) VerifySyncTag(ctx context.Context) error {
	return verifyTag(ctx, p.workingDir, p.syncTag)
}

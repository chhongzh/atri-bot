package character

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

type RemoteProvider struct {
	id     string
	url    string
	branch string
	root   string
}

func NewRemoteProvider(id, url, branch, root string) *RemoteProvider {
	return &RemoteProvider{id: id, url: normalizeGitURL(url), branch: branch, root: root}
}

func (p *RemoteProvider) ID() string {
	return p.id
}

func (p *RemoteProvider) Load(ctx context.Context) ([]*Character, error) {
	if err := os.MkdirAll(filepath.Dir(p.root), 0o755); err != nil {
		return nil, err
	}
	repository, err := git.PlainOpen(p.root)
	if errors.Is(err, git.ErrRepositoryNotExists) {
		options := &git.CloneOptions{
			URL:          p.url,
			SingleBranch: p.branch != "",
			Depth:        1,
		}
		if p.branch != "" {
			options.ReferenceName = plumbing.NewBranchReferenceName(p.branch)
		}
		repository, err = git.PlainCloneContext(ctx, p.root, false, options)
	}
	if err != nil {
		return nil, err
	}

	worktree, err := repository.Worktree()
	if err != nil {
		return nil, err
	}
	pullOptions := &git.PullOptions{RemoteName: "origin", SingleBranch: p.branch != ""}
	if p.branch != "" {
		pullOptions.ReferenceName = plumbing.NewBranchReferenceName(p.branch)
	}
	err = worktree.PullContext(ctx, pullOptions)
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return nil, err
	}

	root := filepath.Join(p.root, "chardefs")
	if _, statErr := os.Stat(root); os.IsNotExist(statErr) {
		root = p.root
	}
	return NewLocalProvider(p.id, root).Load(ctx)
}

func normalizeGitURL(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "github.com/") {
		return "https://" + value
	}
	return value
}

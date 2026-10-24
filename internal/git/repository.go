package git

import (
	"fmt"
	"strings"

	"github.com/go-git/go-git/v5"
)

type (
	RepositoryInfo struct {
		Ref     string
		Hash    string
		Author  string
		Message string
		Remote  string
	}

	// Commit は出力に載せるコミットの要点。Message は1行目だけ。
	Commit struct {
		Hash    string
		Message string
		Ref     string `json:",omitempty"`
	}
)

// Commit は RepositoryInfo の要点を返す。コミットが分からなければ nil。
func (r *RepositoryInfo) Commit() *Commit {
	if r == nil || r.Hash == "" {
		return nil
	}
	msg, _, _ := strings.Cut(strings.TrimSpace(r.Message), "\n")
	return &Commit{Hash: r.Hash, Message: strings.TrimSpace(msg), Ref: r.Ref}
}

func (c *Commit) ShortHash() string {
	if len(c.Hash) > 7 {
		return c.Hash[:7]
	}
	return c.Hash
}

// String は「a1b2c3d "メッセージ" (refs/heads/x)」。
func (c *Commit) String() string {
	if c == nil {
		return "（なし）"
	}
	s := fmt.Sprintf("%s %q", c.ShortHash(), c.Message)
	if c.Ref != "" {
		s += " (" + c.Ref + ")"
	}
	return s
}

func GetInfo(repoDir string) (*RepositoryInfo, error) {
	repo, err := git.PlainOpenWithOptions(repoDir, &git.PlainOpenOptions{
		DetectDotGit: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open git repo: %w", err)
	}

	ref, err := repo.Head()
	if err != nil {
		return nil, fmt.Errorf("failed to get head: %w", err)
	}

	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return nil, fmt.Errorf("failed to get commit: %w", err)
	}

	remote, err := repo.Remote("origin")
	if err != nil {
		return nil, fmt.Errorf("failed to get remote: %w", err)
	}

	remoteUrl := ""
	if len(remote.Config().URLs) > 0 {
		remoteUrl = remote.Config().URLs[0]
	}

	return &RepositoryInfo{
		Ref:     ref.Name().String(),
		Hash:    commit.Hash.String(),
		Author:  commit.Author.String(),
		Message: commit.Message,
		Remote:  remoteUrl,
	}, nil
}

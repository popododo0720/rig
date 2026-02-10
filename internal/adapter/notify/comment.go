package notify

import (
	"context"
	"fmt"

	"github.com/rigdev/rig/internal/core"
)

// CommentPoster is the minimal dependency needed by CommentNotifier.
type CommentPoster interface {
	PostComment(ctx context.Context, owner, repo string, number int, body string) error
}

// CommentNotifier sends notifications as issue comments via GitAdapter.
type CommentNotifier struct {
	adapter CommentPoster
	owner   string
	repo    string
	number  int
}

var _ core.NotifierIface = (*CommentNotifier)(nil)

// NewCommentNotifier creates a new CommentNotifier.
func NewCommentNotifier(adapter CommentPoster, owner, repo string, number int) *CommentNotifier {
	return &CommentNotifier{
		adapter: adapter,
		owner:   owner,
		repo:    repo,
		number:  number,
	}
}

// Notify posts a comment on the configured issue/PR.
func (c *CommentNotifier) Notify(ctx context.Context, message string) error {
	body := fmt.Sprintf("**[rig]** %s", message)
	return c.adapter.PostComment(ctx, c.owner, c.repo, c.number, body)
}

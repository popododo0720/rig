package notify

import (
	"context"
	"fmt"
	"testing"
)

// mockGitAdapter is a test double for CommentPoster.
type mockGitAdapter struct {
	postCommentCalls []postCommentCall
	postCommentErr   error
}

type postCommentCall struct {
	owner  string
	repo   string
	number int
	body   string
}

func (m *mockGitAdapter) PostComment(ctx context.Context, owner, repo string, number int, body string) error {
	m.postCommentCalls = append(m.postCommentCalls, postCommentCall{
		owner:  owner,
		repo:   repo,
		number: number,
		body:   body,
	})
	return m.postCommentErr
}

func TestCommentNotifySuccess(t *testing.T) {
	mock := &mockGitAdapter{}
	notifier := NewCommentNotifier(mock, "myorg", "myrepo", 42)

	err := notifier.Notify(context.Background(), "Task started")
	if err != nil {
		t.Fatalf("Notify failed: %v", err)
	}

	if len(mock.postCommentCalls) != 1 {
		t.Fatalf("Expected 1 PostComment call, got %d", len(mock.postCommentCalls))
	}

	call := mock.postCommentCalls[0]
	if call.owner != "myorg" {
		t.Errorf("Expected owner 'myorg', got %q", call.owner)
	}
	if call.repo != "myrepo" {
		t.Errorf("Expected repo 'myrepo', got %q", call.repo)
	}
	if call.number != 42 {
		t.Errorf("Expected number 42, got %d", call.number)
	}
	expected := "**[rig]** Task started"
	if call.body != expected {
		t.Errorf("Expected body %q, got %q", expected, call.body)
	}
}

func TestCommentNotifyError(t *testing.T) {
	mock := &mockGitAdapter{
		postCommentErr: fmt.Errorf("API rate limit exceeded"),
	}
	notifier := NewCommentNotifier(mock, "myorg", "myrepo", 10)

	err := notifier.Notify(context.Background(), "deploy complete")
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if err.Error() != "API rate limit exceeded" {
		t.Errorf("Expected API rate limit error, got: %v", err)
	}
}

func TestCommentNotifyFormatsMessage(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    string
	}{
		{
			name:    "simple message",
			message: "Processing issue",
			want:    "**[rig]** Processing issue",
		},
		{
			name:    "empty message",
			message: "",
			want:    "**[rig]** ",
		},
		{
			name:    "multiline message",
			message: "Line1\nLine2",
			want:    "**[rig]** Line1\nLine2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockGitAdapter{}
			notifier := NewCommentNotifier(mock, "o", "r", 1)

			err := notifier.Notify(context.Background(), tt.message)
			if err != nil {
				t.Fatalf("Notify failed: %v", err)
			}

			if mock.postCommentCalls[0].body != tt.want {
				t.Errorf("Expected %q, got %q", tt.want, mock.postCommentCalls[0].body)
			}
		})
	}
}

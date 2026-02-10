package notify

import "context"

// Notifier defines the interface for sending notifications.
type Notifier interface {
	// Notify sends a notification message.
	Notify(ctx context.Context, message string) error
}

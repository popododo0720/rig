package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/rigdev/rig/internal/config"
	"github.com/rigdev/rig/internal/core"
)

// ExecuteFunc is a callback invoked when a valid webhook event is accepted.
// It receives the parsed issue and the raw event action string.
type ExecuteFunc func(issue core.Issue) error

// Handler processes incoming GitHub webhook events.
type Handler struct {
	secret    string
	triggers  []config.TriggerConfig
	statePath string
	onExecute ExecuteFunc
}

// NewHandler creates a new webhook Handler.
func NewHandler(secret string, triggers []config.TriggerConfig, statePath string, onExecute ExecuteFunc) *Handler {
	return &Handler{
		secret:    secret,
		triggers:  triggers,
		statePath: statePath,
		onExecute: onExecute,
	}
}

// HandleWebhook is the HTTP handler for POST /webhook.
func (h *Handler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Verify HMAC-SHA256 signature.
	signature := r.Header.Get("X-Hub-Signature-256")
	if !h.verifySignature(body, signature) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	// Determine event type.
	eventType := r.Header.Get("X-GitHub-Event")
	if eventType == "" {
		http.Error(w, "missing event type", http.StatusBadRequest)
		return
	}

	// Parse the payload.
	event, err := h.parseEvent(eventType, body)
	if err != nil {
		log.Printf("failed to parse event: %v", err)
		http.Error(w, "failed to parse event", http.StatusBadRequest)
		return
	}

	// Check if the event action is one we care about.
	action := fmt.Sprintf("%s.%s", eventType, event.Action)
	if !h.isTrackedAction(action) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "event %s ignored", action)
		return
	}

	// Check trigger filters (labels/keywords).
	if !h.matchesTrigger(action, event) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "event %s did not match trigger filters", action)
		return
	}

	// Build core.Issue from the webhook event.
	issue := core.Issue{
		Platform: "github",
		Repo:     event.RepoFullName,
		ID:       fmt.Sprintf("%d", event.IssueNumber),
		Title:    event.IssueTitle,
		URL:      event.IssueURL,
	}

	// Check for in-flight duplicates via state.json.
	state, err := core.LoadState(h.statePath)
	if err != nil {
		log.Printf("failed to load state: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if state.IsInFlight(issue.ID) {
		log.Printf("issue %s already in-flight, skipping", issue.ID)
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "issue %s already in-flight", issue.ID)
		return
	}

	// Invoke engine.Execute placeholder.
	if h.onExecute != nil {
		if err := h.onExecute(issue); err != nil {
			log.Printf("execute failed for issue %s: %v", issue.ID, err)
			http.Error(w, "execution failed", http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusAccepted)
	fmt.Fprintf(w, "accepted issue %s", issue.ID)
}

// verifySignature checks the HMAC-SHA256 signature from GitHub.
func (h *Handler) verifySignature(body []byte, signature string) bool {
	if h.secret == "" {
		log.Println("[webhook] WARNING: no webhook secret configured — rejecting request for safety")
		return false // Reject if no secret configured — require explicit opt-in.
	}

	if signature == "" {
		return false
	}

	// GitHub sends "sha256=<hex>".
	prefix := "sha256="
	if !strings.HasPrefix(signature, prefix) {
		return false
	}
	sigHex := signature[len(prefix):]

	mac := hmac.New(sha256.New, []byte(h.secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(sigHex), []byte(expected))
}

// webhookEvent is an intermediate representation of a GitHub webhook payload.
type webhookEvent struct {
	Action       string
	IssueNumber  int
	IssueTitle   string
	IssueURL     string
	IssueLabels  []string
	RepoFullName string
	CommentBody  string
}

// parseEvent extracts relevant fields from a GitHub webhook payload.
func (h *Handler) parseEvent(eventType string, body []byte) (*webhookEvent, error) {
	var raw struct {
		Action string `json:"action"`
		Issue  struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
			URL    string `json:"html_url"`
			Labels []struct {
				Name string `json:"name"`
			} `json:"labels"`
		} `json:"issue"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		Comment struct {
			Body string `json:"body"`
		} `json:"comment"`
	}

	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal webhook: %w", err)
	}

	labels := make([]string, 0, len(raw.Issue.Labels))
	for _, l := range raw.Issue.Labels {
		labels = append(labels, l.Name)
	}

	return &webhookEvent{
		Action:       raw.Action,
		IssueNumber:  raw.Issue.Number,
		IssueTitle:   raw.Issue.Title,
		IssueURL:     raw.Issue.URL,
		IssueLabels:  labels,
		RepoFullName: raw.Repository.FullName,
		CommentBody:  raw.Comment.Body,
	}, nil
}

// isTrackedAction checks if the action is one we care about.
func (h *Handler) isTrackedAction(action string) bool {
	tracked := map[string]bool{
		"issues.opened":         true,
		"issues.labeled":        true,
		"issue_comment.created": true,
	}
	return tracked[action]
}

// matchesTrigger checks if the event matches any configured trigger filter.
func (h *Handler) matchesTrigger(action string, event *webhookEvent) bool {
	if len(h.triggers) == 0 {
		return true // No triggers configured, accept all tracked events.
	}

	for _, trigger := range h.triggers {
		// Match event type if specified.
		if trigger.Event != "" && trigger.Event != action {
			continue
		}

		// If trigger has label filters, check them.
		if len(trigger.Labels) > 0 {
			if !h.hasAnyLabel(event.IssueLabels, trigger.Labels) {
				continue
			}
		}

		// If trigger has keyword filter, check issue title and comment body.
		if trigger.Keyword != "" {
			if !h.containsKeyword(event, trigger.Keyword) {
				continue
			}
		}

		return true
	}

	return false
}

// hasAnyLabel checks if any of the issue labels match the trigger labels.
func (h *Handler) hasAnyLabel(issueLabels, triggerLabels []string) bool {
	labelSet := make(map[string]bool, len(issueLabels))
	for _, l := range issueLabels {
		labelSet[strings.ToLower(l)] = true
	}
	for _, l := range triggerLabels {
		if labelSet[strings.ToLower(l)] {
			return true
		}
	}
	return false
}

// containsKeyword checks if the keyword appears in the issue title or comment body.
func (h *Handler) containsKeyword(event *webhookEvent, keyword string) bool {
	kw := strings.ToLower(keyword)
	if strings.Contains(strings.ToLower(event.IssueTitle), kw) {
		return true
	}
	if strings.Contains(strings.ToLower(event.CommentBody), kw) {
		return true
	}
	return false
}

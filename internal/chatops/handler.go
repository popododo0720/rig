package chatops

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/rigdev/rig/internal/core"
)

// ExecuteFunc starts pipeline execution for an issue.
type ExecuteFunc = func(issue core.Issue) error

// Handler receives ChatOps webhooks.
type Handler struct {
	statePath string
	onExecute ExecuteFunc
}

// NewHandler creates a ChatOps webhook handler.
func NewHandler(statePath string, onExecute ExecuteFunc) *Handler {
	return &Handler{statePath: statePath, onExecute: onExecute}
}

// HandleSlack handles Slack slash command webhooks.
func (h *Handler) HandleSlack(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.writeSlack(w, "invalid form payload", http.StatusBadRequest)
		return
	}

	raw := strings.TrimSpace(r.FormValue("text"))
	command := strings.TrimSpace(r.FormValue("command"))
	if command != "" {
		raw = strings.TrimSpace(command + " " + raw)
	}

	response, status := h.handleCommand(raw)
	h.writeSlack(w, response, status)
}

// HandleDiscord handles Discord message webhooks.
func (h *Handler) HandleDiscord(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Content string `json:"content"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		h.writeDiscord(w, "invalid JSON payload", http.StatusBadRequest)
		return
	}

	response, status := h.handleCommand(strings.TrimSpace(payload.Content))
	h.writeDiscord(w, response, status)
}

func (h *Handler) handleCommand(input string) (string, int) {
	cmd, err := ParseCommand(input)
	if err != nil {
		if errors.Is(err, errCommandNotFound) {
			return "unknown command. Try: rig status | rig tasks | rig logs <task-id> | rig exec <issue-url> | rig approve <task-id> | rig reject <task-id>", http.StatusBadRequest
		}
		return fmt.Sprintf("invalid command: %v", err), http.StatusBadRequest
	}

	if cmd.Action == "exec" {
		message, execErr := h.executeIssue(cmd)
		if execErr != nil {
			return execErr.Error(), http.StatusBadRequest
		}
		return message, http.StatusOK
	}

	message, execErr := Execute(cmd, h.statePath)
	if execErr != nil {
		return execErr.Error(), http.StatusBadRequest
	}

	return message, http.StatusOK
}

func (h *Handler) executeIssue(cmd *Command) (string, error) {
	if len(cmd.Args) < 1 {
		return "", errors.New("exec requires issue URL")
	}

	issue, err := parseIssueURL(cmd.Args[0])
	if err != nil {
		return "", err
	}

	if h.onExecute == nil {
		return "", errors.New("execution callback not configured")
	}

	var createdTaskID string
	err = core.WithState(h.statePath, func(s *core.State) error {
		if s.IsInFlight(issue.ID) {
			return fmt.Errorf("issue %s is already in-flight", issue.ID)
		}
		task := s.CreateTask(issue)
		createdTaskID = task.ID
		return nil
	})
	if err != nil {
		return "", err
	}

	go func() {
		_ = h.onExecute(issue)
	}()

	return fmt.Sprintf("Started task %s for issue #%s.", createdTaskID, issue.ID), nil
}

func (h *Handler) writeSlack(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"text": message})
}

func (h *Handler) writeDiscord(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(message))
}

func parseIssueURL(url string) (core.Issue, error) {
	re := regexp.MustCompile(`https?://github\.com/([^/]+)/([^/]+)/issues/(\d+)`)
	matches := re.FindStringSubmatch(strings.TrimSpace(url))
	if len(matches) != 4 {
		return core.Issue{}, errors.New("issue URL must match https://github.com/{owner}/{repo}/issues/{number}")
	}

	owner := matches[1]
	repo := matches[2]
	number := matches[3]
	issueNum, err := strconv.Atoi(number)
	if err != nil || issueNum <= 0 {
		return core.Issue{}, errors.New("invalid issue number")
	}

	return core.Issue{
		Platform: "github",
		Repo:     owner + "/" + repo,
		ID:       number,
		Title:    fmt.Sprintf("Issue #%s", number),
		URL:      strings.TrimSpace(url),
	}, nil
}

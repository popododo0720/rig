package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rigdev/rig/internal/config"
	"github.com/rigdev/rig/internal/core"
	"github.com/rigdev/rig/internal/storage"
)

//go:embed static/*
var staticFS embed.FS

// configResponse is the non-sensitive subset of config returned by the API.
type configResponse struct {
	Project  projectInfo  `json:"project"`
	Source   sourceInfo   `json:"source"`
	AI       aiInfo       `json:"ai"`
	Deploy   deployInfo   `json:"deploy"`
	Workflow workflowInfo `json:"workflow"`
}

type projectInfo struct {
	Name        string `json:"name"`
	Language    string `json:"language"`
	Description string `json:"description"`
}

type sourceInfo struct {
	Platform   string `json:"platform"`
	Repo       string `json:"repo"`
	BaseBranch string `json:"base_branch"`
}

type aiInfo struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	MaxRetry int    `json:"max_retry"`
}

type deployInfo struct {
	Method string `json:"method"`
}

type workflowInfo struct {
	Steps    []string               `json:"steps"`
	Triggers []config.TriggerConfig `json:"triggers"`
}

// ExecuteFunc is a callback that executes the automation pipeline for an issue.
type ExecuteFunc func(issue core.Issue) error

// NewHandler creates an http.Handler that serves the web dashboard API and SPA.
// If db is provided, settings/agents APIs are enabled.
// If cfg is nil, the server runs in setup mode (settings only).
// If execFn is provided, new tasks trigger the automation pipeline.
// securityHeadersMiddleware adds standard security headers to all responses.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

// apiKeyAuthMiddleware checks for a valid API key if RIG_API_KEY env var is set.
// If RIG_API_KEY is not set, authentication is skipped (open access).
// API key can be passed via Authorization header (Bearer <key>) or X-API-Key header.
func apiKeyAuthMiddleware(next http.Handler) http.Handler {
	apiKey := os.Getenv("RIG_API_KEY")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if apiKey == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Check Authorization: Bearer <key>
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(auth, "Bearer ") && strings.TrimPrefix(auth, "Bearer ") == apiKey {
			next.ServeHTTP(w, r)
			return
		}

		// Check X-API-Key header
		if r.Header.Get("X-API-Key") == apiKey {
			next.ServeHTTP(w, r)
			return
		}

		// Check query param for SSE/browser convenience
		if r.URL.Query().Get("api_key") == apiKey {
			next.ServeHTTP(w, r)
			return
		}

		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
	})
}

func NewHandler(statePath string, cfg *config.Config, db *storage.DB, execFn ...ExecuteFunc) http.Handler {
	r := chi.NewRouter()

	// Security headers on all responses
	r.Use(securityHeadersMiddleware)

	configured := cfg != nil

	var executeFn ExecuteFunc
	if len(execFn) > 0 && execFn[0] != nil {
		executeFn = execFn[0]
	}

	// --- API routes ---
	r.Route("/api", func(r chi.Router) {
		// API key auth on all API routes (if RIG_API_KEY is set)
		r.Use(apiKeyAuthMiddleware)
		// Always available: settings, agents, status
		if db != nil {
			r.Get("/settings", handleGetSettings(db))
			r.Post("/settings", handleSaveSettings(db))
			r.Get("/agents/{repo}", handleGetAgents(db))
			r.Post("/agents/{repo}", handleSaveAgents(db))
			r.Get("/agents", handleListAgents(db))
		}
		r.Get("/status", handleGetStatus(configured))

		// Task/proposal routes require config (full mode)
		if configured {
			r.Get("/tasks", handleGetTasks(statePath))
			r.Post("/tasks", handleCreateTask(statePath, cfg, executeFn))
			r.Post("/tasks/{id}/retry", handleRetryTask(statePath, executeFn))
			r.Post("/tasks/{id}/stop", handleStopTask(statePath))
			if db != nil {
				r.Get("/tasks/{id}/logs", handleGetTaskLogs(db))
			}
			r.Get("/tasks/{id}", handleGetTask(statePath))
			r.Get("/proposals", handleGetProposals(statePath))
			r.Get("/proposals/{taskId}", handleGetTaskProposals(statePath))
			r.Post("/approve/{taskId}", handleApprove(statePath, cfg))
			r.Post("/reject/{taskId}", handleReject(statePath))
			r.Get("/config", handleGetConfig(cfg))
			r.Get("/projects", handleGetProjects(cfg))
			r.Get("/events", handleSSE(statePath))
		} else {
			// Setup mode: return 503 for task routes
			setupHandler := func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{
					"error": "rig not configured — visit Settings to set up",
				})
			}
			r.Get("/tasks", http.HandlerFunc(setupHandler))
			r.Get("/tasks/{id}", http.HandlerFunc(setupHandler))
			r.Post("/tasks", http.HandlerFunc(setupHandler))
			r.Get("/proposals", http.HandlerFunc(setupHandler))
			r.Get("/events", http.HandlerFunc(setupHandler))
			r.Get("/config", http.HandlerFunc(setupHandler))
			r.Get("/projects", http.HandlerFunc(setupHandler))
		}
	})

	// --- Static SPA files ---
	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Printf("web: CRITICAL: failed to create sub-filesystem: %v", err)
		r.Handle("/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "internal server error: static files unavailable", http.StatusInternalServerError)
		}))
		return r
	}
	fileServer := http.FileServer(http.FS(staticSub))
	r.Handle("/*", fileServer)

	return r
}

func handleGetTasks(statePath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state, err := core.LoadState(statePath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, state.Tasks)
	}
}

func handleGetTask(statePath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")

		state, err := core.LoadState(statePath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		for i := range state.Tasks {
			if state.Tasks[i].ID == id {
				writeJSON(w, http.StatusOK, state.Tasks[i])
				return
			}
		}

		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
	}
}

type createTaskRequest struct {
	Project  string `json:"project"`
	IssueNum string `json:"issue_num"`
	IssueURL string `json:"issue_url"`
	IssueID  string `json:"issue_id"`
	Title    string `json:"title"`
	Body     string `json:"body"`
}

func mergedProjects(cfg *config.Config) []config.ProjectEntry {
	projects := make([]config.ProjectEntry, 0, 1+len(cfg.Projects))
	seen := make(map[string]struct{}, 1+len(cfg.Projects))

	appendUnique := func(p config.ProjectEntry) {
		repo := strings.TrimSpace(p.Repo)
		if repo == "" {
			return
		}
		if _, ok := seen[repo]; ok {
			return
		}
		if p.Platform == "" {
			p.Platform = "github"
		}
		projects = append(projects, p)
		seen[repo] = struct{}{}
	}

	appendUnique(config.ProjectEntry{
		Name:       cfg.Project.Name,
		Platform:   cfg.Source.Platform,
		Repo:       cfg.Source.Repo,
		BaseBranch: cfg.Source.BaseBranch,
	})

	for i := range cfg.Projects {
		appendUnique(cfg.Projects[i])
	}

	return projects
}

type issueURLParts struct {
	owner  string
	repo   string
	number string
}

func parseIssueURL(url string) *issueURLParts {
	// Parse: https://github.com/{owner}/{repo}/issues/{number}
	// Also handle: http://github.com/{owner}/{repo}/issues/{number}
	parts := issueURLParts{}

	// Simple regex-free parsing
	// Expected format: https://github.com/owner/repo/issues/123
	if len(url) < 30 {
		return nil
	}

	// Find "github.com/"
	idx := 0
	for i := 0; i < len(url)-10; i++ {
		if url[i:i+11] == "github.com/" {
			idx = i + 11
			break
		}
	}
	if idx == 0 {
		return nil
	}

	// Extract owner/repo/issues/number
	remaining := url[idx:]
	segments := make([]string, 0)
	current := ""
	for i := 0; i < len(remaining); i++ {
		if remaining[i] == '/' {
			if current != "" {
				segments = append(segments, current)
				current = ""
			}
		} else if remaining[i] == '#' || remaining[i] == '?' {
			if current != "" {
				segments = append(segments, current)
			}
			break
		} else {
			current += string(remaining[i])
		}
	}
	if current != "" {
		segments = append(segments, current)
	}

	// Need at least: owner, repo, "issues", number
	if len(segments) < 4 {
		return nil
	}

	if segments[2] != "issues" {
		return nil
	}

	parts.owner = segments[0]
	parts.repo = segments[1]
	parts.number = segments[3]

	return &parts
}

func handleCreateTask(statePath string, cfg *config.Config, executeFn ExecuteFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createTaskRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		req.Project = strings.TrimSpace(req.Project)
		req.IssueNum = strings.TrimSpace(req.IssueNum)
		req.IssueURL = strings.TrimSpace(req.IssueURL)
		req.IssueID = strings.TrimSpace(req.IssueID)
		req.Title = strings.TrimSpace(req.Title)

		// Parse issue_url if provided
		var issue core.Issue
		if req.Project != "" && req.IssueNum != "" {
			projects := mergedProjects(cfg)
			var project *config.ProjectEntry
			for i := range projects {
				if projects[i].Repo == req.Project {
					project = &projects[i]
					break
				}
			}
			if project == nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "project not found"})
				return
			}
			platform := project.Platform
			if platform == "" {
				platform = "github"
			}
			issueURL := "https://github.com/" + project.Repo + "/issues/" + req.IssueNum
			issue = core.Issue{
				Platform: platform,
				Repo:     project.Repo,
				ID:       req.IssueNum,
				Title:    req.Title,
				URL:      issueURL,
			}
			if issue.Title == "" {
				issue.Title = "Issue #" + req.IssueNum
			}
		} else if req.IssueURL != "" {
			parts := parseIssueURL(req.IssueURL)
			if parts == nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid issue URL format"})
				return
			}
			issue = core.Issue{
				Platform: "github",
				Repo:     parts.owner + "/" + parts.repo,
				ID:       parts.number,
				Title:    req.Title,
				URL:      req.IssueURL,
			}
			if issue.Title == "" {
				issue.Title = "Issue #" + parts.number
			}
		} else if req.IssueID != "" {
			issue = core.Issue{
				Platform: "github",
				ID:       req.IssueID,
				Title:    req.Title,
			}
		} else {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "project+issue_num or issue_url or issue_id required"})
			return
		}

		state, err := core.LoadState(statePath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		task := state.CreateTask(issue)
		if err := core.SaveState(state, statePath); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		// 태스크 생성 후 바로 엔진 실행 (백그라운드)
		if executeFn != nil {
			go func() {
				if err := executeFn(issue); err != nil {
					log.Printf("web: execute task %s failed: %v", task.ID, err)
				}
			}()
		}

		writeJSON(w, http.StatusCreated, task)
	}
}

func handleRetryTask(statePath string, executeFn ExecuteFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")

		state, err := core.LoadState(statePath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		task := state.GetTaskByID(id)
		if task == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
			return
		}

		if executeFn == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "execution not available"})
			return
		}

		go func() {
			if err := executeFn(task.Issue); err != nil {
				log.Printf("web: retry task %s failed: %v", task.ID, err)
			}
		}()

		writeJSON(w, http.StatusOK, map[string]string{"status": "started", "task_id": task.ID})
	}
}

func handleStopTask(statePath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")

		state, err := core.LoadState(statePath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		task := state.GetTaskByID(id)
		if task == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
			return
		}

		if err := core.Transition(task, core.PhaseFailed); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		if err := core.SaveState(state, statePath); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "stopped", "task_id": task.ID})
	}
}

func handleGetTaskLogs(db *storage.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		afterStr := r.URL.Query().Get("after")
		var logs []storage.LogEntry
		var err error
		if afterStr != "" {
			var afterID int64
			fmt.Sscanf(afterStr, "%d", &afterID)
			logs, err = db.GetLogsSince(id, afterID)
		} else {
			logs, err = db.GetLogs(id)
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if logs == nil {
			logs = []storage.LogEntry{}
		}
		writeJSON(w, http.StatusOK, logs)
	}
}

func handleGetProjects(cfg *config.Config) http.HandlerFunc {
	projects := mergedProjects(cfg)

	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, projects)
	}
}

func handleGetConfig(cfg *config.Config) http.HandlerFunc {
	resp := configResponse{
		Project: projectInfo{
			Name:        cfg.Project.Name,
			Language:    cfg.Project.Language,
			Description: cfg.Project.Description,
		},
		Source: sourceInfo{
			Platform:   cfg.Source.Platform,
			Repo:       cfg.Source.Repo,
			BaseBranch: cfg.Source.BaseBranch,
		},
		AI: aiInfo{
			Provider: cfg.AI.Provider,
			Model:    cfg.AI.Model,
			MaxRetry: cfg.AI.MaxRetry,
		},
		Deploy: deployInfo{
			Method: cfg.Deploy.Method,
		},
		Workflow: workflowInfo{
			Steps:    cfg.Workflow.Steps,
			Triggers: cfg.Workflow.Trigger,
		},
	}

	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, resp)
	}
}

type pendingProposalItem struct {
	TaskID    string        `json:"task_id"`
	TaskTitle string        `json:"task_title"`
	Proposal  core.Proposal `json:"proposal"`
}

func handleGetProposals(statePath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state, err := core.LoadState(statePath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		items := make([]pendingProposalItem, 0)
		for _, task := range state.Tasks {
			for _, proposal := range task.Proposals {
				if proposal.Status != core.ProposalPending {
					continue
				}
				items = append(items, pendingProposalItem{
					TaskID:    task.ID,
					TaskTitle: task.Issue.Title,
					Proposal:  proposal,
				})
			}
		}

		writeJSON(w, http.StatusOK, items)
	}
}

func handleGetTaskProposals(statePath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		taskID := chi.URLParam(r, "taskId")

		state, err := core.LoadState(statePath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		task := state.GetTaskByID(taskID)
		if task == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
			return
		}

		proposals := make([]core.Proposal, 0)
		for _, proposal := range task.Proposals {
			if proposal.Status == core.ProposalPending {
				proposals = append(proposals, proposal)
			}
		}

		writeJSON(w, http.StatusOK, proposals)
	}
}

func handleApprove(statePath string, _ *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		taskID := chi.URLParam(r, "taskId")

		state, err := core.LoadState(statePath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		task := state.GetTaskByID(taskID)
		if task == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
			return
		}

		proposal := task.GetPendingProposal()
		if proposal == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no pending proposal"})
			return
		}

		now := time.Now().UTC()
		proposal.Status = core.ProposalApproved
		proposal.ReviewedAt = &now

		if err := core.SaveState(state, statePath); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "approved",
			"message": "Proposal approved. Task will resume on next engine cycle.",
		})
	}
}

func handleReject(statePath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		taskID := chi.URLParam(r, "taskId")

		state, err := core.LoadState(statePath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		task := state.GetTaskByID(taskID)
		if task == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
			return
		}

		proposal := task.GetPendingProposal()
		if proposal == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no pending proposal"})
			return
		}

		now := time.Now().UTC()
		proposal.Status = core.ProposalRejected
		proposal.ReviewedAt = &now

		if err := core.Transition(task, core.PhaseFailed); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		if err := core.SaveState(state, statePath); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "rejected",
			"message": "Proposal rejected. Task marked as failed.",
		})
	}
}

func handleSSE(statePath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		// Send initial state immediately.
		state, err := core.LoadState(statePath)
		if err != nil {
			log.Printf("web: SSE initial load error: %v", err)
			return
		}
		sendSSEEvent(w, flusher, "tasks", state.Tasks)

		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		prevJSON := marshalTasks(state.Tasks)

		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				state, err := core.LoadState(statePath)
				if err != nil {
					log.Printf("web: SSE poll error: %v", err)
					continue
				}
				curJSON := marshalTasks(state.Tasks)
				if curJSON != prevJSON {
					sendSSEEvent(w, flusher, "tasks", state.Tasks)
					prevJSON = curJSON
				}
			}
		}
	}
}

func sendSSEEvent(w http.ResponseWriter, flusher http.Flusher, event string, data []core.Task) {
	payload, err := json.Marshal(data)
	if err != nil {
		log.Printf("web: SSE marshal error: %v", err)
		return
	}
	// SSE clients may disconnect between frames; write errors are expected and safely ignored.
	_, _ = w.Write([]byte("event: " + event + "\ndata: "))
	_, _ = w.Write(payload)
	_, _ = w.Write([]byte("\n\n"))
	flusher.Flush()
}

func marshalTasks(tasks []core.Task) string {
	data, err := json.Marshal(tasks)
	if err != nil {
		log.Printf("web: tasks marshal error: %v", err)
		return ""
	}
	return string(data)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("web: JSON encode error: %v", err)
	}
}

// --- Settings API ---

var sensitiveFields = map[string][]string{
	"source": {"token"},
	"ai":     {"api_key"},
	"server": {"secret"},
}

func handleGetStatus(configured bool) http.HandlerFunc {
	mode := "full"
	if !configured {
		mode = "setup"
	}
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"configured": configured,
			"mode":       mode,
		})
	}
}

func handleGetSettings(db *storage.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		settings, err := db.GetAllSettings()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		// Parse each section into a generic map and mask sensitive fields.
		result := make(map[string]interface{})
		for key, value := range settings {
			var parsed interface{}
			if err := json.Unmarshal([]byte(value), &parsed); err != nil {
				result[key] = value
				continue
			}
			if m, ok := parsed.(map[string]interface{}); ok {
				maskFields(key, m)
			}
			result[key] = parsed
		}

		writeJSON(w, http.StatusOK, result)
	}
}

type saveSettingsRequest struct {
	Section string          `json:"section"`
	Data    json.RawMessage `json:"data"`
}

func handleSaveSettings(db *storage.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read body failed"})
			return
		}

		var req saveSettingsRequest
		if err := json.Unmarshal(body, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		if req.Section != "" {
			// Single section save.
			merged, err := mergeWithExisting(db, req.Section, req.Data)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			if err := db.SetSetting(req.Section, string(merged)); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
		} else {
			// Bulk save: data is a map of sections.
			var sections map[string]json.RawMessage
			if err := json.Unmarshal(req.Data, &sections); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "data must be a section map when section is empty"})
				return
			}
			for section, data := range sections {
				merged, err := mergeWithExisting(db, section, data)
				if err != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
					return
				}
				if err := db.SetSetting(section, string(merged)); err != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
					return
				}
			}
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
	}
}

// mergeWithExisting preserves sensitive field values when the incoming value is "***".
func mergeWithExisting(db *storage.DB, section string, incoming json.RawMessage) (json.RawMessage, error) {
	fields, hasSensitive := sensitiveFields[section]
	if !hasSensitive {
		return incoming, nil
	}

	var incomingMap map[string]interface{}
	if err := json.Unmarshal(incoming, &incomingMap); err != nil {
		return incoming, nil // not a map, store as-is
	}

	needsMerge := false
	for _, field := range fields {
		if v, ok := incomingMap[field]; ok {
			if s, ok := v.(string); ok && s == "***" {
				needsMerge = true
				break
			}
		}
	}

	if !needsMerge {
		return incoming, nil
	}

	existing, err := db.GetSetting(section)
	if err != nil || existing == "" {
		return incoming, nil
	}

	var existingMap map[string]interface{}
	if err := json.Unmarshal([]byte(existing), &existingMap); err != nil {
		return incoming, nil
	}

	for _, field := range fields {
		if v, ok := incomingMap[field]; ok {
			if s, ok := v.(string); ok && s == "***" {
				if orig, ok := existingMap[field]; ok {
					incomingMap[field] = orig
				}
			}
		}
	}

	return json.Marshal(incomingMap)
}

func maskFields(section string, m map[string]interface{}) {
	fields, ok := sensitiveFields[section]
	if !ok {
		return
	}
	for _, field := range fields {
		if v, ok := m[field]; ok {
			if s, ok := v.(string); ok && s != "" {
				m[field] = "***"
			}
		}
	}
}

// --- Agents API ---

func handleGetAgents(db *storage.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repo := chi.URLParam(r, "repo")
		content, err := db.GetAgents(repo)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"repo": repo, "content": content})
	}
}

func handleSaveAgents(db *storage.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repo := chi.URLParam(r, "repo")

		var req struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		if err := db.SetAgents(repo, req.Content); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
	}
}

func handleListAgents(db *storage.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		agents, err := db.ListAgents()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, agents)
	}
}

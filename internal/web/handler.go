package web

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rigdev/rig/internal/config"
	"github.com/rigdev/rig/internal/core"
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

// NewHandler creates an http.Handler that serves the web dashboard API and SPA.
func NewHandler(statePath string, cfg *config.Config) http.Handler {
	r := chi.NewRouter()

	// --- API routes ---
	r.Route("/api", func(r chi.Router) {
		r.Get("/tasks", handleGetTasks(statePath))
		r.Get("/tasks/{id}", handleGetTask(statePath))
		r.Get("/proposals", handleGetProposals(statePath))
		r.Get("/proposals/{taskId}", handleGetTaskProposals(statePath))
		r.Post("/approve/{taskId}", handleApprove(statePath, cfg))
		r.Post("/reject/{taskId}", handleReject(statePath))
		r.Get("/config", handleGetConfig(cfg))
		r.Get("/events", handleSSE(statePath))
	})

	// --- Static SPA files ---
	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatalf("web: failed to create sub-filesystem: %v", err)
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

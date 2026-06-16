package dashboard

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"buycott/internal/model"
	"buycott/internal/server"
)

//go:embed index.html
var indexHTML []byte

func Listen(srv server.Server, port int) error {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	})

	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		st, err := srv.GetStatus()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, st)
	})

	// Clear the current run and start over. Query params:
	//   wipe_artifacts=1  also delete generated project files
	//   restart=1         relaunch the pipeline from the original direction
	mux.HandleFunc("POST /api/reset", func(w http.ResponseWriter, r *http.Request) {
		opts := server.ResetOptions{
			WipeArtifacts: r.URL.Query().Get("wipe_artifacts") == "1",
			Restart:       r.URL.Query().Get("restart") == "1",
		}
		if err := srv.Reset(r.Context(), opts); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("GET /api/tasks", func(w http.ResponseWriter, r *http.Request) {
		var filter model.TaskFilter
		if s := r.URL.Query().Get("status"); s != "" {
			filter.Status = model.TaskStatus(s)
		}
		tasks, err := srv.ListTasks(filter)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if tasks == nil {
			tasks = []*model.Task{}
		}
		writeJSON(w, tasks)
	})

	mux.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		limit := 100
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 {
				limit = n
			}
		}
		all, err := srv.ListEvents(0)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Return the most recent N events (ListEvents returns ASC order).
		if len(all) > limit {
			all = all[len(all)-limit:]
		}
		if all == nil {
			all = []*model.Event{}
		}
		writeJSON(w, all)
	})

	mux.HandleFunc("GET /api/conversations", func(w http.ResponseWriter, r *http.Request) {
		taskID := r.URL.Query().Get("task_id")
		role := r.URL.Query().Get("role")
		limit := 50
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 {
				limit = n
			}
		}
		logs, err := srv.ListConversations(taskID, role, limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if logs == nil {
			logs = []*model.LLMLog{}
		}
		writeJSON(w, logs)
	})

	mux.HandleFunc("GET /api/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		task, err := srv.GetTask(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if task == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSON(w, task)
	})

	mux.HandleFunc("GET /api/stats", func(w http.ResponseWriter, r *http.Request) {
		stats, err := srv.TokenStats()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if stats == nil {
			writeJSON(w, []struct{}{})
			return
		}
		writeJSON(w, stats)
	})

	mux.HandleFunc("GET /api/releases", func(w http.ResponseWriter, r *http.Request) {
		releases, err := srv.ListReleases()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if releases == nil {
			releases = []*model.Release{}
		}
		writeJSON(w, releases)
	})

	// SSE: streams only events newer than the `since` Unix timestamp.
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		// Default since = now; client can send an older timestamp to catch up.
		since := time.Now()
		if s := r.URL.Query().Get("since"); s != "" {
			if n, err := strconv.ParseInt(s, 10, 64); err == nil {
				since = time.Unix(n, 0)
			}
		}

		ch, err := srv.StreamEvents(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Send a keepalive comment every 15s so proxies don't close the connection.
		keepalive := time.NewTicker(15 * time.Second)
		defer keepalive.Stop()

		for {
			select {
			case ev, ok := <-ch:
				if !ok {
					return
				}
				if ev.CreatedAt.Before(since) {
					continue // Skip historical events before the requested cursor.
				}
				data, _ := json.Marshal(ev)
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			case <-keepalive.C:
				fmt.Fprint(w, ": keepalive\n\n")
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})

	return http.ListenAndServe(fmt.Sprintf(":%d", port), mux)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

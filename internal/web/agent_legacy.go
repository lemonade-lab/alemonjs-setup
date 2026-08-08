package web

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"alemonx/internal/agent"
)

// agentLegacyTaskStream keeps the historical SSE wire format while the actual
// execution lives in TaskManager and survives the HTTP connection.
func (s *server) agentLegacyTaskStream(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "请求无法读取。")
		return
	}
	var input agentTaskInput
	if json.Unmarshal(body, &input) != nil {
		writeError(w, http.StatusBadRequest, "请求无法识别。")
		return
	}
	created, createErr := s.createAgentTask(input, true)
	if createErr != nil {
		writeError(w, http.StatusBadRequest, createErr.Error())
		return
	}
	createdID := created.ID
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "当前服务不支持事件流。")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	wake, unsubscribe, subscribeErr := s.agentTasks.Subscribe(createdID)
	if subscribeErr != nil {
		return
	}
	defer unsubscribe()
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	last := int64(0)
	for {
		events, eventErr := s.agentTasks.Events(createdID, last)
		if eventErr != nil {
			return
		}
		for _, event := range events {
			public := publicTaskEvent(event)
			raw, _ := json.Marshal(public.Event)
			_, _ = w.Write([]byte("id: " + strconv.FormatInt(event.ID, 10) + "\ndata: " + string(raw) + "\n\n"))
			last = event.ID
		}
		flusher.Flush()
		task, getErr := s.agentTasks.Get(createdID)
		if getErr != nil || task.Status == agent.TaskCompleted || task.Status == agent.TaskFailed || task.Status == agent.TaskCancelled || task.Status == agent.TaskRolledBack {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-wake:
		case <-heartbeat.C:
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *server) agentLegacyTaskWait(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, 400, "请求无法读取。")
		return
	}
	var input agentTaskInput
	if json.Unmarshal(body, &input) != nil {
		writeError(w, http.StatusBadRequest, "请求无法识别。")
		return
	}
	created, createErr := s.createAgentTask(input, true)
	if createErr != nil {
		writeError(w, http.StatusBadRequest, createErr.Error())
		return
	}
	envelope := struct{ TaskID, SessionID string }{TaskID: created.ID, SessionID: created.SessionID}
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	wake, unsubscribe, subscribeErr := s.agentTasks.Subscribe(envelope.TaskID)
	if subscribeErr != nil {
		writeError(w, http.StatusInternalServerError, subscribeErr.Error())
		return
	}
	defer unsubscribe()
	last := int64(0)
	for {
		for _, event := range rangeMustTaskEvents(s, envelope.TaskID, last) {
			last = event.ID
			if event.Type == "done" {
				writeJSON(w, 200, map[string]any{"answer": event.Text, "sessionId": envelope.SessionID})
				return
			}
			if event.Type == "error" {
				writeError(w, http.StatusBadGateway, event.Text)
				return
			}
		}
		task, getErr := s.agentTasks.Get(envelope.TaskID)
		if getErr != nil {
			writeError(w, 500, getErr.Error())
			return
		}
		if task.Status == agent.TaskCompleted || task.Status == agent.TaskFailed || task.Status == agent.TaskCancelled {
			writeJSON(w, 202, map[string]any{"taskId": envelope.TaskID, "sessionId": envelope.SessionID, "status": task.Status})
			return
		}
		select {
		case <-r.Context().Done():
			writeJSON(w, 202, map[string]any{"taskId": envelope.TaskID, "sessionId": envelope.SessionID, "status": task.Status})
			return
		case <-deadline.C:
			writeJSON(w, 202, map[string]any{"taskId": envelope.TaskID, "sessionId": envelope.SessionID, "status": task.Status})
			return
		case <-wake:
		}
	}
}

func rangeMustTaskEvents(s *server, id string, after int64) []agent.TaskEvent {
	events, _ := s.agentTasks.Events(id, after)
	return events
}

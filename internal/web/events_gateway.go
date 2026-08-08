package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type eventGateway struct {
	mu                                                                 sync.Mutex
	subscribers                                                        map[chan eventEnvelope]struct{}
	published, persistedFailures, replayed, slowDropped, cursorExpired atomic.Int64
}

func newEventGateway() *eventGateway {
	return &eventGateway{subscribers: map[chan eventEnvelope]struct{}{}}
}
func (g *eventGateway) subscribe() chan eventEnvelope {
	ch := make(chan eventEnvelope, 64)
	g.mu.Lock()
	g.subscribers[ch] = struct{}{}
	g.mu.Unlock()
	return ch
}
func (g *eventGateway) unsubscribe(ch chan eventEnvelope) {
	g.mu.Lock()
	delete(g.subscribers, ch)
	g.mu.Unlock()
}
func (g *eventGateway) publish(event eventEnvelope) {
	g.mu.Lock()
	for ch := range g.subscribers {
		select {
		case ch <- event:
		default:
			delete(g.subscribers, ch)
			g.slowDropped.Add(1)
		}
	}
	g.mu.Unlock()
}

func (g *eventGateway) diagnostics() map[string]int64 {
	g.mu.Lock()
	connections := int64(len(g.subscribers))
	g.mu.Unlock()
	return map[string]int64{"connections": connections, "published": g.published.Load(), "persistFailures": g.persistedFailures.Load(), "replayed": g.replayed.Load(), "slowConsumers": g.slowDropped.Load(), "cursorExpired": g.cursorExpired.Load()}
}

func parseEventTopics(raw string) map[string]bool {
	out := map[string]bool{}
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "robot" || item == "ops" || item == "system" || item == "plugins" {
			out[item] = true
		}
	}
	return out
}

func (s *server) publishEvent(topic, kind string, data any, operations []operationTask) (eventEnvelope, bool) {
	if s.operationEvents == nil || s.eventGateway == nil {
		return eventEnvelope{}, false
	}
	event, err := s.operationEvents.append(topic, kind, data, operations)
	if err != nil {
		s.eventGateway.persistedFailures.Add(1)
		return eventEnvelope{}, false
	}
	s.eventGateway.published.Add(1)
	s.eventGateway.publish(event)
	return event, true
}

func (s *server) allowedEventTopics(r *http.Request, requested map[string]bool) (map[string]bool, bool) {
	if s.auth != nil && !s.auth.Authenticate(s.authToken(r)) {
		return nil, false
	}
	allowed := map[string]bool{"robot": true, "system": true, "plugins": true}
	_, role := s.opsActor(r)
	if opsRoleLevel(role) >= opsRoleLevel("viewer") {
		allowed["ops"] = true
	}
	if len(requested) == 0 {
		return allowed, true
	}
	filtered := map[string]bool{}
	for topic := range requested {
		if allowed[topic] {
			filtered[topic] = true
		}
	}
	return filtered, true
}

func (s *server) eventsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	if r.URL.Path == "/api/v1/events/diagnostics" {
		if !s.requireOpsRole(w, r, "viewer", "events.diagnostics", "events") {
			return
		}
		writeJSON(w, http.StatusOK, s.eventGateway.diagnostics())
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "SSE 不受支持。")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	requested := parseEventTopics(r.URL.Query().Get("topics"))
	topics, authenticated := s.allowedEventTopics(r, requested)
	if !authenticated {
		writeError(w, http.StatusForbidden, "事件流需要登录。")
		return
	}
	last, _ := strconv.ParseInt(r.URL.Query().Get("lastEventId"), 10, 64)
	if header, _ := strconv.ParseInt(r.Header.Get("Last-Event-ID"), 10, 64); header > last {
		last = header
	}
	sub := s.eventGateway.subscribe()
	defer s.eventGateway.unsubscribe(sub)
	write := func(item eventEnvelope) bool {
		if len(topics) > 0 && !topics[item.Topic] {
			return true
		}
		data, err := json.Marshal(item)
		if err != nil {
			return true
		}
		if _, err = w.Write([]byte("id: " + strconv.FormatInt(item.ID, 10) + "\ndata: " + string(data) + "\n\n")); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	earliest, latest := s.operationEvents.bounds()
	if last > 0 && earliest > 0 && last < earliest {
		s.eventGateway.cursorExpired.Add(1)
		cursor, _ := json.Marshal(eventEnvelope{ID: latest, Topic: "system", Type: "system.cursor-expired", CreatedAt: time.Now().UTC(), Data: json.RawMessage(`{"type":"system.cursor-expired"}`)})
		if _, err := w.Write([]byte("data: " + string(cursor) + "\n\n")); err != nil {
			return
		}
		flusher.Flush()
		last = earliest - 1
	}
	for _, item := range s.operationEvents.after(last, topics) {
		s.eventGateway.replayed.Add(1)
		if !write(item) {
			return
		}
	}
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case item := <-sub:
			if !write(item) {
				return
			}
		case <-heartbeat.C:
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

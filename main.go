package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	serverchan_sdk "github.com/easychen/serverchan-sdk-golang"
	"gopkg.in/yaml.v3"
	_ "modernc.org/sqlite"
)

type Config struct {
	BaseURL                     string   `yaml:"base_url"`
	APIKey                      string   `yaml:"api_key"`
	ServerChanSendKey           string   `yaml:"serverchan_sendkey"`
	AppriseURLs                 []string `yaml:"apprise_urls"`
	AppriseBin                  string   `yaml:"apprise_bin"`
	SQLitePath                  string   `yaml:"sqlite_path"`
	DefaultExpiresInSeconds     int      `yaml:"default_expires_in_seconds"`
	SSEHeartbeatIntervalSeconds int      `yaml:"sse_heartbeat_interval_seconds"`
	ListenAddr                  string   `yaml:"listen_addr"`
	TerminalCacheSeconds        int      `yaml:"terminal_cache_seconds"`
}

func (c *Config) normalize() error {
	if strings.TrimSpace(c.BaseURL) == "" {
		return errors.New("base_url is required")
	}
	_, err := url.Parse(c.BaseURL)
	if err != nil {
		return fmt.Errorf("invalid base_url: %w", err)
	}
	if strings.TrimSpace(c.APIKey) == "" {
		return errors.New("api_key is required")
	}
	if strings.TrimSpace(c.SQLitePath) == "" {
		c.SQLitePath = "./ask4me.db"
	}
	if strings.TrimSpace(c.AppriseBin) == "" {
		c.AppriseBin = "apprise"
	}
	if c.DefaultExpiresInSeconds <= 0 {
		c.DefaultExpiresInSeconds = 3600
	}
	if c.SSEHeartbeatIntervalSeconds <= 0 {
		c.SSEHeartbeatIntervalSeconds = 15
	}
	if strings.TrimSpace(c.ListenAddr) == "" {
		c.ListenAddr = ":8080"
	}
	if c.TerminalCacheSeconds <= 0 {
		c.TerminalCacheSeconds = 60
	}
	return nil
}

type Event struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Time      string          `json:"time"`
	RequestID string          `json:"request_id"`
	Data      json.RawMessage `json:"data"`
}

type runtimeHub struct {
	mu          sync.Mutex
	subscribers map[string]map[chan Event]struct{}
	terminal    map[string]terminalCacheEntry
	ttl         time.Duration
}

type terminalCacheEntry struct {
	event   Event
	expires time.Time
}

func newRuntimeHub(ttl time.Duration) *runtimeHub {
	h := &runtimeHub{
		subscribers: map[string]map[chan Event]struct{}{},
		terminal:    map[string]terminalCacheEntry{},
		ttl:         ttl,
	}
	go h.evictLoop()
	return h
}

func (h *runtimeHub) evictLoop() {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for range t.C {
		now := time.Now()
		h.mu.Lock()
		for k, v := range h.terminal {
			if now.After(v.expires) {
				delete(h.terminal, k)
			}
		}
		h.mu.Unlock()
	}
}

func (h *runtimeHub) subscribe(requestID string) (chan Event, func()) {
	ch := make(chan Event, 16)
	h.mu.Lock()
	m := h.subscribers[requestID]
	if m == nil {
		m = map[chan Event]struct{}{}
		h.subscribers[requestID] = m
	}
	m[ch] = struct{}{}
	h.mu.Unlock()

	unsub := func() {
		h.mu.Lock()
		if m := h.subscribers[requestID]; m != nil {
			delete(m, ch)
			if len(m) == 0 {
				delete(h.subscribers, requestID)
			}
		}
		h.mu.Unlock()
		close(ch)
	}
	return ch, unsub
}

func (h *runtimeHub) publish(ev Event) {
	h.mu.Lock()
	m := h.subscribers[ev.RequestID]
	for ch := range m {
		select {
		case ch <- ev:
		default:
		}
	}
	h.mu.Unlock()
}

func (h *runtimeHub) setTerminal(ev Event) {
	h.mu.Lock()
	h.terminal[ev.RequestID] = terminalCacheEntry{
		event:   ev,
		expires: time.Now().Add(h.ttl),
	}
	delete(h.subscribers, ev.RequestID)
	h.mu.Unlock()
}

func (h *runtimeHub) getTerminal(requestID string) (Event, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	v, ok := h.terminal[requestID]
	if !ok {
		return Event{}, false
	}
	if time.Now().After(v.expires) {
		delete(h.terminal, requestID)
		return Event{}, false
	}
	return v.event, true
}

type store struct {
	db *sql.DB
}

func newStore(db *sql.DB) (*store, error) {
	s := &store{db: db}
	stmts := []string{
		`PRAGMA journal_mode=WAL;`,
		`CREATE TABLE IF NOT EXISTS requests (
			request_id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			body TEXT NOT NULL,
			mcd TEXT NOT NULL,
			status TEXT NOT NULL,
			expires_at INTEGER NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS tokens (
			request_id TEXT NOT NULL,
			token_hash TEXT NOT NULL,
			expires_at INTEGER NOT NULL,
			created_at INTEGER NOT NULL,
			used_at INTEGER,
			PRIMARY KEY(request_id, token_hash)
		);`,
		`CREATE TABLE IF NOT EXISTS answers (
			request_id TEXT PRIMARY KEY,
			action TEXT,
			text TEXT,
			created_at INTEGER NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS events (
			seq INTEGER PRIMARY KEY AUTOINCREMENT,
			request_id TEXT NOT NULL,
			event_id TEXT NOT NULL,
			type TEXT NOT NULL,
			payload_json TEXT NOT NULL,
			created_at INTEGER NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_events_request_seq ON events(request_id, seq);`,
	}
	for _, st := range stmts {
		if _, err := db.Exec(st); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *store) createRequest(ctx context.Context, reqID, title, body, mcd, status string, expiresAt time.Time) error {
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO requests(request_id,title,body,mcd,status,expires_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`,
		reqID, title, body, mcd, status, expiresAt.Unix(), now, now,
	)
	return err
}

func (s *store) updateRequestStatus(ctx context.Context, reqID, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE requests SET status=?, updated_at=? WHERE request_id=?`, status, time.Now().Unix(), reqID)
	return err
}

func (s *store) getRequestStatus(ctx context.Context, reqID string) (string, int64, error) {
	var status string
	var expiresAt int64
	err := s.db.QueryRowContext(ctx, `SELECT status, expires_at FROM requests WHERE request_id=?`, reqID).Scan(&status, &expiresAt)
	return status, expiresAt, err
}

func (s *store) insertToken(ctx context.Context, reqID, tokenHash string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tokens(request_id,token_hash,expires_at,created_at) VALUES(?,?,?,?)`,
		reqID, tokenHash, expiresAt.Unix(), time.Now().Unix(),
	)
	return err
}

func (s *store) markTokenUsed(ctx context.Context, reqID, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE tokens SET used_at=? WHERE request_id=? AND token_hash=?`, time.Now().Unix(), reqID, tokenHash)
	return err
}

func (s *store) verifyToken(ctx context.Context, reqID, tokenHash string) (bool, error) {
	var expiresAt int64
	err := s.db.QueryRowContext(ctx, `SELECT expires_at FROM tokens WHERE request_id=? AND token_hash=?`, reqID, tokenHash).Scan(&expiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	if time.Now().Unix() > expiresAt {
		return false, nil
	}
	return true, nil
}

func (s *store) insertAnswer(ctx context.Context, reqID, action, text string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO answers(request_id,action,text,created_at) VALUES(?,?,?,?)`,
		reqID, nullIfEmpty(action), nullIfEmpty(text), time.Now().Unix(),
	)
	return err
}

func nullIfEmpty(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

func (s *store) hasAnswer(ctx context.Context, reqID string) (bool, error) {
	var x int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM answers WHERE request_id=?`, reqID).Scan(&x)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *store) insertEvent(ctx context.Context, reqID, eventID, typ string, payload []byte) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO events(request_id,event_id,type,payload_json,created_at) VALUES(?,?,?,?,?)`,
		reqID, eventID, typ, string(payload), time.Now().Unix(),
	)
	return err
}

func (s *store) listEvents(ctx context.Context, reqID string, afterEventID string) ([]Event, error) {
	var rows *sql.Rows
	var err error
	if strings.TrimSpace(afterEventID) == "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT event_id, type, payload_json FROM events WHERE request_id=? ORDER BY seq ASC`,
			reqID,
		)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT e.event_id, e.type, e.payload_json
			 FROM events e
			 JOIN events a ON a.request_id=e.request_id AND a.event_id=?
			 WHERE e.request_id=? AND e.seq > a.seq
			 ORDER BY e.seq ASC`,
			afterEventID, reqID,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var id, typ, payload string
		if err := rows.Scan(&id, &typ, &payload); err != nil {
			return nil, err
		}
		out = append(out, Event{
			ID:        id,
			Type:      typ,
			RequestID: reqID,
			Data:      json.RawMessage(payload),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *store) getLatestEventByTypes(ctx context.Context, reqID string, types []string) (Event, bool, error) {
	if len(types) == 0 {
		return Event{}, false, nil
	}
	placeholders := make([]string, 0, len(types))
	args := make([]any, 0, 1+len(types))
	args = append(args, reqID)
	for _, t := range types {
		placeholders = append(placeholders, "?")
		args = append(args, t)
	}
	q := fmt.Sprintf(
		`SELECT event_id, type, payload_json FROM events WHERE request_id=? AND type IN (%s) ORDER BY seq DESC LIMIT 1`,
		strings.Join(placeholders, ","),
	)
	var id, typ, payload string
	err := s.db.QueryRowContext(ctx, q, args...).Scan(&id, &typ, &payload)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Event{}, false, nil
		}
		return Event{}, false, err
	}
	return Event{
		ID:        id,
		Type:      typ,
		RequestID: reqID,
		Data:      json.RawMessage(payload),
	}, true, nil
}

type askRequest struct {
	Title            string `json:"title"`
	Body             string `json:"body"`
	MCD              string `json:"mcd"`
	ExpiresInSeconds int    `json:"expires_in_seconds"`
}

type buttonSpec struct {
	Label string
	Value string
}

type inputSpec struct {
	Name   string
	Label  string
	Submit string
}

type mcdSpec struct {
	Buttons []buttonSpec
	Input   *inputSpec
}

var (
	reButtonsStart = regexp.MustCompile(`^\s*:::\s*buttons\s*$`)
	reInputStart   = regexp.MustCompile(`^\s*:::\s*input\b(.*)$`)
	reBlockEnd     = regexp.MustCompile(`^\s*:::\s*$`)
	reButtonLine   = regexp.MustCompile(`^\s*-\s*\[(.*?)\]\((.*?)\)\s*$`)
	reAttr         = regexp.MustCompile(`(\w+)\s*=\s*"([^"]*)"`)
)

func parseMCD(mcd string) mcdSpec {
	lines := strings.Split(mcd, "\n")
	var spec mcdSpec
	inButtons := false
	for _, ln := range lines {
		if inButtons {
			if reBlockEnd.MatchString(ln) {
				inButtons = false
				continue
			}
			if m := reButtonLine.FindStringSubmatch(ln); m != nil {
				label := strings.TrimSpace(m[1])
				value := strings.TrimSpace(m[2])
				if label != "" && value != "" {
					spec.Buttons = append(spec.Buttons, buttonSpec{Label: label, Value: value})
				}
			}
			continue
		}

		if reButtonsStart.MatchString(ln) {
			inButtons = true
			continue
		}

		if m := reInputStart.FindStringSubmatch(ln); m != nil {
			attrs := m[1]
			in := &inputSpec{
				Name:   "text",
				Label:  "Text",
				Submit: "Send",
			}
			for _, am := range reAttr.FindAllStringSubmatch(attrs, -1) {
				k := strings.ToLower(am[1])
				v := am[2]
				switch k {
				case "name":
					if strings.TrimSpace(v) != "" {
						in.Name = v
					}
				case "label":
					if strings.TrimSpace(v) != "" {
						in.Label = v
					}
				case "submit":
					if strings.TrimSpace(v) != "" {
						in.Submit = v
					}
				}
			}
			spec.Input = in
			continue
		}
	}
	return spec
}

type htmlData struct {
	Title     string
	Body      string
	Buttons   []buttonSpec
	Input     *inputSpec
	Action    string
	Text      string
	Done      bool
	Token     string
	RequestID string
}

var pageTpl = template.Must(template.New("page").Parse(`<!doctype html>
<html>
<head>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width,initial-scale=1"/>
  <title>{{.Title}}</title>
  <style>
    body{font-family:system-ui,-apple-system,Segoe UI,Roboto,sans-serif;max-width:720px;margin:32px auto;padding:0 16px;}
    pre{white-space:pre-wrap;word-break:break-word;background:#f6f8fa;padding:12px;border-radius:8px;}
    .row{margin-top:16px;}
    button{padding:10px 14px;border-radius:10px;border:1px solid #d0d7de;background:#fff;cursor:pointer;margin:6px 6px 0 0;}
    button:hover{background:#f6f8fa;}
    input[type="text"]{width:100%;padding:10px;border:1px solid #d0d7de;border-radius:10px;}
    .ok{padding:12px;border:1px solid #2da44e;border-radius:10px;background:#dafbe1;}
  </style>
</head>
<body>
  <h1>{{.Title}}</h1>
  <pre>{{.Body}}</pre>

  {{if .Done}}
    <div class="ok">Submitted.</div>
  {{else}}
    {{if .Buttons}}
      <div class="row">
        {{range .Buttons}}
          <form method="post" style="display:inline" action="./submit?k={{urlquery $.Token}}">
            <input type="hidden" name="action" value="{{.Value}}"/>
            <button type="submit">{{.Label}}</button>
          </form>
        {{end}}
      </div>
    {{end}}

    {{if .Input}}
      <div class="row">
        <form method="post" action="./submit?k={{urlquery .Token}}">
          <label>{{.Input.Label}}</label>
          <div style="height:8px"></div>
          <input type="text" name="text" value=""/>
          <div style="height:10px"></div>
          <button type="submit">{{.Input.Submit}}</button>
        </form>
      </div>
    {{end}}
  {{end}}
</body>
</html>`))

type server struct {
	cfg Config
	db  *store
	hub *runtimeHub
}

func (s *server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(auth, "Bearer ") {
			if strings.TrimSpace(strings.TrimPrefix(auth, "Bearer ")) == s.cfg.APIKey {
				next.ServeHTTP(w, r)
				return
			}
		}
		if r.Method == http.MethodGet {
			if strings.TrimSpace(r.URL.Query().Get("key")) == s.cfg.APIKey {
				next.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="ask4me"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/v1/ask", s.auth(http.HandlerFunc(s.handleAsk)))
	mux.HandleFunc("/r/", s.handleUser)
	return mux
}

func (s *server) handleAsk(w http.ResponseWriter, r *http.Request) {
	if parseBoolQuery(r.URL.Query().Get("stream")) {
		s.handleAskSSE(w, r)
		return
	}
	s.handleAskJSON(w, r)
}

func parseBoolQuery(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "t", "true", "y", "yes", "on":
		return true
	default:
		return false
	}
}

func (s *server) isTerminalEventType(typ string) bool {
	switch typ {
	case "user.submitted", "request.expired", "notify.failed":
		return true
	default:
		return false
	}
}

type askWaitResponse struct {
	RequestID     string          `json:"request_id"`
	LastEventType string          `json:"last_event_type"`
	LastEventID   string          `json:"last_event_id"`
	Data          json.RawMessage `json:"data"`
}

func (s *server) writeAskWaitResponse(w http.ResponseWriter, requestID string, ev Event) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Ask4Me-Request-Id", requestID)
	_ = json.NewEncoder(w).Encode(askWaitResponse{
		RequestID:     requestID,
		LastEventType: ev.Type,
		LastEventID:   ev.ID,
		Data:          ev.Data,
	})
}

func (s *server) getTerminalEventFromDB(ctx context.Context, requestID string) (Event, bool, error) {
	return s.db.getLatestEventByTypes(ctx, requestID, []string{"user.submitted", "request.expired", "notify.failed"})
}

func (s *server) waitTerminalEvent(ctx context.Context, requestID string) (Event, error) {
	if tev, ok := s.hub.getTerminal(requestID); ok {
		return tev, nil
	}
	if ev, ok, err := s.getTerminalEventFromDB(ctx, requestID); err != nil {
		return Event{}, err
	} else if ok {
		return ev, nil
	}

	ch, unsub := s.hub.subscribe(requestID)
	defer unsub()

	for {
		select {
		case <-ctx.Done():
			return Event{}, ctx.Err()
		case ev, ok := <-ch:
			if !ok {
				return Event{}, context.Canceled
			}
			if !s.isTerminalEventType(ev.Type) {
				continue
			}
			return ev, nil
		}
	}
}

func isValidRequestID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	if len(id) > 128 {
		return false
	}
	if !strings.HasPrefix(id, "req_") {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if c >= 'a' && c <= 'z' {
			continue
		}
		if c >= '0' && c <= '9' {
			continue
		}
		if c == '_' {
			continue
		}
		return false
	}
	return true
}

func parseAskRequestFromHTTP(r *http.Request) (askRequest, error) {
	var ar askRequest
	switch r.Method {
	case http.MethodPost:
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			return askRequest{}, err
		}
		if len(body) == 0 {
			body = []byte(`{}`)
		}
		if err := json.Unmarshal(body, &ar); err != nil {
			return askRequest{}, err
		}
	case http.MethodGet:
		q := r.URL.Query()
		ar.Title = q.Get("title")
		ar.Body = q.Get("body")
		ar.MCD = q.Get("mcd")
		ar.ExpiresInSeconds, _ = strconv.Atoi(strings.TrimSpace(q.Get("expires_in_seconds")))
	default:
		return askRequest{}, errors.New("method not allowed")
	}
	return ar, nil
}

func normalizeAskRequest(ar *askRequest) int {
	ar.Title = strings.TrimSpace(ar.Title)
	ar.Body = strings.TrimSpace(ar.Body)
	ar.MCD = strings.TrimSpace(ar.MCD)
	if ar.Title == "" {
		ar.Title = "Ask4Me"
	}
	if ar.Body == "" {
		ar.Body = "Please respond."
	}
	if ar.MCD == "" {
		ar.MCD = ":::buttons\n- [OK](ok)\n:::"
	}
	expiresIn := ar.ExpiresInSeconds
	if expiresIn <= 0 {
		expiresIn = 0
	}
	return expiresIn
}

func (s *server) createAskWithRequestID(ctx context.Context, requestID string, ar askRequest, sendTo http.ResponseWriter) (askRequest, time.Time, string, string, error) {
	expiresIn := normalizeAskRequest(&ar)
	if expiresIn <= 0 {
		expiresIn = s.cfg.DefaultExpiresInSeconds
	}
	expiresAt := time.Now().Add(time.Duration(expiresIn) * time.Second)
	if err := s.db.createRequest(ctx, requestID, ar.Title, ar.Body, ar.MCD, "created", expiresAt); err != nil {
		return askRequest{}, time.Time{}, "", "", err
	}

	tokenPlain := genToken()
	tokenHash := sha256Hex(tokenPlain)
	if err := s.db.insertToken(ctx, requestID, tokenHash, expiresAt); err != nil {
		return askRequest{}, time.Time{}, "", "", err
	}

	interactionURL := s.makeInteractionURL(requestID, tokenPlain)
	ev := s.mustNewEvent(ctx, requestID, "request.created", map[string]any{
		"interaction_url": interactionURL,
		"expires_at":      expiresAt.UTC().Format(time.RFC3339),
	})

	if sendTo != nil {
		if err := s.persistAndSendEvent(ctx, sendTo, ev); err != nil {
			return askRequest{}, time.Time{}, "", "", err
		}
	} else {
		_ = s.persistTerminalAware(ctx, ev)
	}

	return ar, expiresAt, interactionURL, ev.ID, nil
}

func (s *server) handleAskJSON(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requestID := strings.TrimSpace(r.URL.Query().Get("request_id"))
	if requestID != "" && !isValidRequestID(requestID) {
		http.Error(w, "invalid request_id", http.StatusBadRequest)
		return
	}
	if requestID == "" {
		requestID = genID("req_")
		ar, err := parseAskRequestFromHTTP(r)
		if err != nil {
			if err.Error() == "method not allowed" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		ar2, expiresAt, interactionURL, _, err := s.createAskWithRequestID(ctx, requestID, ar, nil)
		if err != nil {
			http.Error(w, "failed to create request", http.StatusInternalServerError)
			return
		}

		go s.sendNotification(context.Background(), requestID, ar2.Title, ar2.Body, interactionURL)
		go s.expireLoop(context.Background(), requestID, expiresAt)

		tev, err := s.waitTerminalEvent(ctx, requestID)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		s.writeAskWaitResponse(w, requestID, tev)
		return
	}

	status, _, err := s.db.getRequestStatus(ctx, requestID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if tev, ok := s.hub.getTerminal(requestID); ok {
				s.writeAskWaitResponse(w, requestID, tev)
				return
			}
			ar, err := parseAskRequestFromHTTP(r)
			if err != nil {
				if err.Error() == "method not allowed" {
					http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
					return
				}
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			ar2, expiresAt, interactionURL, _, err := s.createAskWithRequestID(ctx, requestID, ar, nil)
			if err != nil {
				http.Error(w, "failed to create request", http.StatusInternalServerError)
				return
			}
			go s.sendNotification(context.Background(), requestID, ar2.Title, ar2.Body, interactionURL)
			go s.expireLoop(context.Background(), requestID, expiresAt)

			tev, err := s.waitTerminalEvent(ctx, requestID)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			s.writeAskWaitResponse(w, requestID, tev)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if status == "submitted" || status == "expired" || status == "notify_failed" {
		if tev, ok := s.hub.getTerminal(requestID); ok {
			s.writeAskWaitResponse(w, requestID, tev)
			return
		}
		if tev, ok, err := s.getTerminalEventFromDB(ctx, requestID); err == nil && ok {
			s.writeAskWaitResponse(w, requestID, tev)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	tev, err := s.waitTerminalEvent(ctx, requestID)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.writeAskWaitResponse(w, requestID, tev)
}

func (s *server) handleAskSSE(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requestID := strings.TrimSpace(r.URL.Query().Get("request_id"))
	lastEventID := strings.TrimSpace(r.URL.Query().Get("last_event_id"))
	if requestID != "" && !isValidRequestID(requestID) {
		http.Error(w, "invalid request_id", http.StatusBadRequest)
		return
	}

	if requestID == "" {
		requestID = genID("req_")
		ar, err := parseAskRequestFromHTTP(r)
		if err != nil {
			if err.Error() == "method not allowed" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		sseInit(w)
		w.Header().Set("X-Ask4Me-Request-Id", requestID)
		fl, _ := w.(http.Flusher)
		if fl != nil {
			fl.Flush()
		}

		ar2, expiresAt, interactionURL, firstEventID, err := s.createAskWithRequestID(ctx, requestID, ar, w)
		if err != nil {
			http.Error(w, "failed to create request", http.StatusInternalServerError)
			return
		}

		go s.sendNotification(context.Background(), requestID, ar2.Title, ar2.Body, interactionURL)
		go s.expireLoop(context.Background(), requestID, expiresAt)

		s.streamUntilDone(ctx, w, requestID, firstEventID)
		return
	}

	sseInit(w)
	w.Header().Set("X-Ask4Me-Request-Id", requestID)
	fl, _ := w.(http.Flusher)
	if fl != nil {
		fl.Flush()
	}

	status, _, err := s.db.getRequestStatus(ctx, requestID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if tev, ok := s.hub.getTerminal(requestID); ok {
				_ = s.sendEvent(w, tev)
				s.sendDone(w)
				return
			}
			ar, err := parseAskRequestFromHTTP(r)
			if err != nil {
				if err.Error() == "method not allowed" {
					http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
					return
				}
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			ar2, expiresAt, interactionURL, firstEventID, err := s.createAskWithRequestID(ctx, requestID, ar, w)
			if err != nil {
				http.Error(w, "failed to create request", http.StatusInternalServerError)
				return
			}
			go s.sendNotification(context.Background(), requestID, ar2.Title, ar2.Body, interactionURL)
			go s.expireLoop(context.Background(), requestID, expiresAt)

			s.streamUntilDone(ctx, w, requestID, firstEventID)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.replayEvents(ctx, w, requestID, lastEventID)
	if status == "submitted" || status == "expired" {
		s.sendDone(w)
		return
	}

	s.streamUntilDone(ctx, w, requestID, lastEventID)
}

func sseInit(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
}

func (s *server) makeInteractionURL(requestID, tokenPlain string) string {
	base := strings.TrimRight(s.cfg.BaseURL, "/")
	return fmt.Sprintf("%s/r/%s/?k=%s", base, url.PathEscape(requestID), url.QueryEscape(tokenPlain))
}

func (s *server) mustNewEvent(ctx context.Context, requestID, typ string, data any) Event {
	evID := genID("evt_")
	b, _ := json.Marshal(data)
	return Event{
		ID:        evID,
		Type:      typ,
		Time:      time.Now().UTC().Format(time.RFC3339),
		RequestID: requestID,
		Data:      json.RawMessage(b),
	}
}

func (s *server) persistAndSendEvent(ctx context.Context, w http.ResponseWriter, ev Event) error {
	payload, err := json.Marshal(ev.Data)
	if err != nil {
		return err
	}
	if err := s.db.insertEvent(ctx, ev.RequestID, ev.ID, ev.Type, payload); err != nil {
		return err
	}
	s.hub.publish(ev)
	return s.sendEvent(w, ev)
}

func (s *server) sendEvent(w http.ResponseWriter, ev Event) error {
	ev.Time = time.Now().UTC().Format(time.RFC3339)
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, "data: ")
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, "\n\n")
	if err != nil {
		return err
	}
	if fl, ok := w.(http.Flusher); ok {
		fl.Flush()
	}
	return nil
}

func (s *server) sendDone(w http.ResponseWriter) {
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	if fl, ok := w.(http.Flusher); ok {
		fl.Flush()
	}
}

func (s *server) replayEvents(ctx context.Context, w http.ResponseWriter, requestID, afterEventID string) {
	evs, err := s.db.listEvents(ctx, requestID, afterEventID)
	if err != nil {
		return
	}
	for _, ev := range evs {
		ev.Time = time.Now().UTC().Format(time.RFC3339)
		_ = s.sendEvent(w, ev)
	}
}

func (s *server) streamUntilDone(ctx context.Context, w http.ResponseWriter, requestID, lastEventID string) {
	ch, unsub := s.hub.subscribe(requestID)
	defer unsub()

	seen := map[string]struct{}{}
	if strings.TrimSpace(lastEventID) != "" {
		seen[lastEventID] = struct{}{}
	}
	evs, err := s.db.listEvents(ctx, requestID, lastEventID)
	if err == nil && len(evs) > 0 {
		for _, ev := range evs {
			seen[ev.ID] = struct{}{}
			lastEventID = ev.ID
			_ = s.sendEvent(w, ev)
			if s.isTerminalEventType(ev.Type) {
				s.sendDone(w)
				return
			}
		}
	}

	hb := time.NewTicker(time.Duration(s.cfg.SSEHeartbeatIntervalSeconds) * time.Second)
	defer hb.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-hb.C:
			ev := Event{
				ID:        "",
				Type:      "heartbeat",
				Time:      time.Now().UTC().Format(time.RFC3339),
				RequestID: requestID,
				Data:      json.RawMessage([]byte(`{}`)),
			}
			_ = s.sendEvent(w, ev)
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if ev.ID != "" {
				if _, ok := seen[ev.ID]; ok {
					continue
				}
				seen[ev.ID] = struct{}{}
			}
			if lastEventID != "" && ev.ID == lastEventID {
				continue
			}
			if err := s.sendEvent(w, ev); err != nil {
				return
			}
			lastEventID = ev.ID
			if s.isTerminalEventType(ev.Type) {
				s.sendDone(w)
				return
			}
		}
	}
}

func normalizeAppriseURL(s string) string {
	v := strings.TrimSpace(s)
	low := strings.ToLower(v)
	if strings.HasPrefix(low, "serverchan://") {
		return "schan://" + v[len("serverchan://"):]
	}
	return v
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\"'\"'`) + "'"
}

func formatShellCommand(bin string, args []string) string {
	parts := make([]string, 0, 1+len(args))
	parts = append(parts, shellQuote(bin))
	for _, a := range args {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

func (s *server) sendNotification(ctx context.Context, requestID, title, body, interactionURL string) {
	msg := strings.TrimSpace(body)
	if msg == "" {
		msg = "Please respond."
	}
	if interactionURL != "" {
		msg = msg + "\n\n" + interactionURL
	}

	sendkey := strings.TrimSpace(s.cfg.ServerChanSendKey)
	if sendkey != "" {
		resp, err := serverchan_sdk.ScSend(sendkey, title, msg, &serverchan_sdk.ScSendOptions{
			Tags: "ask4me",
		})
		if err != nil {
			ev := s.mustNewEvent(ctx, requestID, "notify.failed", map[string]any{
				"channel": "serverchan",
				"error":   err.Error(),
			})
			_ = s.persistTerminalAware(ctx, ev)
			s.hub.setTerminal(ev)
			_ = s.db.updateRequestStatus(ctx, requestID, "notify_failed")
			return
		}
		if resp != nil && resp.Code != 0 {
			output, _ := json.Marshal(resp)
			ev := s.mustNewEvent(ctx, requestID, "notify.failed", map[string]any{
				"channel": "serverchan",
				"error":   fmt.Sprintf("serverchan code %d: %s", resp.Code, resp.Message),
				"output":  truncate(string(output), 2000),
			})
			_ = s.persistTerminalAware(ctx, ev)
			s.hub.setTerminal(ev)
			_ = s.db.updateRequestStatus(ctx, requestID, "notify_failed")
			return
		}

		ev := s.mustNewEvent(ctx, requestID, "notify.sent", map[string]any{
			"channel": "serverchan",
		})
		_ = s.persistTerminalAware(ctx, ev)
		_ = s.db.updateRequestStatus(ctx, requestID, "delivered")
		return
	}

	if len(s.cfg.AppriseURLs) == 0 {
		ev := s.mustNewEvent(ctx, requestID, "notify.failed", map[string]any{
			"error": "no serverchan_sendkey or apprise_urls configured",
		})
		_ = s.persistTerminalAware(ctx, ev)
		s.hub.setTerminal(ev)
		_ = s.db.updateRequestStatus(ctx, requestID, "notify_failed")
		return
	}

	args := []string{"-vv", "--title", title, "--body", msg}
	for _, u := range s.cfg.AppriseURLs {
		v := normalizeAppriseURL(u)
		if v != "" {
			args = append(args, v)
		}
	}
	cmdlineSh := formatShellCommand(s.cfg.AppriseBin, args)

	cmd := exec.CommandContext(ctx, s.cfg.AppriseBin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		ev := s.mustNewEvent(ctx, requestID, "notify.failed", map[string]any{
			"channel":      "apprise",
			"error":        err.Error(),
			"command":      cmdlineSh,
			"command_sh":   cmdlineSh,
			"command_args": args,
			"output":       truncate(string(out), 2000),
		})
		_ = s.persistTerminalAware(ctx, ev)
		s.hub.setTerminal(ev)
		_ = s.db.updateRequestStatus(ctx, requestID, "notify_failed")
		return
	}

	ev := s.mustNewEvent(ctx, requestID, "notify.sent", map[string]any{
		"channel":      "apprise",
		"command":      cmdlineSh,
		"command_sh":   cmdlineSh,
		"command_args": args,
	})
	_ = s.persistTerminalAware(ctx, ev)
	_ = s.db.updateRequestStatus(ctx, requestID, "delivered")
}

func (s *server) persistTerminalAware(ctx context.Context, ev Event) error {
	payload, err := json.Marshal(ev.Data)
	if err != nil {
		return err
	}
	if err := s.db.insertEvent(ctx, ev.RequestID, ev.ID, ev.Type, payload); err != nil {
		return err
	}
	s.hub.publish(ev)
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func (s *server) expireLoop(ctx context.Context, requestID string, expiresAt time.Time) {
	timer := time.NewTimer(time.Until(expiresAt))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
		has, err := s.db.hasAnswer(ctx, requestID)
		if err != nil || has {
			return
		}
		_ = s.db.updateRequestStatus(ctx, requestID, "expired")
		ev := s.mustNewEvent(ctx, requestID, "request.expired", map[string]any{})
		_ = s.persistTerminalAware(ctx, ev)
		s.hub.setTerminal(ev)
	}
}

func (s *server) handleUser(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/r/")
	parts := strings.SplitN(path, "/", 2)
	requestID := parts[0]
	if requestID == "" {
		http.NotFound(w, r)
		return
	}
	tokenPlain := r.URL.Query().Get("k")
	if tokenPlain == "" {
		http.Error(w, "missing token", http.StatusBadRequest)
		return
	}
	tokenHash := sha256Hex(tokenPlain)
	ok, err := s.db.verifyToken(r.Context(), requestID, tokenHash)
	if err != nil || !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	status, expiresAtUnix, err := s.db.getRequestStatus(r.Context(), requestID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if time.Now().Unix() > expiresAtUnix {
		http.Error(w, "expired", http.StatusGone)
		return
	}

	if len(parts) == 2 && parts[1] == "submit" {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if status == "submitted" || status == "expired" {
			http.Redirect(w, r, "./?k="+url.QueryEscape(tokenPlain), http.StatusSeeOther)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		action := strings.TrimSpace(r.FormValue("action"))
		text := strings.TrimSpace(r.FormValue("text"))
		if action == "" && text == "" {
			http.Error(w, "empty submission", http.StatusBadRequest)
			return
		}
		if err := s.db.insertAnswer(r.Context(), requestID, action, text); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "unique") {
				http.Redirect(w, r, "./?k="+url.QueryEscape(tokenPlain), http.StatusSeeOther)
				return
			}
			http.Error(w, "failed", http.StatusInternalServerError)
			return
		}
		_ = s.db.markTokenUsed(r.Context(), requestID, tokenHash)
		_ = s.db.updateRequestStatus(r.Context(), requestID, "submitted")
		ev := s.mustNewEvent(r.Context(), requestID, "user.submitted", map[string]any{
			"action": action,
			"text":   text,
		})
		_ = s.persistTerminalAware(r.Context(), ev)
		s.hub.setTerminal(ev)
		http.Redirect(w, r, "./?k="+url.QueryEscape(tokenPlain), http.StatusSeeOther)
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var title, body, mcd string
	err = s.db.db.QueryRowContext(r.Context(), `SELECT title, body, mcd FROM requests WHERE request_id=?`, requestID).Scan(&title, &body, &mcd)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	spec := parseMCD(mcd)
	done := status == "submitted" || status == "expired"

	if status != "submitted" && status != "expired" {
		ev := s.mustNewEvent(r.Context(), requestID, "user.page_loaded", map[string]any{})
		_ = s.persistTerminalAware(r.Context(), ev)
	}

	data := htmlData{
		Title:     title,
		Body:      body,
		Buttons:   spec.Buttons,
		Input:     spec.Input,
		Done:      done,
		Token:     tokenPlain,
		RequestID: requestID,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pageTpl.Execute(w, data)
}

func genID(prefix string) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	s := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
	s = strings.ToLower(s)
	return prefix + s
}

func genToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	const hex = "0123456789abcdef"
	out := make([]byte, 64)
	for i, v := range sum {
		out[i*2] = hex[v>>4]
		out[i*2+1] = hex[v&0x0f]
	}
	return string(out)
}

func loadConfigYAML(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, cfg.normalize()
}

func loadConfigFromDotenv(path string) (Config, error) {
	m, err := parseDotenvFile(path)
	if err != nil {
		return Config{}, err
	}
	for k, v := range m {
		_ = os.Setenv(k, v)
	}

	cfg := Config{
		BaseURL:                     strings.TrimSpace(envFirst("ASK4ME_BASE_URL", "BASE_URL")),
		APIKey:                      strings.TrimSpace(envFirst("ASK4ME_API_KEY", "API_KEY")),
		ServerChanSendKey:           strings.TrimSpace(envFirst("ASK4ME_SERVERCHAN_SENDKEY", "SERVERCHAN_SENDKEY")),
		AppriseURLs:                 parseCSVStrings(envFirst("ASK4ME_APPRISE_URLS", "APPRISE_URLS")),
		AppriseBin:                  strings.TrimSpace(envFirst("ASK4ME_APPRISE_BIN", "APPRISE_BIN")),
		SQLitePath:                  strings.TrimSpace(envFirst("ASK4ME_SQLITE_PATH", "SQLITE_PATH")),
		DefaultExpiresInSeconds:     parseEnvInt(envFirst("ASK4ME_DEFAULT_EXPIRES_IN_SECONDS", "DEFAULT_EXPIRES_IN_SECONDS")),
		SSEHeartbeatIntervalSeconds: parseEnvInt(envFirst("ASK4ME_SSE_HEARTBEAT_INTERVAL_SECONDS", "SSE_HEARTBEAT_INTERVAL_SECONDS")),
		ListenAddr:                  strings.TrimSpace(envFirst("ASK4ME_LISTEN_ADDR", "LISTEN_ADDR")),
		TerminalCacheSeconds:        parseEnvInt(envFirst("ASK4ME_TERMINAL_CACHE_SECONDS", "TERMINAL_CACHE_SECONDS")),
	}
	if cfg.BaseURL == "" {
		return Config{}, errors.New("ASK4ME_BASE_URL is required")
	}
	if cfg.APIKey == "" {
		return Config{}, errors.New("ASK4ME_API_KEY is required")
	}
	return cfg, cfg.normalize()
}

func loadConfigAny(path string) (Config, error) {
	base := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".yaml", ".yml":
		return loadConfigYAML(path)
	case ".env":
		return loadConfigFromDotenv(path)
	default:
		if strings.HasPrefix(base, ".env") {
			return loadConfigFromDotenv(path)
		}
		cfg, err := loadConfigYAML(path)
		if err == nil {
			return cfg, nil
		}
		cfg2, err2 := loadConfigFromDotenv(path)
		if err2 == nil {
			return cfg2, nil
		}
		return Config{}, fmt.Errorf("unrecognized config file: yaml error: %v; dotenv error: %v", err, err2)
	}
}

func loadConfigAuto(configPath string) (Config, string, error) {
	if strings.TrimSpace(configPath) != "" {
		cfg, err := loadConfigAny(configPath)
		return cfg, configPath, err
	}

	if fileExists("./.env") {
		cfg, err := loadConfigFromDotenv("./.env")
		return cfg, "./.env", err
	}

	candidates := []string{
		"./ask4me.yaml",
		"./ask4me.yml",
		"./ask for me.yml",
		"./ask-for-me.yml",
		"./ask_for_me.yml",
	}
	if p, ok := findFirstExisting(candidates); ok {
		cfg, err := loadConfigYAML(p)
		return cfg, p, err
	}
	return Config{}, "", errors.New("no config found: expected ./.env or ./ask4me.yaml (or ./ask for me.yml)")
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func findFirstExisting(paths []string) (string, bool) {
	for _, p := range paths {
		if fileExists(p) {
			return p, true
		}
	}
	return "", false
}

func envFirst(keys ...string) string {
	for _, k := range keys {
		if v, ok := os.LookupEnv(k); ok {
			return v
		}
	}
	return ""
}

func parseEnvInt(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return v
}

func parseCSVStrings(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	s = strings.ReplaceAll(s, "\n", ",")
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseDotenvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	m := make(map[string]string)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		k, v, ok, err := parseDotenvLine(line)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if !ok {
			continue
		}
		m[k] = v
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return m, nil
}

func parseDotenvLine(line string) (key, value string, ok bool, err error) {
	s := strings.TrimSpace(line)
	s = strings.TrimPrefix(s, "\uFEFF")
	if s == "" || strings.HasPrefix(s, "#") {
		return "", "", false, nil
	}
	if strings.HasPrefix(s, "export ") {
		s = strings.TrimSpace(strings.TrimPrefix(s, "export "))
	}
	i := strings.IndexByte(s, '=')
	if i <= 0 {
		return "", "", false, fmt.Errorf("invalid dotenv line: %q", line)
	}
	key = strings.TrimSpace(s[:i])
	key = strings.TrimPrefix(key, "\uFEFF")
	value = strings.TrimSpace(s[i+1:])
	if key == "" {
		return "", "", false, fmt.Errorf("invalid dotenv line: %q", line)
	}
	if value == "" {
		return key, "", true, nil
	}

	if (strings.HasPrefix(value, `"`)) || (strings.HasPrefix(value, "'")) || (strings.HasPrefix(value, "`")) {
		if len(value) >= 2 && value[0] == value[len(value)-1] {
			switch value[0] {
			case '"':
				u, e := strconv.Unquote(value)
				if e != nil {
					return "", "", false, fmt.Errorf("invalid quoted value: %q", line)
				}
				return key, u, true, nil
			case '\'':
				return key, value[1 : len(value)-1], true, nil
			case '`':
				return key, value[1 : len(value)-1], true, nil
			}
		}
		return key, value, true, nil
	}

	if idx := strings.Index(value, " #"); idx >= 0 {
		value = strings.TrimSpace(value[:idx])
	}
	if idx := strings.Index(value, "\t#"); idx >= 0 {
		value = strings.TrimSpace(value[:idx])
	}
	return key, value, true, nil
}

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "", "config file path (.env or .yml/.yaml). If empty, auto-detect: .env then ask4me.yaml")
	flag.Parse()

	cfg, used, err := loadConfigAuto(configPath)
	if err != nil {
		if used != "" {
			fmt.Fprintf(os.Stderr, "load config (%s): %s\n", used, err.Error())
		} else {
			fmt.Fprintln(os.Stderr, err.Error())
		}
		os.Exit(1)
	}

	sqlitePath := cfg.SQLitePath
	if !filepath.IsAbs(sqlitePath) {
		if abs, err := filepath.Abs(sqlitePath); err == nil {
			sqlitePath = abs
		}
	}

	db, err := sql.Open("sqlite", sqlitePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	defer db.Close()

	db.SetMaxOpenConns(1)
	st, err := newStore(db)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	hub := newRuntimeHub(time.Duration(cfg.TerminalCacheSeconds) * time.Second)
	srv := &server{cfg: cfg, db: st, hub: hub}

	httpSrv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           srv.routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ln, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "listening on %s\n", ln.Addr().String())
	_ = httpSrv.Serve(ln)
}

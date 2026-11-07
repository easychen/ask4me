package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	multiUserModeAllowlist = "allowlist"
	multiUserModeOpen      = "open"

	defaultMultiUserRequestsPerDay       = 200
	defaultMultiUserMaxPending           = 5
	defaultMultiUserMaxExpiresInSeconds  = 86400
	defaultMultiUserPerIPRequestsPerHour = 60
	defaultMultiUserPerIPNewKeysPerDay   = 10
	defaultMultiUserGlobalRequestsPerDay = 5000

	multiUserKeyPrefix    = "a4m"
	multiUserMinRandomLen = 16
)

var defaultAppriseSchemes = []string{"schan", "tgram", "discord", "slack", "ntfy", "gotify"}

// MultiUserConfig controls the accountless multi-tenant mode. When Enabled is
// false the server behaves exactly like the original single-user build.
type MultiUserConfig struct {
	Enabled               bool                    `yaml:"enabled"`
	Auth                  MultiUserAuthConfig     `yaml:"auth"`
	Defaults              MultiUserDefaultsConfig `yaml:"defaults"`
	Limits                MultiUserLimitsConfig   `yaml:"limits"`
	AllowedAppriseSchemes []string                `yaml:"allowed_apprise_schemes"`
}

type MultiUserAuthConfig struct {
	Mode        string               `yaml:"mode"`
	InviteCodes []string             `yaml:"invite_codes"`
	Keys        []MultiUserKeyConfig `yaml:"keys"`
}

type MultiUserKeyConfig struct {
	Key            string `yaml:"key"`
	Name           string `yaml:"name"`
	RequestsPerDay int    `yaml:"requests_per_day"`
	MaxPending     int    `yaml:"max_pending"`
}

type MultiUserDefaultsConfig struct {
	RequestsPerDay      int `yaml:"requests_per_day"`
	MaxPending          int `yaml:"max_pending"`
	MaxExpiresInSeconds int `yaml:"max_expires_in_seconds"`
}

type MultiUserLimitsConfig struct {
	PerIPRequestsPerHour int  `yaml:"per_ip_requests_per_hour"`
	PerIPNewKeysPerDay   int  `yaml:"per_ip_new_keys_per_day"`
	GlobalRequestsPerDay int  `yaml:"global_requests_per_day"`
	TrustProxy           bool `yaml:"trust_proxy"`
}

func (m *MultiUserConfig) normalize() error {
	if err := m.normalizeAppriseSchemes(); err != nil {
		return err
	}
	if !m.Enabled {
		return nil
	}

	m.Auth.Mode = strings.ToLower(strings.TrimSpace(m.Auth.Mode))
	if m.Auth.Mode == "" {
		m.Auth.Mode = multiUserModeAllowlist
	}
	if m.Auth.Mode != multiUserModeAllowlist && m.Auth.Mode != multiUserModeOpen {
		return fmt.Errorf("multi_user.auth.mode must be %q or %q", multiUserModeAllowlist, multiUserModeOpen)
	}

	if m.Defaults.RequestsPerDay <= 0 {
		m.Defaults.RequestsPerDay = defaultMultiUserRequestsPerDay
	}
	if m.Defaults.MaxPending <= 0 {
		m.Defaults.MaxPending = defaultMultiUserMaxPending
	}
	if m.Defaults.MaxExpiresInSeconds <= 0 {
		m.Defaults.MaxExpiresInSeconds = defaultMultiUserMaxExpiresInSeconds
	}
	if m.Limits.PerIPRequestsPerHour <= 0 {
		m.Limits.PerIPRequestsPerHour = defaultMultiUserPerIPRequestsPerHour
	}
	if m.Limits.PerIPNewKeysPerDay <= 0 {
		m.Limits.PerIPNewKeysPerDay = defaultMultiUserPerIPNewKeysPerDay
	}
	if m.Limits.GlobalRequestsPerDay <= 0 {
		m.Limits.GlobalRequestsPerDay = defaultMultiUserGlobalRequestsPerDay
	}

	seenKeys := map[string]struct{}{}
	keys := make([]MultiUserKeyConfig, 0, len(m.Auth.Keys))
	for _, kc := range m.Auth.Keys {
		key := strings.TrimSpace(kc.Key)
		if key == "" {
			continue
		}
		if len(key) < 8 || len(key) > 512 {
			return errors.New("multi_user.auth.keys: key length must be between 8 and 512 bytes")
		}
		if strings.ContainsAny(key, " \t\r\n") {
			return errors.New("multi_user.auth.keys: key must not contain whitespace")
		}
		if _, ok := seenKeys[key]; ok {
			return errors.New("multi_user.auth.keys: duplicate key")
		}
		seenKeys[key] = struct{}{}
		kc.Key = key
		kc.Name = strings.TrimSpace(kc.Name)
		if kc.Name == "" {
			kc.Name = "key"
		}
		if kc.RequestsPerDay <= 0 {
			kc.RequestsPerDay = m.Defaults.RequestsPerDay
		}
		if kc.MaxPending <= 0 {
			kc.MaxPending = m.Defaults.MaxPending
		}
		keys = append(keys, kc)
	}
	m.Auth.Keys = keys

	seenInvites := map[string]struct{}{}
	invites := make([]string, 0, len(m.Auth.InviteCodes))
	for _, invite := range m.Auth.InviteCodes {
		invite = strings.TrimSpace(invite)
		if invite == "" {
			continue
		}
		if len(invite) < 4 || len(invite) > 64 {
			return errors.New("multi_user.auth.invite_codes: invite length must be between 4 and 64 bytes")
		}
		if !isValidInviteCode(invite) {
			return fmt.Errorf("multi_user.auth.invite_codes: invalid invite %q (allowed: A-Z a-z 0-9 -)", invite)
		}
		if _, ok := seenInvites[invite]; ok {
			continue
		}
		seenInvites[invite] = struct{}{}
		invites = append(invites, invite)
	}
	m.Auth.InviteCodes = invites

	if m.Auth.Mode == multiUserModeOpen && len(invites) == 0 {
		return errors.New("multi_user.auth.invite_codes is required when auth.mode=open")
	}
	return nil
}

func (m *MultiUserConfig) normalizeAppriseSchemes() error {
	if len(m.AllowedAppriseSchemes) == 0 {
		m.AllowedAppriseSchemes = append([]string{}, defaultAppriseSchemes...)
	}
	seen := map[string]struct{}{}
	schemes := make([]string, 0, len(m.AllowedAppriseSchemes))
	for _, scheme := range m.AllowedAppriseSchemes {
		scheme = strings.ToLower(strings.TrimSpace(scheme))
		if scheme == "" {
			continue
		}
		if strings.ContainsAny(scheme, ":/ \t\r\n") {
			return fmt.Errorf("multi_user.allowed_apprise_schemes: invalid scheme %q", scheme)
		}
		if _, ok := seen[scheme]; ok {
			continue
		}
		seen[scheme] = struct{}{}
		schemes = append(schemes, scheme)
	}
	if len(schemes) == 0 {
		return errors.New("multi_user.allowed_apprise_schemes must not be empty")
	}
	m.AllowedAppriseSchemes = schemes
	return nil
}

func isValidInviteCode(s string) bool {
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '-':
		default:
			return false
		}
	}
	return true
}

type tenantQuota struct {
	RequestsPerDay      int
	MaxPending          int
	MaxExpiresInSeconds int
}

type tenant struct {
	KeyHash string
	Name    string
	Quota   tenantQuota
}

type tenantContextKey struct{}

type tenantRegistry struct {
	keys            map[string]tenant
	invites         map[string]struct{}
	mode            string
	allowedSchemes  map[string]struct{}
	allowAllSchemes bool
}

func tenantFromContext(ctx context.Context) (tenant, bool) {
	t, ok := ctx.Value(tenantContextKey{}).(tenant)
	return t, ok
}

func defaultTenantQuota(d MultiUserDefaultsConfig) tenantQuota {
	return tenantQuota{
		RequestsPerDay:      d.RequestsPerDay,
		MaxPending:          d.MaxPending,
		MaxExpiresInSeconds: d.MaxExpiresInSeconds,
	}
}

func (s *server) initTenants() error {
	reg := &tenantRegistry{
		keys:           map[string]tenant{},
		invites:        map[string]struct{}{},
		mode:           s.cfg.MultiUser.Auth.Mode,
		allowedSchemes: map[string]struct{}{},
	}
	schemes := s.cfg.MultiUser.AllowedAppriseSchemes
	if len(schemes) == 0 {
		schemes = defaultAppriseSchemes
	}
	for _, scheme := range schemes {
		scheme = strings.ToLower(strings.TrimSpace(scheme))
		if scheme == "*" {
			reg.allowAllSchemes = true
			continue
		}
		if scheme != "" {
			reg.allowedSchemes[scheme] = struct{}{}
		}
	}

	if s.cfg.MultiUser.Enabled {
		for _, invite := range s.cfg.MultiUser.Auth.InviteCodes {
			reg.invites[invite] = struct{}{}
		}
		for _, kc := range s.cfg.MultiUser.Auth.Keys {
			t := tenant{
				KeyHash: sha256Hex(kc.Key),
				Name:    kc.Name,
				Quota: tenantQuota{
					RequestsPerDay:      kc.RequestsPerDay,
					MaxPending:          kc.MaxPending,
					MaxExpiresInSeconds: s.cfg.MultiUser.Defaults.MaxExpiresInSeconds,
				},
			}
			reg.keys[t.KeyHash] = t
		}
	}

	s.tenants = reg
	return nil
}

func (s *server) resolveTenant(key string) (tenant, bool) {
	if s.tenants == nil || key == "" || len(key) > 512 {
		return tenant{}, false
	}
	h := sha256Hex(key)
	if t, ok := s.tenants.keys[h]; ok {
		return t, true
	}
	if len(key) < 8 {
		return tenant{}, false
	}
	if s.cfg.MultiUser.Enabled && s.tenants.mode == multiUserModeOpen {
		invite, ok := inviteFromKey(key)
		if !ok {
			return tenant{}, false
		}
		if _, ok := s.tenants.invites[invite]; !ok {
			return tenant{}, false
		}
		return tenant{
			KeyHash: h,
			Name:    "invite:" + invite,
			Quota:   defaultTenantQuota(s.cfg.MultiUser.Defaults),
		}, true
	}
	return tenant{}, false
}

// inviteFromKey parses a4m_<invite>_<random>. Invite codes never contain "_",
// so the random part may contain underscores without ambiguity.
func inviteFromKey(key string) (string, bool) {
	parts := strings.SplitN(key, "_", 3)
	if len(parts) != 3 || parts[0] != multiUserKeyPrefix || parts[1] == "" || len(parts[2]) < multiUserMinRandomLen {
		return "", false
	}
	for _, c := range parts[1] {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
			return "", false
		}
	}
	for _, c := range parts[2] {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return "", false
		}
	}
	return parts[1], true
}

func extractAPIKey(r *http.Request) (key string, fromQuery bool) {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		if k := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer ")); k != "" {
			return k, false
		}
	}
	if r.Method == http.MethodGet {
		if k := strings.TrimSpace(r.URL.Query().Get("key")); k != "" {
			return k, true
		}
	}
	return "", false
}

func (s *server) writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="ask4me"`)
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}

type askHTTPError struct {
	Status  int
	Message string
}

func (e *askHTTPError) Error() string {
	return e.Message
}

type quotaError struct {
	Message    string
	RetryAfter int
}

func (e *quotaError) Error() string {
	return e.Message
}

func startOfUTCDay(now time.Time) time.Time {
	y, m, d := now.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func secondsUntilUTCDayEnd(now time.Time) int {
	next := startOfUTCDay(now).Add(24 * time.Hour)
	sec := int(time.Until(next).Seconds())
	if sec < 1 {
		sec = 1
	}
	return sec
}

func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
			if i := strings.IndexByte(xff, ','); i >= 0 {
				xff = xff[:i]
			}
			if ip := strings.TrimSpace(xff); ip != "" {
				return ip
			}
		}
		if xr := strings.TrimSpace(r.Header.Get("X-Real-IP")); xr != "" {
			return xr
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func (s *server) checkCreateQuota(ctx context.Context, t tenant, ip string) error {
	if !s.cfg.MultiUser.Enabled {
		return nil
	}

	now := time.Now().UTC()
	dayStart := startOfUTCDay(now)

	if t.Quota.RequestsPerDay > 0 {
		n, err := s.db.countRequestsByKeySince(ctx, t.KeyHash, dayStart)
		if err != nil {
			return fmt.Errorf("quota check: %w", err)
		}
		if n >= t.Quota.RequestsPerDay {
			return &quotaError{
				Message:    fmt.Sprintf("daily request quota exceeded (%d/day)", t.Quota.RequestsPerDay),
				RetryAfter: secondsUntilUTCDayEnd(now),
			}
		}
	}

	if t.Quota.MaxPending > 0 {
		n, err := s.db.countPendingByKey(ctx, t.KeyHash)
		if err != nil {
			return fmt.Errorf("quota check: %w", err)
		}
		if n >= t.Quota.MaxPending {
			return &quotaError{
				Message:    fmt.Sprintf("too many pending requests (%d)", t.Quota.MaxPending),
				RetryAfter: 60,
			}
		}
	}

	if s.cfg.MultiUser.Limits.GlobalRequestsPerDay > 0 {
		n, err := s.db.countRequestsSince(ctx, dayStart)
		if err != nil {
			return fmt.Errorf("quota check: %w", err)
		}
		if n >= s.cfg.MultiUser.Limits.GlobalRequestsPerDay {
			return &quotaError{
				Message:    "global daily request limit reached",
				RetryAfter: secondsUntilUTCDayEnd(now),
			}
		}
	}

	if ip != "" && s.cfg.MultiUser.Limits.PerIPRequestsPerHour > 0 {
		n, err := s.db.countRequestsByIPSince(ctx, ip, now.Add(-time.Hour))
		if err != nil {
			return fmt.Errorf("quota check: %w", err)
		}
		if n >= s.cfg.MultiUser.Limits.PerIPRequestsPerHour {
			return &quotaError{
				Message:    "per-IP hourly request limit reached",
				RetryAfter: 300,
			}
		}
	}

	if ip != "" && s.cfg.MultiUser.Limits.PerIPNewKeysPerDay > 0 && t.KeyHash != "" {
		seen, err := s.db.keySeenForIP(ctx, ip, t.KeyHash)
		if err != nil {
			return fmt.Errorf("quota check: %w", err)
		}
		if !seen {
			n, err := s.db.countDistinctKeysByIPSince(ctx, ip, dayStart)
			if err != nil {
				return fmt.Errorf("quota check: %w", err)
			}
			if n >= s.cfg.MultiUser.Limits.PerIPNewKeysPerDay {
				return &quotaError{
					Message:    "per-IP daily new-key limit reached",
					RetryAfter: secondsUntilUTCDayEnd(now),
				}
			}
		}
	}

	return nil
}

func writeAskError(w http.ResponseWriter, err error) {
	var qe *quotaError
	if errors.As(err, &qe) {
		if qe.RetryAfter > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(qe.RetryAfter))
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":   "quota_exceeded",
			"message": qe.Message,
		})
		return
	}
	http.Error(w, "internal error", http.StatusInternalServerError)
}

func (s *server) canAccessRequest(t tenant, owner sql.NullString) bool {
	if !s.cfg.MultiUser.Enabled {
		return true
	}
	if !owner.Valid {
		return false
	}
	return owner.String == t.KeyHash
}

type notifySpec struct {
	ServerChanSendKey string   `json:"serverchan_sendkey"`
	SendKey           string   `json:"sendkey"`
	AppriseURLs       []string `json:"apprise_urls"`
}

type notifyChannel struct {
	ServerChanSendKey string
	AppriseURLs       []string
}

func (s *server) resolveNotifyChannel(ar askRequest) (notifyChannel, error) {
	var ch notifyChannel
	userSupplied := false
	if ar.Notify != nil {
		userSupplied = true
		ch.ServerChanSendKey = strings.TrimSpace(ar.Notify.ServerChanSendKey)
		if ch.ServerChanSendKey == "" {
			ch.ServerChanSendKey = strings.TrimSpace(ar.Notify.SendKey)
		}
		seen := map[string]struct{}{}
		for _, raw := range ar.Notify.AppriseURLs {
			v := normalizeAppriseURL(raw)
			if v == "" {
				continue
			}
			if _, ok := seen[v]; ok {
				continue
			}
			seen[v] = struct{}{}
			ch.AppriseURLs = append(ch.AppriseURLs, v)
		}
	}

	if ch.ServerChanSendKey == "" && len(ch.AppriseURLs) == 0 {
		if s.cfg.MultiUser.Enabled {
			return ch, errors.New("notify is required in multi-user mode: pass notify.serverchan_sendkey or notify.apprise_urls")
		}
		userSupplied = false
		ch.ServerChanSendKey = strings.TrimSpace(s.cfg.ServerChanSendKey)
		for _, raw := range s.cfg.AppriseURLs {
			v := normalizeAppriseURL(raw)
			if v != "" {
				ch.AppriseURLs = append(ch.AppriseURLs, v)
			}
		}
	}

	if ch.ServerChanSendKey == "" && len(ch.AppriseURLs) == 0 {
		// Single-user behavior: create the request and let sendNotification
		// emit notify.failed.
		return ch, nil
	}
	if userSupplied {
		if err := s.validateAppriseURLs(ch.AppriseURLs); err != nil {
			return ch, err
		}
	}
	return ch, nil
}

func (s *server) validateAppriseURLs(urls []string) error {
	if len(urls) == 0 {
		return nil
	}
	if s.tenants != nil && s.tenants.allowAllSchemes {
		return nil
	}
	allowed := map[string]struct{}{}
	if s.tenants != nil {
		allowed = s.tenants.allowedSchemes
	}
	if len(allowed) == 0 {
		for _, scheme := range defaultAppriseSchemes {
			allowed[scheme] = struct{}{}
		}
	}
	for _, raw := range urls {
		u, err := url.Parse(raw)
		if err != nil || strings.TrimSpace(u.Scheme) == "" {
			return errors.New("invalid notify.apprise_urls entry")
		}
		scheme := strings.ToLower(u.Scheme)
		if _, ok := allowed[scheme]; !ok {
			return fmt.Errorf("apprise scheme %q is not allowed", scheme)
		}
	}
	return nil
}

func (s *server) redactNotifyText(text string, ch notifyChannel) string {
	if text == "" {
		return text
	}
	if ch.ServerChanSendKey != "" {
		text = strings.ReplaceAll(text, ch.ServerChanSendKey, "<redacted>")
	}
	for _, u := range ch.AppriseURLs {
		if u != "" {
			text = strings.ReplaceAll(text, u, "<redacted>")
		}
	}
	return text
}

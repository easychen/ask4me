package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testMultiUserConfig(t *testing.T, mutate func(*Config)) Config {
	t.Helper()
	cfg := Config{
		BaseURL:    "http://127.0.0.1:18080",
		SQLitePath: ":memory:",
		MultiUser: MultiUserConfig{
			Enabled: true,
			Auth: MultiUserAuthConfig{
				Mode:        multiUserModeOpen,
				InviteCodes: []string{"trial"},
			},
		},
	}
	if mutate != nil {
		mutate(&cfg)
	}
	if err := cfg.normalize(); err != nil {
		t.Fatalf("normalize config: %v", err)
	}
	return cfg
}

func newTestServer(t *testing.T, cfg Config) *server {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	st, err := newStore(db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	s := &server{cfg: cfg, db: st, hub: newRuntimeHub(time.Minute)}
	if err := s.initTenants(); err != nil {
		t.Fatalf("init tenants: %v", err)
	}
	return s
}

func createTestRequest(t *testing.T, s *server, id, keyHash, ip, status string) {
	t.Helper()
	err := s.db.createRequest(
		context.Background(), id, "title", "body", "", status,
		time.Now().Add(time.Hour),
		sql.NullString{}, sql.NullString{}, sql.NullString{}, sql.NullString{}, sql.NullString{},
		keyHash, ip,
	)
	if err != nil {
		t.Fatalf("create request %s: %v", id, err)
	}
}

func TestMultiUserConfigDefaults(t *testing.T) {
	cfg := testMultiUserConfig(t, nil)
	if cfg.MultiUser.Auth.Mode != multiUserModeOpen {
		t.Fatalf("mode = %q", cfg.MultiUser.Auth.Mode)
	}
	if cfg.MultiUser.Defaults.RequestsPerDay != defaultMultiUserRequestsPerDay {
		t.Fatalf("requests_per_day = %d", cfg.MultiUser.Defaults.RequestsPerDay)
	}
	if len(cfg.MultiUser.AllowedAppriseSchemes) == 0 {
		t.Fatal("expected default apprise schemes")
	}
}

func TestMultiUserConfigRequiresInvitesForOpen(t *testing.T) {
	cfg := Config{
		BaseURL: "http://127.0.0.1",
		MultiUser: MultiUserConfig{
			Enabled: true,
			Auth:    MultiUserAuthConfig{Mode: multiUserModeOpen},
		},
	}
	if err := cfg.normalize(); err == nil {
		t.Fatal("expected error for open mode without invite codes")
	}
}

func TestMultiUserConfigRequiresKeysForAllowlist(t *testing.T) {
	cfg := Config{
		BaseURL: "http://127.0.0.1",
		APIKey:  "legacy-key-123456",
		MultiUser: MultiUserConfig{
			Enabled: true,
			Auth:    MultiUserAuthConfig{Mode: multiUserModeAllowlist},
		},
	}
	if err := cfg.normalize(); err == nil {
		t.Fatal("expected error for allowlist mode without explicit keys (api_key is ignored)")
	}
}

func TestMultiUserInviteCodeValidation(t *testing.T) {
	cfg := Config{
		BaseURL: "http://127.0.0.1",
		MultiUser: MultiUserConfig{
			Enabled: true,
			Auth: MultiUserAuthConfig{
				Mode:        multiUserModeOpen,
				InviteCodes: []string{"bad_code"},
			},
		},
	}
	if err := cfg.normalize(); err == nil {
		t.Fatal("expected error for invite code containing underscore")
	}
}

func TestInviteFromKey(t *testing.T) {
	cases := []struct {
		key    string
		invite string
		ok     bool
	}{
		{"a4m_trial_abcdefghijklmnop", "trial", true},
		{"a4m_trial_abc", "", false},
		{"a4m_trial_bad!random123456", "", false},
		{"wrong_trial_abcdefghijklmnop", "", false},
		{"a4m_trial_abcdefghijklmnop_qrst", "trial", true},
	}
	for _, tc := range cases {
		invite, ok := inviteFromKey(tc.key)
		if ok != tc.ok || invite != tc.invite {
			t.Fatalf("inviteFromKey(%q) = (%q,%v), want (%q,%v)", tc.key, invite, ok, tc.invite, tc.ok)
		}
	}
}

func TestResolveTenant(t *testing.T) {
	cfg := testMultiUserConfig(t, nil)
	s := newTestServer(t, cfg)

	tn, ok := s.resolveTenant("a4m_trial_abcdefghijklmnop")
	if !ok || tn.KeyHash == "" {
		t.Fatal("expected invite key to resolve")
	}
	if tn.Quota.RequestsPerDay != cfg.MultiUser.Defaults.RequestsPerDay {
		t.Fatalf("quota = %d", tn.Quota.RequestsPerDay)
	}

	if _, ok := s.resolveTenant("a4m_unknown_abcdefghijklmnop"); ok {
		t.Fatal("expected unknown invite to be rejected")
	}
	if _, ok := s.resolveTenant("a4m_trial_short"); ok {
		t.Fatal("expected short key to be rejected")
	}
}

func TestResolveTenantAllowlistAndIgnoredAPIKey(t *testing.T) {
	cfg := testMultiUserConfig(t, func(c *Config) {
		c.APIKey = "leg"
		c.MultiUser.Auth.Mode = multiUserModeAllowlist
		c.MultiUser.Auth.InviteCodes = nil
		c.MultiUser.Auth.Keys = []MultiUserKeyConfig{{Key: "allowlisted-key-123456", Name: "alice"}}
	})
	s := newTestServer(t, cfg)

	if tn, ok := s.resolveTenant("allowlisted-key-123456"); !ok || tn.Name != "alice" {
		t.Fatalf("allowlisted tenant = %+v, ok=%v", tn, ok)
	}
	if _, ok := s.resolveTenant("leg"); ok {
		t.Fatal("api_key must be ignored in multi-user mode")
	}
	if _, ok := s.resolveTenant("unknown-key-123456"); ok {
		t.Fatal("expected unknown key to be rejected")
	}
}

func TestQuotaPerKeyDaily(t *testing.T) {
	cfg := testMultiUserConfig(t, func(c *Config) {
		c.MultiUser.Defaults.RequestsPerDay = 2
		c.MultiUser.Limits.PerIPRequestsPerHour = 1000
		c.MultiUser.Limits.PerIPNewKeysPerDay = 1000
		c.MultiUser.Limits.GlobalRequestsPerDay = 1000
	})
	s := newTestServer(t, cfg)
	tn, ok := s.resolveTenant("a4m_trial_abcdefghijklmnop")
	if !ok {
		t.Fatal("resolve tenant")
	}

	createTestRequest(t, s, "req_test1", tn.KeyHash, "10.0.0.1", "created")
	createTestRequest(t, s, "req_test2", tn.KeyHash, "10.0.0.1", "created")

	err := s.checkCreateQuota(context.Background(), tn, "10.0.0.1")
	var qe *quotaError
	if !errors.As(err, &qe) {
		t.Fatalf("expected quota error, got %v", err)
	}
	if !strings.Contains(qe.Message, "daily") {
		t.Fatalf("unexpected message: %s", qe.Message)
	}
}

func TestQuotaMaxPending(t *testing.T) {
	cfg := testMultiUserConfig(t, func(c *Config) {
		c.MultiUser.Defaults.RequestsPerDay = 100
		c.MultiUser.Defaults.MaxPending = 1
		c.MultiUser.Limits.PerIPRequestsPerHour = 1000
		c.MultiUser.Limits.PerIPNewKeysPerDay = 1000
		c.MultiUser.Limits.GlobalRequestsPerDay = 1000
	})
	s := newTestServer(t, cfg)
	tn, ok := s.resolveTenant("a4m_trial_abcdefghijklmnop")
	if !ok {
		t.Fatal("resolve tenant")
	}
	createTestRequest(t, s, "req_test1", tn.KeyHash, "10.0.0.1", "created")
	if err := s.checkCreateQuota(context.Background(), tn, "10.0.0.1"); err == nil {
		t.Fatal("expected pending quota error")
	}
}

func TestQuotaPerIPNewKeys(t *testing.T) {
	cfg := testMultiUserConfig(t, func(c *Config) {
		c.MultiUser.Defaults.RequestsPerDay = 100
		c.MultiUser.Defaults.MaxPending = 100
		c.MultiUser.Limits.PerIPRequestsPerHour = 1000
		c.MultiUser.Limits.PerIPNewKeysPerDay = 1
		c.MultiUser.Limits.GlobalRequestsPerDay = 1000
	})
	s := newTestServer(t, cfg)
	first, ok := s.resolveTenant("a4m_trial_aaaaaaaaaaaaaaaa")
	if !ok {
		t.Fatal("resolve first tenant")
	}
	second, ok := s.resolveTenant("a4m_trial_bbbbbbbbbbbbbbbb")
	if !ok {
		t.Fatal("resolve second tenant")
	}
	createTestRequest(t, s, "req_test1", first.KeyHash, "10.0.0.1", "created")
	if err := s.checkCreateQuota(context.Background(), second, "10.0.0.1"); err == nil {
		t.Fatal("expected per-IP new key quota error")
	}
	// An already-seen key from the same IP is not a new key.
	if err := s.checkCreateQuota(context.Background(), first, "10.0.0.1"); err != nil {
		t.Fatalf("existing key should pass: %v", err)
	}
}

func TestCanAccessRequest(t *testing.T) {
	cfg := testMultiUserConfig(t, nil)
	s := newTestServer(t, cfg)
	tn, ok := s.resolveTenant("a4m_trial_abcdefghijklmnop")
	if !ok {
		t.Fatal("resolve tenant")
	}
	if !s.canAccessRequest(tn, sql.NullString{String: tn.KeyHash, Valid: true}) {
		t.Fatal("owner should have access")
	}
	if s.canAccessRequest(tn, sql.NullString{String: "other", Valid: true}) {
		t.Fatal("other tenant must not have access")
	}
	if s.canAccessRequest(tn, sql.NullString{}) {
		t.Fatal("ownerless rows must not be accessible in multi-user mode")
	}
}

func TestGetRequestStatusOwner(t *testing.T) {
	s := newTestServer(t, testMultiUserConfig(t, nil))
	createTestRequest(t, s, "req_owner", "owner-hash", "10.0.0.1", "created")
	status, _, owner, err := s.db.getRequestStatusOwner(context.Background(), "req_owner")
	if err != nil {
		t.Fatalf("get owner: %v", err)
	}
	if status != "created" || !owner.Valid || owner.String != "owner-hash" {
		t.Fatalf("status=%q owner=%+v", status, owner)
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("POST", "/v1/ask", nil)
	r.RemoteAddr = "10.1.2.3:4567"
	if got := clientIP(r, false); got != "10.1.2.3" {
		t.Fatalf("clientIP = %q", got)
	}
	r.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")
	if got := clientIP(r, false); got != "10.1.2.3" {
		t.Fatalf("clientIP without trust = %q", got)
	}
	if got := clientIP(r, true); got != "203.0.113.9" {
		t.Fatalf("clientIP with trust = %q", got)
	}
}

func TestResolveNotifyChannel(t *testing.T) {
	cfg := testMultiUserConfig(t, nil)
	s := newTestServer(t, cfg)

	ar := askRequest{Notify: &notifySpec{AppriseURLs: []string{"file:///tmp/x"}}}
	if _, err := s.resolveNotifyChannel(ar); err == nil {
		t.Fatal("expected disallowed apprise scheme")
	}

	ar = askRequest{Notify: &notifySpec{AppriseURLs: []string{"schan://SCT123"}}}
	ch, err := s.resolveNotifyChannel(ar)
	if err != nil {
		t.Fatalf("resolve schan: %v", err)
	}
	if len(ch.AppriseURLs) != 1 || ch.AppriseURLs[0] != "schan://SCT123" {
		t.Fatalf("channel = %+v", ch)
	}

	ar = askRequest{Notify: &notifySpec{ServerChanSendKey: "SCT456"}}
	ch, err = s.resolveNotifyChannel(ar)
	if err != nil || ch.ServerChanSendKey != "SCT456" {
		t.Fatalf("serverchan channel = %+v, err=%v", ch, err)
	}

	emptyCfg := testMultiUserConfig(t, nil)
	emptySrv := newTestServer(t, emptyCfg)
	if _, err := emptySrv.resolveNotifyChannel(askRequest{}); err == nil {
		t.Fatal("expected error when no channel is available")
	}
}

func TestGlobalNotifyChannelIgnoredInMultiUser(t *testing.T) {
	cfg := testMultiUserConfig(t, func(c *Config) {
		c.APIKey = "legacy-key-123456"
		c.ServerChanSendKey = "GLOBAL-SCT"
	})
	s := newTestServer(t, cfg)

	if _, ok := s.resolveTenant("legacy-key-123456"); ok {
		t.Fatal("api_key must be ignored in multi-user mode")
	}
	if _, err := s.resolveNotifyChannel(askRequest{}); err == nil {
		t.Fatal("multi-user requests must carry notify, even when a global channel exists")
	}
}

func TestGlobalNotifyChannelForSingleUser(t *testing.T) {
	cfg := Config{
		BaseURL:           "http://127.0.0.1",
		APIKey:            "single-key",
		SQLitePath:        ":memory:",
		ServerChanSendKey: "GLOBAL-SCT",
	}
	if err := cfg.normalize(); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	s := newTestServer(t, cfg)
	ch, err := s.resolveNotifyChannel(askRequest{})
	if err != nil {
		t.Fatalf("single-user global channel: %v", err)
	}
	if ch.ServerChanSendKey != "GLOBAL-SCT" {
		t.Fatalf("channel = %+v", ch)
	}
}

func TestNoChannelSingleUserKeepsNotifyFailedBehavior(t *testing.T) {
	cfg := Config{
		BaseURL:    "http://127.0.0.1",
		APIKey:     "single-key",
		SQLitePath: ":memory:",
	}
	if err := cfg.normalize(); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	s := newTestServer(t, cfg)
	ch, err := s.resolveNotifyChannel(askRequest{})
	if err != nil {
		t.Fatalf("single-user without channel should not fail preflight: %v", err)
	}
	if ch.ServerChanSendKey != "" || len(ch.AppriseURLs) != 0 {
		t.Fatalf("expected empty channel, got %+v", ch)
	}
}

func TestRedactNotifyText(t *testing.T) {
	s := newTestServer(t, testMultiUserConfig(t, nil))
	ch := notifyChannel{ServerChanSendKey: "SCT-secret", AppriseURLs: []string{"schan://secret"}}
	got := s.redactNotifyText("error SCT-secret and schan://secret", ch)
	if strings.Contains(got, "SCT-secret") || strings.Contains(got, "schan://secret") {
		t.Fatalf("text was not redacted: %s", got)
	}
}

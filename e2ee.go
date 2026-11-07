package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const (
	e2eeTokenPrefix       = "enc:v1:"
	e2eeAAD               = "ask4me-e2ee-v1"
	defaultE2EEIterations = 310000
	minE2EEIterations     = 100000
	maxE2EEIterations     = 1000000
	e2eeSaltBytes         = 16
	e2eeNotificationBody  = "收到一条加密消息，请打开链接并输入口令查看。"
	e2eeNotificationTitle = "Ask4Me 加密消息"
)

// E2EEConfig controls the optional end-to-end encryption feature. The server
// never sees the password or plaintext: clients encrypt before POSTing, and
// the interaction page decrypts in the browser with WebCrypto.
type E2EEConfig struct {
	Enabled               bool  `yaml:"enabled"`
	KDFIterations         int   `yaml:"kdf_iterations"`
	EncryptAnswersDefault *bool `yaml:"encrypt_answers_default"`
}

func (c *E2EEConfig) normalize(baseURL string) error {
	if !c.Enabled {
		return nil
	}
	if c.KDFIterations <= 0 {
		c.KDFIterations = defaultE2EEIterations
	}
	if c.KDFIterations < minE2EEIterations || c.KDFIterations > maxE2EEIterations {
		return fmt.Errorf("e2ee.kdf_iterations must be between %d and %d", minE2EEIterations, maxE2EEIterations)
	}
	if !isSecureE2EEBaseURL(baseURL) {
		return errors.New("e2ee requires an https base_url (localhost is allowed for development)")
	}
	if c.EncryptAnswersDefault == nil {
		v := true
		c.EncryptAnswersDefault = &v
	}
	return nil
}

func (c E2EEConfig) encryptAnswersDefault() bool {
	if c.EncryptAnswersDefault == nil {
		return true
	}
	return *c.EncryptAnswersDefault
}

func isSecureE2EEBaseURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return true
	case "http":
		host := strings.ToLower(u.Hostname())
		return host == "localhost" || host == "127.0.0.1" || host == "::1"
	default:
		return false
	}
}

// e2eeSpec is the per-request E2EE metadata sent by the client. The password
// is never sent; only the salt and KDF parameters needed by the page/SDK.
type e2eeSpec struct {
	Enabled    bool   `json:"enabled"`
	Salt       string `json:"salt"`
	Iterations int    `json:"iterations"`
	Answers    *bool  `json:"answers"`
}

func (s *server) e2eeActive(ar askRequest) bool {
	return s.cfg.E2EE.Enabled && ar.E2EE != nil && ar.E2EE.Enabled
}

func (s *server) e2eeAnswers(ar askRequest) bool {
	if ar.E2EE == nil || ar.E2EE.Answers == nil {
		return s.cfg.E2EE.encryptAnswersDefault()
	}
	return *ar.E2EE.Answers
}

func (s *server) normalizeE2EE(ar *askRequest) error {
	requested := ar.E2EE != nil && ar.E2EE.Enabled
	if !s.cfg.E2EE.Enabled {
		if requested {
			return errors.New("e2ee is disabled on this server")
		}
		return nil
	}
	if !requested {
		return nil
	}
	if strings.TrimSpace(ar.E2EE.Salt) == "" {
		return errors.New("e2ee.salt is required")
	}
	salt, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(ar.E2EE.Salt))
	if err != nil || len(salt) != e2eeSaltBytes {
		return errors.New("e2ee.salt must be 16 bytes encoded as base64url without padding")
	}
	if ar.E2EE.Iterations <= 0 {
		ar.E2EE.Iterations = s.cfg.E2EE.KDFIterations
	}
	if ar.E2EE.Iterations < minE2EEIterations || ar.E2EE.Iterations > maxE2EEIterations {
		return fmt.Errorf("e2ee.iterations must be between %d and %d", minE2EEIterations, maxE2EEIterations)
	}
	if ar.E2EE.Answers == nil {
		v := s.cfg.E2EE.encryptAnswersDefault()
		ar.E2EE.Answers = &v
	}
	if body := strings.TrimSpace(ar.Body); body != "" && !isE2EEToken(body) {
		return errors.New("e2ee.body must be an enc:v1 token")
	}
	return nil
}

func isE2EEToken(v string) bool {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, e2eeTokenPrefix) {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(v, e2eeTokenPrefix))
	if err != nil {
		return false
	}
	// AES-GCM: 12-byte nonce + at least a 16-byte tag.
	return len(raw) >= 12+16
}

func (s *server) notificationTitle(ar askRequest) string {
	title := strings.TrimSpace(ar.Title)
	if title == "" {
		title = "Ask4Me"
	}
	if s.e2eeActive(ar) && isE2EEToken(title) {
		return e2eeNotificationTitle
	}
	return title
}

func (s *server) notificationBody(ar askRequest) string {
	if s.e2eeActive(ar) {
		return e2eeNotificationBody
	}
	body := strings.TrimSpace(ar.Body)
	if body == "" {
		return "Please respond."
	}
	return body
}

package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"proxy2api/internal/config"
	"proxy2api/internal/store"
)

func TestProxyAndStats(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok","usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`))
	}))
	defer up.Close()

	s, ts := newTestGateway(t, testCfg(up.URL, nil))
	defer ts.Close()
	defer s.store.Close()

	reqBody := []byte(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}]}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/chat/completions", bytes.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer sk-test")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("proxy request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	adminReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/admin/stats", nil)
	adminReq.Header.Set("X-Admin-Key", "admin-test")
	adminResp, err := http.DefaultClient.Do(adminReq)
	if err != nil {
		t.Fatalf("stats request failed: %v", err)
	}
	defer adminResp.Body.Close()
	if adminResp.StatusCode != http.StatusOK {
		t.Fatalf("expected stats 200, got %d", adminResp.StatusCode)
	}

	var stats map[string]any
	if err := json.NewDecoder(adminResp.Body).Decode(&stats); err != nil {
		t.Fatalf("decode stats failed: %v", err)
	}
	if _, ok := stats["usage_last_24h"]; !ok {
		t.Fatalf("expected usage_last_24h in admin stats")
	}
}

func TestRuleDeny(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer up.Close()

	rules := []config.RoutingRule{
		{
			Name: "deny-ban",
			Match: config.RuleMatch{
				ModelPrefix: "ban-",
			},
			Action: config.RuleAction{Deny: true},
		},
	}
	s, ts := newTestGateway(t, testCfg(up.URL, rules))
	defer ts.Close()
	defer s.store.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/chat/completions", bytes.NewReader([]byte(`{"model":"ban-model"}`)))
	req.Header.Set("Authorization", "Bearer sk-test")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("deny request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestProviderKeyCooldownRecovery(t *testing.T) {
	var n atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"upstream fail"}`))
			return
		}
		_, _ = w.Write([]byte(`{"usage":{"total_tokens":8}}`))
	}))
	defer up.Close()

	cfg := testCfg(up.URL, nil)
	cfg.Gateway.ProviderFailThreshold = 1
	cfg.Gateway.ProviderCooldownSec = 1
	cfg.Gateway.ReloadSeconds = 1
	cfg.Providers[0].RecoverIntervalSec = 1

	s, ts := newTestGateway(t, cfg)
	defer ts.Close()
	defer s.store.Close()

	doReq := func() int {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/chat/completions", bytes.NewReader([]byte(`{"model":"gpt-4o-mini"}`)))
		req.Header.Set("Authorization", "Bearer sk-test")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	if code := doReq(); code != http.StatusBadGateway {
		t.Fatalf("expected first request 502, got %d", code)
	}
	if code := doReq(); code != http.StatusServiceUnavailable {
		t.Fatalf("expected second request 503 during cooldown, got %d", code)
	}
	time.Sleep(1200 * time.Millisecond)
	if code := doReq(); code != http.StatusOK {
		t.Fatalf("expected third request 200 after cooldown, got %d", code)
	}
}

func TestAdminRuleCRUD(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"usage":{"total_tokens":8}}`))
	}))
	defer up.Close()

	s, ts := newTestGateway(t, testCfg(up.URL, nil))
	defer ts.Close()
	defer s.store.Close()

	createBody := []byte(`{
		"name":"deny-all-temp",
		"priority":1,
		"enabled":true,
		"condition_js":"ctx.model.startsWith('gpt-')",
		"action":{"deny":true}
	}`)
	reqCreate, _ := http.NewRequest(http.MethodPost, ts.URL+"/admin/rules", bytes.NewReader(createBody))
	reqCreate.Header.Set("X-Admin-Key", "admin-test")
	reqCreate.Header.Set("Content-Type", "application/json")
	respCreate, err := http.DefaultClient.Do(reqCreate)
	if err != nil {
		t.Fatalf("create rule failed: %v", err)
	}
	defer respCreate.Body.Close()
	if respCreate.StatusCode != http.StatusOK {
		t.Fatalf("expected create rule 200, got %d", respCreate.StatusCode)
	}
	var createResult map[string]any
	_ = json.NewDecoder(respCreate.Body).Decode(&createResult)
	if createResult["id"] == nil {
		t.Fatalf("create rule did not return id")
	}

	reqProxy, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/chat/completions", bytes.NewReader([]byte(`{"model":"gpt-4o-mini"}`)))
	reqProxy.Header.Set("Authorization", "Bearer sk-test")
	respProxy, err := http.DefaultClient.Do(reqProxy)
	if err != nil {
		t.Fatalf("proxy after create rule failed: %v", err)
	}
	defer respProxy.Body.Close()
	if respProxy.StatusCode != http.StatusForbidden {
		t.Fatalf("expected forbidden after deny rule, got %d", respProxy.StatusCode)
	}

	idVal := int64(createResult["id"].(float64))
	reqDel, _ := http.NewRequest(http.MethodDelete, ts.URL+"/admin/rules?id="+strconv.FormatInt(idVal, 10), nil)
	reqDel.Header.Set("X-Admin-Key", "admin-test")
	respDel, err := http.DefaultClient.Do(reqDel)
	if err != nil {
		t.Fatalf("delete rule failed: %v", err)
	}
	defer respDel.Body.Close()
	if respDel.StatusCode != http.StatusOK {
		t.Fatalf("expected delete rule 200, got %d", respDel.StatusCode)
	}

	reqProxy2, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/chat/completions", bytes.NewReader([]byte(`{"model":"gpt-4o-mini"}`)))
	reqProxy2.Header.Set("Authorization", "Bearer sk-test")
	respProxy2, err := http.DefaultClient.Do(reqProxy2)
	if err != nil {
		t.Fatalf("proxy after delete rule failed: %v", err)
	}
	defer respProxy2.Body.Close()
	if respProxy2.StatusCode != http.StatusOK {
		t.Fatalf("expected success after deleting rule, got %d", respProxy2.StatusCode)
	}
}

func newTestGateway(t *testing.T, cfg *config.Config) (*Server, *httptest.Server) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "proxy2api-test.db")
	cfg.DB.Path = dbPath

	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store failed: %v", err)
	}
	if err := st.SeedFromConfig(cfg); err != nil {
		t.Fatalf("seed store failed: %v", err)
	}
	s, err := NewServer(cfg, st)
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	ts := httptest.NewServer(s.Handler())
	return s, ts
}

func testCfg(upstream string, rules []config.RoutingRule) *config.Config {
	return &config.Config{
		Server: config.ServerConfig{Listen: ":0"},
		Auth:   config.AuthConfig{AdminKey: "admin-test"},
		DB:     config.DBConfig{Path: "unused"},
		Gateway: config.GatewayConfig{
			TimeoutSeconds:        3,
			StickySessionHeader:   "session_id",
			ProviderFailThreshold: 2,
			ProviderCooldownSec:   1,
			Timezone:              "UTC",
			ReloadSeconds:         1,
		},
		Providers: []config.Provider{
			{
				Name:               "p1",
				BaseURL:            upstream,
				UpstreamKeys:       []string{"up-key-1"},
				Weight:             1,
				Models:             []string{"gpt-4o-mini"},
				ModelMap:           map[string]string{},
				MaxRPM:             1000,
				MaxTPM:             100000,
				Enabled:            true,
				GroupName:          "default",
				RecoverIntervalSec: 1,
			},
		},
		Keys: []config.APIKeyConfig{
			{
				Key:            "sk-test",
				Name:           "test",
				MaxRPM:         1000,
				MaxTPM:         100000,
				AllowedModels:  []string{"gpt-4o-mini", "ban-model"},
				RateMultiplier: 1,
			},
		},
		Rules: rules,
	}
}

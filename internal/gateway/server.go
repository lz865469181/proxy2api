package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"proxy2api/internal/config"
	"proxy2api/internal/store"
)

type Server struct {
	cfg   *config.Config
	store *store.Store

	httpServer *http.Server
	client     *http.Client
	loc        *time.Location

	snapshot atomicSnapshot

	userLimiter    Limiter
	channelLimiter Limiter

	stickyMu      sync.Mutex
	stickySession map[string]stickyBind

	statsMu sync.Mutex
	stats   map[string]int64
}

func NewServer(cfg *config.Config, st *store.Store) (*Server, error) {
	loc, err := time.LoadLocation(cfg.Gateway.Timezone)
	if err != nil {
		loc = time.UTC
	}

	s := &Server{
		cfg:            cfg,
		store:          st,
		client:         &http.Client{Timeout: time.Duration(cfg.Gateway.TimeoutSeconds) * time.Second},
		loc:            loc,
		userLimiter:    newMinuteLimiter(),
		channelLimiter: newMinuteLimiter(),
		stickySession:  make(map[string]stickyBind),
		stats:          make(map[string]int64),
	}
	if cfg.Redis.Enabled && cfg.Redis.Addr != "" {
		if rl, err := newRedisLimiter(cfg.Redis); err == nil {
			s.userLimiter = rl
			s.channelLimiter = rl
			log.Printf("redis limiter enabled addr=%s", cfg.Redis.Addr)
		} else {
			log.Printf("redis limiter init failed, fallback to memory: %v", err)
			s.userLimiter = newMinuteLimiter()
			s.channelLimiter = newMinuteLimiter()
		}
	} else {
		s.userLimiter = newMinuteLimiter()
		s.channelLimiter = newMinuteLimiter()
	}

	if err := s.reloadSnapshot(); err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/v1/", s.handleProxy)
	mux.HandleFunc("/admin/providers", s.handleAdminProviders)
	mux.HandleFunc("/admin/providers/", s.handleAdminProviderAction)
	mux.HandleFunc("/admin/provider-keys", s.handleAdminProviderKeys)
	mux.HandleFunc("/admin/api-keys", s.handleAdminAPIKeys)
	mux.HandleFunc("/admin/rules", s.handleAdminRules)
	mux.HandleFunc("/admin/schedules", s.handleAdminSchedules)
	mux.HandleFunc("/admin/config/export", s.handleAdminConfigExport)
	mux.HandleFunc("/admin/config/import", s.handleAdminConfigImport)
	mux.HandleFunc("/admin/stats", s.handleAdminStats)
	mux.HandleFunc("/admin/reload", s.handleAdminReload)
	mux.HandleFunc("/admin/ui", s.handleAdminUI)

	s.httpServer = &http.Server{
		Addr:    cfg.Server.Listen,
		Handler: withRecover(withMetrics(withAccessLog(mux))),
	}
	return s, nil
}

func (s *Server) Start() error {
	go s.loopReload()
	return s.httpServer.ListenAndServe()
}

func (s *Server) Handler() http.Handler {
	return s.httpServer.Handler
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) loopReload() {
	ticker := time.NewTicker(time.Duration(s.cfg.Gateway.ReloadSeconds) * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if err := s.reloadSnapshot(); err != nil {
			log.Printf("reload snapshot failed: %v", err)
		}
	}
}

func (s *Server) reloadSnapshot() error {
	providers, err := s.store.ListProviders()
	if err != nil {
		return err
	}
	providerKeys, err := s.store.ListProviderKeys("")
	if err != nil {
		return err
	}
	apiKeys, err := s.store.ListAPIKeys()
	if err != nil {
		return err
	}
	rules, err := s.store.ListRules()
	if err != nil {
		return err
	}
	schedules, err := s.store.ListSchedules()
	if err != nil {
		return err
	}

	byProviderKey := map[string][]store.ProviderKey{}
	for _, pk := range providerKeys {
		byProviderKey[pk.ProviderName] = append(byProviderKey[pk.ProviderName], pk)
	}

	snap := runtimeSnapshot{
		providers: map[string]providerState{},
		keys:      map[string]gatewayKey{},
		rules:     []ruleState{},
		schedules: []groupSchedule{},
	}

	for _, p := range providers {
		var models []string
		var modelMap map[string]string
		_ = json.Unmarshal([]byte(p.ModelsJSON), &models)
		_ = json.Unmarshal([]byte(p.ModelMapJSON), &modelMap)
		if modelMap == nil {
			modelMap = map[string]string{}
		}

		keys := make([]providerKeyState, 0, len(byProviderKey[p.Name]))
		for _, pk := range byProviderKey[p.Name] {
			keys = append(keys, providerKeyState{
				ID:              pk.ID,
				Alias:           pk.Alias,
				APIKey:          pk.APIKey,
				Enabled:         pk.Enabled,
				ConsecutiveErrs: pk.ConsecutiveErrs,
				CooldownUntil:   time.Unix(pk.CooldownUntil, 0),
				LastStatus:      pk.LastStatus,
			})
		}

		snap.providers[p.Name] = providerState{
			Name:            p.Name,
			BaseURL:         p.BaseURL,
			Weight:          p.Weight,
			Models:          models,
			ModelMap:        modelMap,
			MaxRPM:          p.MaxRPM,
			MaxTPM:          p.MaxTPM,
			Enabled:         p.Enabled,
			GroupName:       p.GroupName,
			RecoverInterval: time.Duration(max(1, p.RecoverIntervalSec)) * time.Second,
			Keys:            keys,
		}
	}

	for _, k := range apiKeys {
		var allow []string
		_ = json.Unmarshal([]byte(k.AllowedModels), &allow)
		snap.keys[k.KeyValue] = gatewayKey{
			Key:            k.KeyValue,
			Name:           k.Name,
			MaxRPM:         k.MaxRPM,
			MaxTPM:         k.MaxTPM,
			AllowedModels:  allow,
			RateMultiplier: k.RateMultiplier,
			Enabled:        k.Enabled,
		}
	}

	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		var action ruleAction
		if err := json.Unmarshal([]byte(r.ActionJSON), &action); err != nil {
			log.Printf("skip invalid rule action id=%d: %v", r.ID, err)
			continue
		}
		snap.rules = append(snap.rules, ruleState{
			ID:        r.ID,
			Name:      r.Name,
			Priority:  r.Priority,
			Enabled:   r.Enabled,
			Condition: r.ConditionJS,
			Action:    action,
		})
	}

	for _, sc := range schedules {
		snap.schedules = append(snap.schedules, groupSchedule{
			GroupName:   sc.GroupName,
			WeekdayMask: parseWeekdayMask(sc.WeekdayMask),
			StartMinute: parseHHMM(sc.StartHHMM),
			EndMinute:   parseHHMM(sc.EndHHMM),
			Multiplier:  sc.Multiplier,
			Enabled:     sc.Enabled,
		})
	}

	s.snapshot.Set(snap)
	return nil
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "time": time.Now().UTC()})
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	_, _, err := s.authenticateUser(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}

	snap := s.snapshot.Get()
	modelSet := make(map[string]struct{})
	for _, p := range snap.providers {
		if !p.Enabled {
			continue
		}
		for _, m := range p.Models {
			modelSet[m] = struct{}{}
		}
	}

	models := make([]map[string]any, 0, len(modelSet))
	for model := range modelSet {
		models = append(models, map[string]any{
			"id":       model,
			"object":   "model",
			"created":  time.Now().Unix(),
			"owned_by": "proxy2api",
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": models})
}

func (s *Server) handleProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "only POST is allowed on proxy endpoints"})
		return
	}

	userToken, keyCfg, err := s.authenticateUser(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}

	body, reqInfo, err := readAndParseRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if len(keyCfg.AllowedModels) > 0 && !supportsModel(keyCfg.AllowedModels, reqInfo.Model) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "model not allowed for this api key"})
		return
	}

	effectiveMultiplier := keyCfg.RateMultiplier
	forcedProvider, denied := s.applyRules(userToken, reqInfo.Model, r.URL.Path, &effectiveMultiplier)
	if denied {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "request denied by routing rule"})
		return
	}

	estimatedToken := int(float64(reqInfo.EstimatedToken) * effectiveMultiplier)
	if estimatedToken <= 0 {
		estimatedToken = 1
	}

	if !s.userLimiter.Allow("user:"+keyCfg.Key, 1, estimatedToken, keyCfg.MaxRPM, keyCfg.MaxTPM) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "user rate limit exceeded"})
		return
	}

	provider, providerKey, err := s.selectProviderAndKey(r, reqInfo.Model, forcedProvider)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}

	channelLimitMul := s.scheduleMultiplier(provider.GroupName)
	effectiveProviderMaxRPM := int(float64(provider.MaxRPM) * channelLimitMul)
	effectiveProviderMaxTPM := int(float64(provider.MaxTPM) * channelLimitMul)
	if effectiveProviderMaxRPM < 0 {
		effectiveProviderMaxRPM = 0
	}
	if effectiveProviderMaxTPM < 0 {
		effectiveProviderMaxTPM = 0
	}

	channelKey := "provider:" + provider.Name + ":" + providerKey.Alias
	if !s.channelLimiter.Allow(channelKey, 1, estimatedToken, effectiveProviderMaxRPM, effectiveProviderMaxTPM) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "provider rate limit exceeded"})
		return
	}

	respUsage, err := s.forward(w, r, body, reqInfo.Model, provider, providerKey)
	if err != nil {
		log.Printf("proxy error provider=%s key=%s path=%s err=%v", provider.Name, providerKey.Alias, r.URL.Path, err)
		s.markProviderKeyFailure(provider, providerKey, err.Error())
		s.incStat("proxy_error")
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	s.markProviderKeySuccess(providerKey)
	_ = s.store.AddUsage(store.UsageRecord{
		AtUnix:           time.Now().Unix(),
		APIKey:           userToken,
		Provider:         provider.Name,
		ProviderKey:      providerKey.Alias,
		Model:            reqInfo.Model,
		PromptTokens:     respUsage.Prompt,
		CompletionTokens: respUsage.Completion,
		TotalTokens:      max(estimatedToken, respUsage.Total),
	})
	s.incStat("proxy_ok")
}

type usage struct {
	Prompt     int
	Completion int
	Total      int
}

func (s *Server) forward(w http.ResponseWriter, r *http.Request, body []byte, model string, provider providerState, providerKey providerKeyState) (usage, error) {
	base, err := url.Parse(provider.BaseURL)
	if err != nil {
		return usage{}, fmt.Errorf("invalid provider base_url: %w", err)
	}
	target := base.JoinPath(r.URL.Path)
	target.RawQuery = r.URL.RawQuery

	outBody := maybeRewriteModel(body, model, provider.ModelMap)
	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), bytes.NewReader(outBody))
	if err != nil {
		return usage{}, err
	}
	copyHeaders(r.Header, outReq.Header)
	outReq.Header.Set("Authorization", "Bearer "+providerKey.APIKey)
	outReq.Header.Set("X-Provider-Key-Alias", providerKey.Alias)

	resp, err := s.client.Do(outReq)
	if err != nil {
		return usage{}, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return usage{}, err
	}

	if resp.StatusCode >= 500 {
		return usage{}, fmt.Errorf("upstream status %d", resp.StatusCode)
	}

	for k, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(respBody)

	u := extractUsage(respBody)
	return u, nil
}

func (s *Server) authenticateUser(r *http.Request) (string, gatewayKey, error) {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if auth == "" {
		return "", gatewayKey{}, errors.New("missing authorization header")
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", gatewayKey{}, errors.New("invalid authorization header")
	}
	key := strings.TrimSpace(parts[1])
	snap := s.snapshot.Get()
	k, ok := snap.keys[key]
	if !ok || !k.Enabled {
		return "", gatewayKey{}, errors.New("invalid api key")
	}
	return key, k, nil
}

func (s *Server) applyRules(apiKey, model, path string, multiplier *float64) (forceProvider string, denied bool) {
	snap := s.snapshot.Get()
	for _, rule := range snap.rules {
		if !rule.Enabled {
			continue
		}
		matched, err := evalRule(rule.Condition, map[string]any{
			"api_key": apiKey,
			"model":   model,
			"path":    path,
		})
		if err != nil || !matched {
			continue
		}
		if rule.Action.Deny {
			return "", true
		}
		if rule.Action.ForceProvider != "" {
			forceProvider = rule.Action.ForceProvider
		}
		if rule.Action.RateMultiplier > 0 {
			*multiplier *= rule.Action.RateMultiplier
		}
	}
	return forceProvider, false
}

func (s *Server) selectProviderAndKey(r *http.Request, model, forcedProvider string) (providerState, providerKeyState, error) {
	snap := s.snapshot.Get()
	now := time.Now()

	if forcedProvider != "" {
		p, ok := snap.providers[forcedProvider]
		if !ok || !p.Enabled {
			return providerState{}, providerKeyState{}, fmt.Errorf("forced provider %s unavailable", forcedProvider)
		}
		if !supportsModel(p.Models, model) {
			return providerState{}, providerKeyState{}, fmt.Errorf("forced provider %s does not support model %s", forcedProvider, model)
		}
		k, err := pickProviderKey(p, now)
		return p, k, err
	}

	stickyHeader := s.cfg.Gateway.StickySessionHeader
	sessionID := strings.TrimSpace(r.Header.Get(stickyHeader))
	if sessionID != "" {
		s.stickyMu.Lock()
		bind, ok := s.stickySession[sessionID]
		s.stickyMu.Unlock()
		if ok {
			if p, ok2 := snap.providers[bind.Provider]; ok2 && p.Enabled && supportsModel(p.Models, model) {
				k, err := pickProviderKey(p, now)
				if err == nil {
					return p, k, nil
				}
			}
		}
	}

	candidates := make([]providerState, 0, len(snap.providers))
	for _, p := range snap.providers {
		if !p.Enabled || !supportsModel(p.Models, model) {
			continue
		}
		if hasLiveKey(p, now) {
			candidates = append(candidates, p)
		}
	}
	if len(candidates) == 0 {
		return providerState{}, providerKeyState{}, fmt.Errorf("no provider available for model %s", model)
	}
	p := weightedProviderPick(candidates)
	k, err := pickProviderKey(p, now)
	if err != nil {
		return providerState{}, providerKeyState{}, err
	}
	if sessionID != "" {
		s.stickyMu.Lock()
		s.stickySession[sessionID] = stickyBind{Provider: p.Name, Updated: time.Now()}
		s.stickyMu.Unlock()
	}
	return p, k, nil
}

func hasLiveKey(p providerState, now time.Time) bool {
	for _, k := range p.Keys {
		if k.Enabled && now.After(k.CooldownUntil) {
			return true
		}
	}
	return false
}

func pickProviderKey(p providerState, now time.Time) (providerKeyState, error) {
	live := make([]providerKeyState, 0, len(p.Keys))
	for _, k := range p.Keys {
		if k.Enabled && now.After(k.CooldownUntil) {
			live = append(live, k)
		}
	}
	if len(live) == 0 {
		return providerKeyState{}, fmt.Errorf("provider %s has no available keys", p.Name)
	}
	return live[time.Now().UnixNano()%int64(len(live))], nil
}

func (s *Server) markProviderKeySuccess(key providerKeyState) {
	_ = s.store.UpdateProviderKeyHealth(key.ID, 0, 0, "ok")
	_ = s.reloadSnapshot()
}

func (s *Server) markProviderKeyFailure(provider providerState, key providerKeyState, status string) {
	nextErrs := key.ConsecutiveErrs + 1
	cooldownUntil := int64(0)
	if nextErrs >= s.cfg.Gateway.ProviderFailThreshold {
		cooldownUntil = time.Now().Add(maxDuration(provider.RecoverInterval, time.Duration(s.cfg.Gateway.ProviderCooldownSec)*time.Second)).Unix()
		nextErrs = 0
	}
	_ = s.store.UpdateProviderKeyHealth(key.ID, nextErrs, cooldownUntil, status)
	_ = s.reloadSnapshot()
}

func (s *Server) scheduleMultiplier(groupName string) float64 {
	if groupName == "" {
		groupName = "default"
	}
	snap := s.snapshot.Get()
	now := time.Now().In(s.loc)
	minute := now.Hour()*60 + now.Minute()
	mul := 1.0
	for _, sc := range snap.schedules {
		if !sc.Enabled || sc.GroupName != groupName {
			continue
		}
		if !sc.WeekdayMask[now.Weekday()] {
			continue
		}
		if minute >= sc.StartMinute && minute <= sc.EndMinute {
			if sc.Multiplier > 0 {
				mul *= sc.Multiplier
			}
		}
	}
	return mul
}

func readAndParseRequest(r *http.Request) ([]byte, parsedRequest, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, parsedRequest{}, err
	}
	defer r.Body.Close()

	var p parsedRequest
	if len(body) > 0 {
		_ = json.Unmarshal(body, &p)
	}
	p.EstimatedToken = estimateTokens(body)
	if p.Model == "" {
		p.Model = "unknown"
	}
	return body, p, nil
}

func maybeRewriteModel(body []byte, originalModel string, modelMap map[string]string) []byte {
	if len(modelMap) == 0 {
		return body
	}
	targetModel, ok := modelMap[originalModel]
	if !ok || targetModel == "" || targetModel == originalModel {
		return body
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	payload["model"] = targetModel
	rewritten, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return rewritten
}

func estimateTokens(body []byte) int {
	if len(body) == 0 {
		return 1
	}
	v := len(body) / 4
	if v <= 0 {
		return 1
	}
	return v
}

func extractUsage(respBody []byte) usage {
	var obj struct {
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &obj); err != nil {
		return usage{}
	}
	return usage{Prompt: obj.Usage.PromptTokens, Completion: obj.Usage.CompletionTokens, Total: obj.Usage.TotalTokens}
}

func evalRule(expr string, ctx map[string]any) (bool, error) {
	vm := goja.New()
	if err := vm.Set("ctx", ctx); err != nil {
		return false, err
	}
	value, err := vm.RunString(expr)
	if err != nil {
		return false, err
	}
	return value.ToBoolean(), nil
}

func copyHeaders(src, dst http.Header) {
	skipped := map[string]struct{}{
		"Host":           {},
		"Authorization":  {},
		"Content-Length": {},
	}
	for k, values := range src {
		if _, skip := skipped[k]; skip {
			continue
		}
		for _, v := range values {
			dst.Add(k, v)
		}
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func withRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) incStat(key string) {
	s.statsMu.Lock()
	defer s.statsMu.Unlock()
	s.stats[key]++
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

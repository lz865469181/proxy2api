package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"proxy2api/internal/store"
)

func (s *Server) handleAdminProviders(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	snap := s.snapshot.Get()
	type item struct {
		Name      string `json:"name"`
		Enabled   bool   `json:"enabled"`
		Weight    int    `json:"weight"`
		GroupName string `json:"group_name"`
		Keys      int    `json:"keys"`
	}
	out := make([]item, 0, len(snap.providers))
	for _, p := range snap.providers {
		out = append(out, item{
			Name:      p.Name,
			Enabled:   p.Enabled,
			Weight:    p.Weight,
			GroupName: p.GroupName,
			Keys:      len(p.Keys),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": out})
}

func (s *Server) handleAdminProviderAction(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/admin/providers/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}
	name, action := parts[0], parts[1]
	switch action {
	case "enable":
		if err := s.store.SetProviderEnabled(name, true); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	case "disable":
		if err := s.store.SetProviderEnabled(name, false); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action"})
		return
	}
	_ = s.reloadSnapshot()
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "enabled": action == "enable"})
}

func (s *Server) handleAdminProviderKeys(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if r.Method == http.MethodGet {
		list, err := s.store.ListProviderKeys("")
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"provider_keys": list})
		return
	}
	if r.Method == http.MethodPost {
		idStr := strings.TrimSpace(r.URL.Query().Get("id"))
		enableStr := strings.TrimSpace(r.URL.Query().Get("enabled"))
		if idStr == "" || enableStr == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id and enabled query are required"})
			return
		}
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}
		enabled := enableStr == "1" || strings.EqualFold(enableStr, "true")
		if err := s.store.SetProviderKeyEnabled(id, enabled); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		_ = s.reloadSnapshot()
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "enabled": enabled})
		return
	}
	writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
}

func (s *Server) handleAdminStats(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	s.statsMu.Lock()
	out := make(map[string]int64, len(s.stats))
	for k, v := range s.stats {
		out[k] = v
	}
	s.statsMu.Unlock()

	usage24h, _ := s.store.UsageSummaryLast24h()
	writeJSON(w, http.StatusOK, map[string]any{
		"stats":          out,
		"usage_last_24h": usage24h,
	})
}

func (s *Server) handleAdminRules(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		rules, err := s.store.ListRules()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"rules": rules})
	case http.MethodPost:
		var req struct {
			ID          int64           `json:"id"`
			Name        string          `json:"name"`
			Priority    int             `json:"priority"`
			Enabled     bool            `json:"enabled"`
			ConditionJS string          `json:"condition_js"`
			Action      json.RawMessage `json:"action"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
			return
		}
		if !json.Valid(req.Action) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "action must be valid json object"})
			return
		}
		id, err := s.store.UpsertRule(store.Rule{
			ID:          req.ID,
			Name:        req.Name,
			Priority:    req.Priority,
			Enabled:     req.Enabled,
			ConditionJS: req.ConditionJS,
			ActionJSON:  string(req.Action),
		})
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		_ = s.reloadSnapshot()
		writeJSON(w, http.StatusOK, map[string]any{"id": id})
	case http.MethodDelete:
		id, err := readIDQuery(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := s.store.DeleteRule(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		_ = s.reloadSnapshot()
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "deleted": true})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (s *Server) handleAdminSchedules(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		list, err := s.store.ListSchedules()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"schedules": list})
	case http.MethodPost:
		var req struct {
			ID          int64   `json:"id"`
			GroupName   string  `json:"group_name"`
			WeekdayMask string  `json:"weekday_mask"`
			StartHHMM   string  `json:"start_hhmm"`
			EndHHMM     string  `json:"end_hhmm"`
			Multiplier  float64 `json:"multiplier"`
			Enabled     bool    `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
			return
		}
		id, err := s.store.UpsertSchedule(store.GroupSchedule{
			ID:          req.ID,
			GroupName:   req.GroupName,
			WeekdayMask: req.WeekdayMask,
			StartHHMM:   req.StartHHMM,
			EndHHMM:     req.EndHHMM,
			Multiplier:  req.Multiplier,
			Enabled:     req.Enabled,
		})
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		_ = s.reloadSnapshot()
		writeJSON(w, http.StatusOK, map[string]any{"id": id})
	case http.MethodDelete:
		id, err := readIDQuery(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := s.store.DeleteSchedule(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		_ = s.reloadSnapshot()
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "deleted": true})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (s *Server) handleAdminConfigExport(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	data, err := s.store.ExportRuntimeConfig()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, data)
}

func (s *Server) handleAdminConfigImport(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read body failed"})
		return
	}
	var payload struct {
		Rules     []store.Rule          `json:"rules"`
		Schedules []store.GroupSchedule `json:"schedules"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return
	}
	if err := s.store.ReplaceRulesAndSchedules(payload.Rules, payload.Schedules); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	_ = s.reloadSnapshot()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAdminReload(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if err := s.reloadSnapshot(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAdminUI(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(fmt.Sprintf(`<!doctype html>
<html>
<head><meta charset="utf-8"/><title>proxy2api admin</title></head>
<body style="font-family:Consolas,monospace;padding:20px">
<h2>proxy2api Admin</h2>
<p>Use <code>X-Admin-Key</code> header for API calls.</p>
<ul>
  <li><a href="/admin/providers">/admin/providers</a></li>
  <li><a href="/admin/provider-keys">/admin/provider-keys</a></li>
  <li><a href="/admin/rules">/admin/rules</a></li>
  <li><a href="/admin/schedules">/admin/schedules</a></li>
  <li><a href="/admin/config/export">/admin/config/export</a></li>
  <li><a href="/admin/stats">/admin/stats</a></li>
</ul>
<p>Reload command:</p>
<pre>curl -X POST -H "X-Admin-Key: %s" http://localhost%s/admin/reload</pre>
</body></html>`, s.cfg.Auth.AdminKey, s.cfg.Server.Listen)))
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if s.cfg.Auth.AdminKey == "" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin api disabled (missing admin_key)"})
		return false
	}
	got := strings.TrimSpace(r.Header.Get("X-Admin-Key"))
	if got == "" || got != s.cfg.Auth.AdminKey {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "admin unauthorized"})
		return false
	}
	return true
}

func readIDQuery(r *http.Request) (int64, error) {
	idStr := strings.TrimSpace(r.URL.Query().Get("id"))
	if idStr == "" {
		return 0, fmt.Errorf("id query is required")
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid id")
	}
	return id, nil
}

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
	switch r.Method {
	case http.MethodGet:
		items, err := s.store.ListProviders()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"providers": items})
	case http.MethodPost:
		var req struct {
			ID                 int64             `json:"id"`
			Name               string            `json:"name"`
			BaseURL            string            `json:"base_url"`
			Weight             int               `json:"weight"`
			Models             []string          `json:"models"`
			ModelMap           map[string]string `json:"model_map"`
			MaxRPM             int               `json:"max_rpm"`
			MaxTPM             int               `json:"max_tpm"`
			Enabled            bool              `json:"enabled"`
			GroupName          string            `json:"group_name"`
			RecoverIntervalSec int               `json:"recover_interval_sec"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
			return
		}
		modelsRaw, _ := json.Marshal(req.Models)
		modelMapRaw, _ := json.Marshal(req.ModelMap)
		err := s.store.UpsertProvider(store.Provider{
			ID:                 req.ID,
			Name:               req.Name,
			BaseURL:            req.BaseURL,
			Weight:             req.Weight,
			ModelsJSON:         string(modelsRaw),
			ModelMapJSON:       string(modelMapRaw),
			MaxRPM:             req.MaxRPM,
			MaxTPM:             req.MaxTPM,
			Enabled:            req.Enabled,
			GroupName:          req.GroupName,
			RecoverIntervalSec: req.RecoverIntervalSec,
		})
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		_ = s.reloadSnapshot()
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case http.MethodDelete:
		name := strings.TrimSpace(r.URL.Query().Get("name"))
		if name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name query is required"})
			return
		}
		if err := s.store.DeleteProvider(name); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		_ = s.reloadSnapshot()
		writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "name": name})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
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
	switch r.Method {
	case http.MethodGet:
		list, err := s.store.ListProviderKeys("")
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"provider_keys": list})
	case http.MethodPost:
		var req struct {
			ID           int64  `json:"id"`
			ProviderName string `json:"provider_name"`
			Alias        string `json:"alias"`
			APIKey       string `json:"api_key"`
			Enabled      bool   `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
			return
		}
		id, err := s.store.UpsertProviderKey(store.ProviderKey{
			ID:           req.ID,
			ProviderName: req.ProviderName,
			Alias:        req.Alias,
			APIKey:       req.APIKey,
			Enabled:      req.Enabled,
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
		if err := s.store.DeleteProviderKey(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		_ = s.reloadSnapshot()
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "deleted": true})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (s *Server) handleAdminAPIKeys(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		list, err := s.store.ListAPIKeys()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"api_keys": list})
	case http.MethodPost:
		var req struct {
			ID             int64    `json:"id"`
			KeyValue       string   `json:"key_value"`
			Name           string   `json:"name"`
			MaxRPM         int      `json:"max_rpm"`
			MaxTPM         int      `json:"max_tpm"`
			AllowedModels  []string `json:"allowed_models"`
			RateMultiplier float64  `json:"rate_multiplier"`
			Enabled        bool     `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
			return
		}
		allowRaw, _ := json.Marshal(req.AllowedModels)
		id, err := s.store.UpsertAPIKey(store.APIKey{
			ID:             req.ID,
			KeyValue:       req.KeyValue,
			Name:           req.Name,
			MaxRPM:         req.MaxRPM,
			MaxTPM:         req.MaxTPM,
			AllowedModels:  string(allowRaw),
			RateMultiplier: req.RateMultiplier,
			Enabled:        req.Enabled,
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
		if err := s.store.DeleteAPIKey(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		_ = s.reloadSnapshot()
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "deleted": true})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
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
	_, _ = w.Write([]byte(`<!doctype html>
<html>
<head>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width, initial-scale=1"/>
  <title>proxy2api Admin</title>
  <style>
    :root { --bg:#f2f4f8; --card:#fff; --text:#1f2937; --muted:#6b7280; --line:#d1d5db; --accent:#0f766e; }
    * { box-sizing: border-box; }
    body { margin:0; font-family:"Segoe UI",system-ui,sans-serif; background:linear-gradient(120deg,#f2f4f8,#eaf5f4); color:var(--text);}
    header { padding:14px 20px; background:#0f766e; color:#fff; font-weight:700; }
    .wrap { padding:16px; display:grid; grid-template-columns: repeat(auto-fit, minmax(340px, 1fr)); gap:12px; }
    .card { background:var(--card); border:1px solid var(--line); border-radius:12px; padding:12px; box-shadow:0 1px 4px rgba(0,0,0,.06);}
    h3 { margin:0 0 8px 0; font-size:16px; }
    textarea,input,select { width:100%; margin:4px 0; padding:8px; border:1px solid var(--line); border-radius:8px; }
    button { padding:8px 12px; border:none; background:var(--accent); color:#fff; border-radius:8px; cursor:pointer; }
    pre { white-space:pre-wrap; word-break:break-word; background:#0b1020; color:#b8f2df; padding:10px; border-radius:8px; min-height:120px; max-height:320px; overflow:auto; }
    .row { display:flex; gap:8px; align-items:center; }
    .row > * { flex:1; }
    .hint { color:var(--muted); font-size:12px; margin:0 0 8px 0;}
  </style>
</head>
<body>
  <header>proxy2api Admin Console</header>
  <div class="wrap">
    <div class="card">
      <h3>Stats</h3>
      <p class="hint">Live stats and 24h usage summary.</p>
      <div class="row"><button onclick="fetchJSON('/admin/stats','statsOut')">Refresh</button><button onclick="postEmpty('/admin/reload')">Reload Snapshot</button></div>
      <pre id="statsOut"></pre>
    </div>
    <div class="card">
      <h3>Providers</h3>
      <p class="hint">Create/update provider with JSON payload.</p>
      <div class="row"><button onclick="fetchJSON('/admin/providers','providersOut')">List</button></div>
      <textarea id="providerBody" rows="8">{ "name":"openai-main","base_url":"https://api.openai.com","weight":5,"models":["gpt-4o-mini"],"model_map":{},"max_rpm":1000,"max_tpm":2000000,"enabled":true,"group_name":"default","recover_interval_sec":60 }</textarea>
      <div class="row"><button onclick="postJSON('/admin/providers','providerBody','providersOut')">Upsert</button><input id="providerDeleteName" placeholder="delete by name"/><button onclick="delByName('/admin/providers','providerDeleteName','providersOut')">Delete</button></div>
      <pre id="providersOut"></pre>
    </div>
    <div class="card">
      <h3>Provider Keys</h3>
      <div class="row"><button onclick="fetchJSON('/admin/provider-keys','pkeysOut')">List</button></div>
      <textarea id="pkeyBody" rows="6">{ "provider_name":"openai-main","alias":"k1","api_key":"sk-xxx","enabled":true }</textarea>
      <div class="row"><button onclick="postJSON('/admin/provider-keys','pkeyBody','pkeysOut')">Upsert</button><input id="pkeyDeleteId" placeholder="delete id"/><button onclick="delById('/admin/provider-keys','pkeyDeleteId','pkeysOut')">Delete</button></div>
      <pre id="pkeysOut"></pre>
    </div>
    <div class="card">
      <h3>Gateway API Keys</h3>
      <div class="row"><button onclick="fetchJSON('/admin/api-keys','apiKeysOut')">List</button></div>
      <textarea id="apiKeyBody" rows="7">{ "key_value":"sk-user-demo","name":"demo","max_rpm":120,"max_tpm":300000,"allowed_models":["gpt-4o-mini"],"rate_multiplier":1.0,"enabled":true }</textarea>
      <div class="row"><button onclick="postJSON('/admin/api-keys','apiKeyBody','apiKeysOut')">Upsert</button><input id="apiKeyDeleteId" placeholder="delete id"/><button onclick="delById('/admin/api-keys','apiKeyDeleteId','apiKeysOut')">Delete</button></div>
      <pre id="apiKeysOut"></pre>
    </div>
    <div class="card">
      <h3>Rules</h3>
      <div class="row"><button onclick="fetchJSON('/admin/rules','rulesOut')">List</button></div>
      <textarea id="ruleBody" rows="7">{ "name":"deny-exp","priority":10,"enabled":true,"condition_js":"ctx.model.startsWith('exp-')","action":{"deny":true} }</textarea>
      <div class="row"><button onclick="postJSON('/admin/rules','ruleBody','rulesOut')">Upsert</button><input id="ruleDeleteId" placeholder="delete id"/><button onclick="delById('/admin/rules','ruleDeleteId','rulesOut')">Delete</button></div>
      <pre id="rulesOut"></pre>
    </div>
    <div class="card">
      <h3>Schedules</h3>
      <div class="row"><button onclick="fetchJSON('/admin/schedules','schedOut')">List</button></div>
      <textarea id="schedBody" rows="6">{ "group_name":"default","weekday_mask":"1,2,3,4,5","start_hhmm":"09:00","end_hhmm":"18:00","multiplier":0.8,"enabled":true }</textarea>
      <div class="row"><button onclick="postJSON('/admin/schedules','schedBody','schedOut')">Upsert</button><input id="schedDeleteId" placeholder="delete id"/><button onclick="delById('/admin/schedules','schedDeleteId','schedOut')">Delete</button></div>
      <pre id="schedOut"></pre>
    </div>
    <div class="card">
      <h3>Config Export/Import</h3>
      <div class="row"><button onclick="fetchJSON('/admin/config/export','cfgOut')">Export</button></div>
      <textarea id="cfgIn" rows="8" placeholder='{"rules":[...],"schedules":[...]}'></textarea>
      <div class="row"><button onclick="importConfig()">Import Rules+Schedules</button></div>
      <pre id="cfgOut"></pre>
    </div>
  </div>
<script>
async function api(url,opt){
  opt=opt||{}; opt.headers=opt.headers||{};
  const k = prompt('X-Admin-Key');
  if(!k){ throw new Error('no admin key'); }
  opt.headers['X-Admin-Key']=k;
  if(!opt.headers['Content-Type'] && opt.body){ opt.headers['Content-Type']='application/json'; }
  const r=await fetch(url,opt); const t=await r.text();
  let d=t; try{ d=JSON.parse(t);}catch(e){}
  if(!r.ok){ throw new Error(typeof d==='string'?d:JSON.stringify(d,null,2));}
  return d;
}
async function fetchJSON(url,out){ try{ const d=await api(url); document.getElementById(out).textContent=JSON.stringify(d,null,2);}catch(e){document.getElementById(out).textContent=String(e);} }
async function postJSON(url,bodyId,out){ try{ const d=await api(url,{method:'POST',body:document.getElementById(bodyId).value}); document.getElementById(out).textContent=JSON.stringify(d,null,2);}catch(e){document.getElementById(out).textContent=String(e);} }
async function delById(url,inputId,out){ const id=document.getElementById(inputId).value.trim(); if(!id)return; try{ const d=await api(url+'?id='+encodeURIComponent(id),{method:'DELETE'}); document.getElementById(out).textContent=JSON.stringify(d,null,2);}catch(e){document.getElementById(out).textContent=String(e);} }
async function delByName(url,inputId,out){ const name=document.getElementById(inputId).value.trim(); if(!name)return; try{ const d=await api(url+'?name='+encodeURIComponent(name),{method:'DELETE'}); document.getElementById(out).textContent=JSON.stringify(d,null,2);}catch(e){document.getElementById(out).textContent=String(e);} }
async function postEmpty(url){ try{ const d=await api(url,{method:'POST'}); alert(JSON.stringify(d)); }catch(e){ alert(String(e)); } }
async function importConfig(){ const v=document.getElementById('cfgIn').value; try{ const d=await api('/admin/config/import',{method:'POST',body:v}); document.getElementById('cfgOut').textContent=JSON.stringify(d,null,2);}catch(e){document.getElementById('cfgOut').textContent=String(e);} }
</script>
</body>
</html>`))
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

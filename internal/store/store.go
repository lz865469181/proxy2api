package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	_ "modernc.org/sqlite"

	"proxy2api/internal/config"
)

type Store struct {
	db *sql.DB
}

type Provider struct {
	ID                 int64
	Name               string
	BaseURL            string
	Weight             int
	ModelsJSON         string
	ModelMapJSON       string
	MaxRPM             int
	MaxTPM             int
	Enabled            bool
	GroupName          string
	RecoverIntervalSec int
}

type ProviderKey struct {
	ID              int64
	ProviderName    string
	Alias           string
	APIKey          string
	Enabled         bool
	ConsecutiveErrs int
	CooldownUntil   int64
	LastStatus      string
}

type APIKey struct {
	ID             int64
	KeyValue       string
	Name           string
	MaxRPM         int
	MaxTPM         int
	AllowedModels  string
	RateMultiplier float64
	Enabled        bool
}

type Rule struct {
	ID          int64
	Name        string
	Priority    int
	Enabled     bool
	ConditionJS string
	ActionJSON  string
}

type GroupSchedule struct {
	ID          int64
	GroupName   string
	WeekdayMask string
	StartHHMM   string
	EndHHMM     string
	Multiplier  float64
	Enabled     bool
}

type UsageRecord struct {
	AtUnix           int64
	APIKey           string
	Provider         string
	ProviderKey      string
	Model            string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

type RuntimeConfigExport struct {
	Providers    []Provider      `json:"providers"`
	ProviderKeys []ProviderKey   `json:"provider_keys"`
	APIKeys      []APIKey        `json:"api_keys"`
	Rules        []Rule          `json:"rules"`
	Schedules    []GroupSchedule `json:"schedules"`
}

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("db path cannot be empty")
	}
	if err := os.MkdirAll(dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS providers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			base_url TEXT NOT NULL,
			weight INTEGER NOT NULL DEFAULT 1,
			models_json TEXT NOT NULL DEFAULT '[]',
			model_map_json TEXT NOT NULL DEFAULT '{}',
			max_rpm INTEGER NOT NULL DEFAULT 0,
			max_tpm INTEGER NOT NULL DEFAULT 0,
			enabled INTEGER NOT NULL DEFAULT 1,
			group_name TEXT NOT NULL DEFAULT 'default',
			recover_interval_sec INTEGER NOT NULL DEFAULT 60
		);`,
		`CREATE TABLE IF NOT EXISTS provider_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			provider_name TEXT NOT NULL,
			alias TEXT NOT NULL,
			api_key TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			consecutive_errs INTEGER NOT NULL DEFAULT 0,
			cooldown_until INTEGER NOT NULL DEFAULT 0,
			last_status TEXT NOT NULL DEFAULT '',
			UNIQUE(provider_name, alias)
		);`,
		`CREATE TABLE IF NOT EXISTS api_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			key_value TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			max_rpm INTEGER NOT NULL DEFAULT 0,
			max_tpm INTEGER NOT NULL DEFAULT 0,
			allowed_models_json TEXT NOT NULL DEFAULT '[]',
			rate_multiplier REAL NOT NULL DEFAULT 1.0,
			enabled INTEGER NOT NULL DEFAULT 1
		);`,
		`CREATE TABLE IF NOT EXISTS rules (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			priority INTEGER NOT NULL DEFAULT 100,
			enabled INTEGER NOT NULL DEFAULT 1,
			condition_js TEXT NOT NULL,
			action_json TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS group_schedules (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			group_name TEXT NOT NULL,
			weekday_mask TEXT NOT NULL DEFAULT '1,2,3,4,5,6,7',
			start_hhmm TEXT NOT NULL DEFAULT '00:00',
			end_hhmm TEXT NOT NULL DEFAULT '23:59',
			multiplier REAL NOT NULL DEFAULT 1.0,
			enabled INTEGER NOT NULL DEFAULT 1
		);`,
		`CREATE TABLE IF NOT EXISTS usage_records (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			at_unix INTEGER NOT NULL,
			api_key TEXT NOT NULL,
			provider TEXT NOT NULL,
			provider_key TEXT NOT NULL,
			model TEXT NOT NULL,
			prompt_tokens INTEGER NOT NULL DEFAULT 0,
			completion_tokens INTEGER NOT NULL DEFAULT 0,
			total_tokens INTEGER NOT NULL DEFAULT 0
		);`,
		`CREATE TABLE IF NOT EXISTS kv (
			k TEXT PRIMARY KEY,
			v TEXT NOT NULL
		);`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SeedFromConfig(cfg *config.Config) error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM providers`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	for _, p := range cfg.Providers {
		modelsRaw, _ := json.Marshal(p.Models)
		modelMapRaw, _ := json.Marshal(p.ModelMap)
		groupName := "default"
		if p.GroupName != "" {
			groupName = p.GroupName
		}
		_, err = tx.Exec(
			`INSERT INTO providers(name, base_url, weight, models_json, model_map_json, max_rpm, max_tpm, enabled, group_name, recover_interval_sec)
			 VALUES(?,?,?,?,?,?,?,?,?,?)`,
			p.Name, p.BaseURL, p.Weight, string(modelsRaw), string(modelMapRaw),
			p.MaxRPM, p.MaxTPM, boolToInt(p.Enabled), groupName, max(1, p.RecoverIntervalSec),
		)
		if err != nil {
			return err
		}

		if len(p.UpstreamKeys) == 0 && p.APIKey != "" {
			_, err = tx.Exec(
				`INSERT INTO provider_keys(provider_name, alias, api_key, enabled) VALUES(?,?,?,1)`,
				p.Name, "primary", p.APIKey,
			)
			if err != nil {
				return err
			}
		}
		for i, k := range p.UpstreamKeys {
			if k == "" {
				continue
			}
			_, err = tx.Exec(
				`INSERT INTO provider_keys(provider_name, alias, api_key, enabled) VALUES(?,?,?,1)`,
				p.Name, fmt.Sprintf("k%d", i+1), k,
			)
			if err != nil {
				return err
			}
		}
	}

	for _, k := range cfg.Keys {
		allowedRaw, _ := json.Marshal(k.AllowedModels)
		_, err = tx.Exec(
			`INSERT INTO api_keys(key_value, name, max_rpm, max_tpm, allowed_models_json, rate_multiplier, enabled)
			 VALUES(?,?,?,?,?,?,1)`,
			k.Key, k.Name, k.MaxRPM, k.MaxTPM, string(allowedRaw), k.RateMultiplier,
		)
		if err != nil {
			return err
		}
	}

	for idx, rule := range cfg.Rules {
		actionRaw, _ := json.Marshal(rule.Action)
		cond := buildConditionJS(rule)
		_, err = tx.Exec(
			`INSERT INTO rules(name, priority, enabled, condition_js, action_json) VALUES(?,?,?,?,?)`,
			rule.Name, idx+1, 1, cond, string(actionRaw),
		)
		if err != nil {
			return err
		}
	}

	if _, err = tx.Exec(`INSERT OR REPLACE INTO kv(k,v) VALUES('seeded_at',?)`, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	return tx.Commit()
}

func buildConditionJS(rule config.RoutingRule) string {
	parts := make([]string, 0, 2)
	if len(rule.Match.APIKeys) > 0 {
		b, _ := json.Marshal(rule.Match.APIKeys)
		parts = append(parts, fmt.Sprintf("%s.includes(ctx.api_key)", string(b)))
	}
	if rule.Match.ModelPrefix != "" {
		parts = append(parts, fmt.Sprintf("ctx.model.startsWith(%q)", rule.Match.ModelPrefix))
	}
	if len(parts) == 0 {
		return "true"
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return "(" + parts[0] + ") && (" + parts[1] + ")"
}

func (s *Store) ListProviders() ([]Provider, error) {
	rows, err := s.db.Query(`SELECT id,name,base_url,weight,models_json,model_map_json,max_rpm,max_tpm,enabled,group_name,recover_interval_sec FROM providers ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Provider, 0)
	for rows.Next() {
		var p Provider
		var enabled int
		if err := rows.Scan(&p.ID, &p.Name, &p.BaseURL, &p.Weight, &p.ModelsJSON, &p.ModelMapJSON, &p.MaxRPM, &p.MaxTPM, &enabled, &p.GroupName, &p.RecoverIntervalSec); err != nil {
			return nil, err
		}
		p.Enabled = enabled == 1
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) SetProviderEnabled(name string, enabled bool) error {
	_, err := s.db.Exec(`UPDATE providers SET enabled=? WHERE name=?`, boolToInt(enabled), name)
	return err
}

func (s *Store) ListProviderKeys(providerName string) ([]ProviderKey, error) {
	query := `SELECT id,provider_name,alias,api_key,enabled,consecutive_errs,cooldown_until,last_status FROM provider_keys`
	args := []any{}
	if providerName != "" {
		query += ` WHERE provider_name=?`
		args = append(args, providerName)
	}
	query += ` ORDER BY provider_name, id`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ProviderKey, 0)
	for rows.Next() {
		var x ProviderKey
		var enabled int
		if err := rows.Scan(&x.ID, &x.ProviderName, &x.Alias, &x.APIKey, &enabled, &x.ConsecutiveErrs, &x.CooldownUntil, &x.LastStatus); err != nil {
			return nil, err
		}
		x.Enabled = enabled == 1
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) UpdateProviderKeyHealth(id int64, consecutiveErrs int, cooldownUntil int64, lastStatus string) error {
	_, err := s.db.Exec(`UPDATE provider_keys SET consecutive_errs=?, cooldown_until=?, last_status=? WHERE id=?`, consecutiveErrs, cooldownUntil, lastStatus, id)
	return err
}

func (s *Store) SetProviderKeyEnabled(id int64, enabled bool) error {
	_, err := s.db.Exec(`UPDATE provider_keys SET enabled=? WHERE id=?`, boolToInt(enabled), id)
	return err
}

func (s *Store) ListAPIKeys() ([]APIKey, error) {
	rows, err := s.db.Query(`SELECT id,key_value,name,max_rpm,max_tpm,allowed_models_json,rate_multiplier,enabled FROM api_keys ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]APIKey, 0)
	for rows.Next() {
		var x APIKey
		var enabled int
		if err := rows.Scan(&x.ID, &x.KeyValue, &x.Name, &x.MaxRPM, &x.MaxTPM, &x.AllowedModels, &x.RateMultiplier, &enabled); err != nil {
			return nil, err
		}
		x.Enabled = enabled == 1
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) ListRules() ([]Rule, error) {
	rows, err := s.db.Query(`SELECT id,name,priority,enabled,condition_js,action_json FROM rules ORDER BY priority ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Rule, 0)
	for rows.Next() {
		var x Rule
		var enabled int
		if err := rows.Scan(&x.ID, &x.Name, &x.Priority, &enabled, &x.ConditionJS, &x.ActionJSON); err != nil {
			return nil, err
		}
		x.Enabled = enabled == 1
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) UpsertRule(r Rule) (int64, error) {
	if r.Name == "" {
		return 0, errors.New("rule name is required")
	}
	if r.ConditionJS == "" {
		return 0, errors.New("condition_js is required")
	}
	if r.ActionJSON == "" {
		return 0, errors.New("action_json is required")
	}
	if r.Priority <= 0 {
		r.Priority = 100
	}

	if r.ID > 0 {
		_, err := s.db.Exec(
			`UPDATE rules SET name=?, priority=?, enabled=?, condition_js=?, action_json=? WHERE id=?`,
			r.Name, r.Priority, boolToInt(r.Enabled), r.ConditionJS, r.ActionJSON, r.ID,
		)
		if err != nil {
			return 0, err
		}
		return r.ID, nil
	}

	res, err := s.db.Exec(
		`INSERT INTO rules(name, priority, enabled, condition_js, action_json) VALUES(?,?,?,?,?)`,
		r.Name, r.Priority, boolToInt(r.Enabled), r.ConditionJS, r.ActionJSON,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) DeleteRule(id int64) error {
	_, err := s.db.Exec(`DELETE FROM rules WHERE id=?`, id)
	return err
}

func (s *Store) ListSchedules() ([]GroupSchedule, error) {
	rows, err := s.db.Query(`SELECT id,group_name,weekday_mask,start_hhmm,end_hhmm,multiplier,enabled FROM group_schedules ORDER BY group_name,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]GroupSchedule, 0)
	for rows.Next() {
		var x GroupSchedule
		var enabled int
		if err := rows.Scan(&x.ID, &x.GroupName, &x.WeekdayMask, &x.StartHHMM, &x.EndHHMM, &x.Multiplier, &enabled); err != nil {
			return nil, err
		}
		x.Enabled = enabled == 1
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) UpsertSchedule(sc GroupSchedule) (int64, error) {
	if sc.GroupName == "" {
		return 0, errors.New("group_name is required")
	}
	if sc.WeekdayMask == "" {
		sc.WeekdayMask = "1,2,3,4,5,6,7"
	}
	if sc.StartHHMM == "" {
		sc.StartHHMM = "00:00"
	}
	if sc.EndHHMM == "" {
		sc.EndHHMM = "23:59"
	}
	if sc.Multiplier <= 0 {
		sc.Multiplier = 1
	}

	if sc.ID > 0 {
		_, err := s.db.Exec(
			`UPDATE group_schedules SET group_name=?, weekday_mask=?, start_hhmm=?, end_hhmm=?, multiplier=?, enabled=? WHERE id=?`,
			sc.GroupName, sc.WeekdayMask, sc.StartHHMM, sc.EndHHMM, sc.Multiplier, boolToInt(sc.Enabled), sc.ID,
		)
		if err != nil {
			return 0, err
		}
		return sc.ID, nil
	}

	res, err := s.db.Exec(
		`INSERT INTO group_schedules(group_name, weekday_mask, start_hhmm, end_hhmm, multiplier, enabled) VALUES(?,?,?,?,?,?)`,
		sc.GroupName, sc.WeekdayMask, sc.StartHHMM, sc.EndHHMM, sc.Multiplier, boolToInt(sc.Enabled),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) DeleteSchedule(id int64) error {
	_, err := s.db.Exec(`DELETE FROM group_schedules WHERE id=?`, id)
	return err
}

func (s *Store) ExportRuntimeConfig() (RuntimeConfigExport, error) {
	providers, err := s.ListProviders()
	if err != nil {
		return RuntimeConfigExport{}, err
	}
	providerKeys, err := s.ListProviderKeys("")
	if err != nil {
		return RuntimeConfigExport{}, err
	}
	apiKeys, err := s.ListAPIKeys()
	if err != nil {
		return RuntimeConfigExport{}, err
	}
	rules, err := s.ListRules()
	if err != nil {
		return RuntimeConfigExport{}, err
	}
	schedules, err := s.ListSchedules()
	if err != nil {
		return RuntimeConfigExport{}, err
	}
	return RuntimeConfigExport{
		Providers:    providers,
		ProviderKeys: providerKeys,
		APIKeys:      apiKeys,
		Rules:        rules,
		Schedules:    schedules,
	}, nil
}

func (s *Store) ReplaceRulesAndSchedules(rules []Rule, schedules []GroupSchedule) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.Exec(`DELETE FROM rules`); err != nil {
		return err
	}
	for _, r := range rules {
		if r.Name == "" || r.ConditionJS == "" || r.ActionJSON == "" {
			continue
		}
		if r.Priority <= 0 {
			r.Priority = 100
		}
		if _, err = tx.Exec(
			`INSERT INTO rules(name, priority, enabled, condition_js, action_json) VALUES(?,?,?,?,?)`,
			r.Name, r.Priority, boolToInt(r.Enabled), r.ConditionJS, r.ActionJSON,
		); err != nil {
			return err
		}
	}

	if _, err = tx.Exec(`DELETE FROM group_schedules`); err != nil {
		return err
	}
	for _, sc := range schedules {
		if sc.GroupName == "" {
			continue
		}
		if sc.WeekdayMask == "" {
			sc.WeekdayMask = "1,2,3,4,5,6,7"
		}
		if sc.StartHHMM == "" {
			sc.StartHHMM = "00:00"
		}
		if sc.EndHHMM == "" {
			sc.EndHHMM = "23:59"
		}
		if sc.Multiplier <= 0 {
			sc.Multiplier = 1
		}
		if _, err = tx.Exec(
			`INSERT INTO group_schedules(group_name, weekday_mask, start_hhmm, end_hhmm, multiplier, enabled) VALUES(?,?,?,?,?,?)`,
			sc.GroupName, sc.WeekdayMask, sc.StartHHMM, sc.EndHHMM, sc.Multiplier, boolToInt(sc.Enabled),
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *Store) AddUsage(u UsageRecord) error {
	_, err := s.db.Exec(
		`INSERT INTO usage_records(at_unix,api_key,provider,provider_key,model,prompt_tokens,completion_tokens,total_tokens)
		 VALUES(?,?,?,?,?,?,?,?)`,
		u.AtUnix, u.APIKey, u.Provider, u.ProviderKey, u.Model, u.PromptTokens, u.CompletionTokens, u.TotalTokens,
	)
	return err
}

func (s *Store) UsageSummaryLast24h() (map[string]int64, error) {
	since := time.Now().Add(-24 * time.Hour).Unix()
	rows, err := s.db.Query(`SELECT api_key, SUM(total_tokens) FROM usage_records WHERE at_unix>=? GROUP BY api_key`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var k string
		var v sql.NullInt64
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		if v.Valid {
			out[k] = v.Int64
		}
	}
	return out, rows.Err()
}

func dir(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			if i == 0 {
				return "."
			}
			return path[:i]
		}
	}
	return "."
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

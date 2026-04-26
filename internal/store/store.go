package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"proxy2api/internal/config"
)

type Store struct {
	db *gorm.DB
}

type Provider struct {
	ID                 int64  `gorm:"primaryKey;autoIncrement"`
	Name               string `gorm:"size:191;uniqueIndex;not null"`
	BaseURL            string `gorm:"type:text;not null"`
	Weight             int    `gorm:"not null;default:1"`
	ModelsJSON         string `gorm:"type:text;not null;default:'[]'"`
	ModelMapJSON       string `gorm:"type:text;not null;default:'{}'"`
	MaxRPM             int    `gorm:"not null;default:0"`
	MaxTPM             int    `gorm:"not null;default:0"`
	Enabled            bool   `gorm:"not null;default:true"`
	GroupName          string `gorm:"size:191;not null;default:'default'"`
	RecoverIntervalSec int    `gorm:"not null;default:60"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type ProviderKey struct {
	ID              int64  `gorm:"primaryKey;autoIncrement"`
	ProviderName    string `gorm:"size:191;index;not null"`
	Alias           string `gorm:"size:191;not null"`
	APIKey          string `gorm:"type:text;not null"`
	Enabled         bool   `gorm:"not null;default:true"`
	ConsecutiveErrs int    `gorm:"not null;default:0"`
	CooldownUntil   int64  `gorm:"not null;default:0"`
	LastStatus      string `gorm:"type:text;not null;default:''"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type APIKey struct {
	ID             int64   `gorm:"primaryKey;autoIncrement"`
	KeyValue       string  `gorm:"size:191;uniqueIndex;not null"`
	Name           string  `gorm:"size:191;not null"`
	TenantName     string  `gorm:"size:191;index;not null;default:'default'"`
	BalanceMicros  int64   `gorm:"not null;default:0"`
	MaxRPM         int     `gorm:"not null;default:0"`
	MaxTPM         int     `gorm:"not null;default:0"`
	AllowedModels  string  `gorm:"type:text;not null;default:'[]'"`
	RateMultiplier float64 `gorm:"not null;default:1.0"`
	Enabled        bool    `gorm:"not null;default:true"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Rule struct {
	ID          int64  `gorm:"primaryKey;autoIncrement"`
	Name        string `gorm:"size:191;uniqueIndex;not null"`
	Priority    int    `gorm:"not null;default:100"`
	Enabled     bool   `gorm:"not null;default:true"`
	ConditionJS string `gorm:"type:text;not null"`
	ActionJSON  string `gorm:"type:text;not null"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type GroupSchedule struct {
	ID          int64   `gorm:"primaryKey;autoIncrement"`
	GroupName   string  `gorm:"size:191;index;not null"`
	WeekdayMask string  `gorm:"size:64;not null;default:'1,2,3,4,5,6,7'"`
	StartHHMM   string  `gorm:"size:8;not null;default:'00:00'"`
	EndHHMM     string  `gorm:"size:8;not null;default:'23:59'"`
	Multiplier  float64 `gorm:"not null;default:1.0"`
	Enabled     bool    `gorm:"not null;default:true"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type UsageRecord struct {
	ID               int64  `gorm:"primaryKey;autoIncrement"`
	AtUnix           int64  `gorm:"index;not null"`
	APIKey           string `gorm:"size:191;index;not null"`
	Provider         string `gorm:"size:191;index;not null"`
	ProviderKey      string `gorm:"size:191;index;not null"`
	Model            string `gorm:"size:191;index;not null"`
	PromptTokens     int    `gorm:"not null;default:0"`
	CompletionTokens int    `gorm:"not null;default:0"`
	TotalTokens      int    `gorm:"not null;default:0"`
	CreatedAt        time.Time
}

type KV struct {
	K string `gorm:"primaryKey;size:191"`
	V string `gorm:"type:text;not null"`
}

type Tenant struct {
	ID        int64  `gorm:"primaryKey;autoIncrement"`
	Name      string `gorm:"size:191;uniqueIndex;not null"`
	Enabled   bool   `gorm:"not null;default:true"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

type AdminUser struct {
	ID           int64  `gorm:"primaryKey;autoIncrement"`
	Username     string `gorm:"size:191;uniqueIndex;not null"`
	PasswordHash string `gorm:"type:text;not null"`
	Role         string `gorm:"size:32;not null;default:'viewer'"`
	Token        string `gorm:"type:text;not null;default:''"`
	Enabled      bool   `gorm:"not null;default:true"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type PriceRule struct {
	ID               int64  `gorm:"primaryKey;autoIncrement"`
	ModelPrefix      string `gorm:"size:191;uniqueIndex;not null"`
	PricePer1MMicros int64  `gorm:"not null;default:0"`
	Enabled          bool   `gorm:"not null;default:true"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type BillingTxn struct {
	ID          int64  `gorm:"primaryKey;autoIncrement"`
	APIKey      string `gorm:"size:191;index;not null"`
	DeltaMicros int64  `gorm:"not null"`
	Reason      string `gorm:"size:64;not null"`
	Ref         string `gorm:"size:191;not null;default:''"`
	CreatedAt   time.Time
}

type AuditLog struct {
	ID         int64  `gorm:"primaryKey;autoIncrement"`
	Actor      string `gorm:"size:191;index;not null"`
	Action     string `gorm:"size:191;index;not null"`
	Target     string `gorm:"size:191;not null;default:''"`
	DetailJSON string `gorm:"type:text;not null;default:'{}'"`
	CreatedAt  time.Time
}

type RuntimeConfigExport struct {
	Providers    []Provider      `json:"providers"`
	ProviderKeys []ProviderKey   `json:"provider_keys"`
	APIKeys      []APIKey        `json:"api_keys"`
	Rules        []Rule          `json:"rules"`
	Schedules    []GroupSchedule `json:"schedules"`
}

func (ProviderKey) TableName() string {
	return "provider_keys"
}
func (APIKey) TableName() string {
	return "api_keys"
}
func (GroupSchedule) TableName() string {
	return "group_schedules"
}
func (UsageRecord) TableName() string {
	return "usage_records"
}

func Open(cfg config.DBConfig) (*Store, error) {
	driver := strings.ToLower(strings.TrimSpace(cfg.Driver))
	if driver == "" {
		driver = "sqlite"
	}

	var (
		db  *gorm.DB
		err error
	)
	switch driver {
	case "sqlite":
		path := cfg.Path
		if path == "" {
			path = "data/proxy2api.db"
		}
		db, err = gorm.Open(sqlite.Open(path), &gorm.Config{})
	case "mysql":
		if cfg.DSN == "" {
			return nil, errors.New("db.dsn is required for mysql")
		}
		db, err = gorm.Open(mysql.Open(cfg.DSN), &gorm.Config{})
	case "postgres", "postgresql":
		if cfg.DSN == "" {
			return nil, errors.New("db.dsn is required for postgres")
		}
		db, err = gorm.Open(postgres.Open(cfg.DSN), &gorm.Config{})
	default:
		return nil, fmt.Errorf("unsupported db driver: %s", cfg.Driver)
	}
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	if cfg.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetimeSec > 0 {
		sqlDB.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetimeSec) * time.Second)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	if err := s.ensureIndexes(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	return s.db.AutoMigrate(
		&Provider{},
		&ProviderKey{},
		&APIKey{},
		&Rule{},
		&GroupSchedule{},
		&UsageRecord{},
		&KV{},
		&Tenant{},
		&AdminUser{},
		&PriceRule{},
		&BillingTxn{},
		&AuditLog{},
	)
}

func (s *Store) ensureIndexes() error {
	return s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uq_provider_keys_provider_alias ON provider_keys(provider_name, alias)`).Error
}

func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func (s *Store) SeedFromConfig(cfg *config.Config) error {
	var n int64
	if err := s.db.Model(&Provider{}).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}

	return s.db.Transaction(func(tx *gorm.DB) error {
		for _, p := range cfg.Providers {
			modelsRaw, _ := json.Marshal(p.Models)
			modelMapRaw, _ := json.Marshal(p.ModelMap)
			provider := Provider{
				Name:               p.Name,
				BaseURL:            p.BaseURL,
				Weight:             p.Weight,
				ModelsJSON:         string(modelsRaw),
				ModelMapJSON:       string(modelMapRaw),
				MaxRPM:             p.MaxRPM,
				MaxTPM:             p.MaxTPM,
				Enabled:            p.Enabled,
				GroupName:          defaultStr(p.GroupName, "default"),
				RecoverIntervalSec: max(1, p.RecoverIntervalSec),
			}
			if err := tx.Create(&provider).Error; err != nil {
				return err
			}

			if len(p.UpstreamKeys) == 0 && p.APIKey != "" {
				if err := tx.Create(&ProviderKey{
					ProviderName: p.Name,
					Alias:        "primary",
					APIKey:       p.APIKey,
					Enabled:      true,
				}).Error; err != nil {
					return err
				}
			}
			for i, k := range p.UpstreamKeys {
				if strings.TrimSpace(k) == "" {
					continue
				}
				if err := tx.Create(&ProviderKey{
					ProviderName: p.Name,
					Alias:        fmt.Sprintf("k%d", i+1),
					APIKey:       k,
					Enabled:      true,
				}).Error; err != nil {
					return err
				}
			}
		}

		for _, k := range cfg.Keys {
			allowRaw, _ := json.Marshal(k.AllowedModels)
			if err := tx.Create(&APIKey{
				KeyValue:       k.Key,
				Name:           k.Name,
				TenantName:     defaultStr(k.Tenant, "default"),
				BalanceMicros:  k.BalanceMicros,
				MaxRPM:         k.MaxRPM,
				MaxTPM:         k.MaxTPM,
				AllowedModels:  string(allowRaw),
				RateMultiplier: k.RateMultiplier,
				Enabled:        true,
			}).Error; err != nil {
				return err
			}
		}

		insertedDefaultTenant := false
		for _, t := range cfg.Tenants {
			if strings.TrimSpace(t.Name) == "" {
				continue
			}
			if t.Name == "default" {
				insertedDefaultTenant = true
			}
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "name"}},
				DoUpdates: clause.AssignmentColumns([]string{"enabled"}),
			}).Create(&Tenant{Name: t.Name, Enabled: t.Enabled}).Error; err != nil {
				return err
			}
		}
		if !insertedDefaultTenant {
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "name"}},
				DoUpdates: clause.AssignmentColumns([]string{"enabled"}),
			}).Create(&Tenant{Name: "default", Enabled: true}).Error; err != nil {
				return err
			}
		}

		for _, a := range cfg.Admins {
			if strings.TrimSpace(a.Username) == "" || strings.TrimSpace(a.Password) == "" {
				continue
			}
			hash := sha256Hex(a.Password)
			token := randomToken()
			role := normalizeRole(a.Role)
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "username"}},
				DoUpdates: clause.AssignmentColumns([]string{"password_hash", "role", "enabled"}),
			}).Create(&AdminUser{
				Username:     a.Username,
				PasswordHash: hash,
				Role:         role,
				Token:        token,
				Enabled:      true,
			}).Error; err != nil {
				return err
			}
		}

		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "model_prefix"}},
			DoUpdates: clause.AssignmentColumns([]string{"price_per1_m_micros", "enabled"}),
		}).Create(&PriceRule{
			ModelPrefix:      "*",
			PricePer1MMicros: cfg.Billing.DefaultPricePerMillionMicros,
			Enabled:          true,
		}).Error; err != nil {
			return err
		}

		for idx, r := range cfg.Rules {
			actionRaw, _ := json.Marshal(r.Action)
			cond := buildConditionJS(r)
			if err := tx.Create(&Rule{
				Name:        r.Name,
				Priority:    idx + 1,
				Enabled:     true,
				ConditionJS: cond,
				ActionJSON:  string(actionRaw),
			}).Error; err != nil {
				return err
			}
		}

		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "k"}},
			DoUpdates: clause.AssignmentColumns([]string{"v"}),
		}).Create(&KV{
			K: "seeded_at",
			V: time.Now().UTC().Format(time.RFC3339),
		}).Error
	})
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
	var out []Provider
	err := s.db.Order("name").Find(&out).Error
	return out, err
}

func (s *Store) UpsertProvider(p Provider) error {
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("provider name is required")
	}
	if strings.TrimSpace(p.BaseURL) == "" {
		return errors.New("provider base_url is required")
	}
	if p.Weight <= 0 {
		p.Weight = 1
	}
	if p.GroupName == "" {
		p.GroupName = "default"
	}
	if p.RecoverIntervalSec <= 0 {
		p.RecoverIntervalSec = 60
	}

	if p.ID > 0 {
		return s.db.Model(&Provider{}).Where("id = ?", p.ID).Updates(map[string]any{
			"name":                 p.Name,
			"base_url":             p.BaseURL,
			"weight":               p.Weight,
			"models_json":          p.ModelsJSON,
			"model_map_json":       p.ModelMapJSON,
			"max_rpm":              p.MaxRPM,
			"max_tpm":              p.MaxTPM,
			"enabled":              p.Enabled,
			"group_name":           p.GroupName,
			"recover_interval_sec": p.RecoverIntervalSec,
		}).Error
	}
	return s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{"base_url", "weight", "models_json", "model_map_json", "max_rpm", "max_tpm", "enabled", "group_name", "recover_interval_sec"}),
	}).Create(&p).Error
}

func (s *Store) DeleteProvider(name string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("provider_name = ?", name).Delete(&ProviderKey{}).Error; err != nil {
			return err
		}
		return tx.Where("name = ?", name).Delete(&Provider{}).Error
	})
}

func (s *Store) SetProviderEnabled(name string, enabled bool) error {
	return s.db.Model(&Provider{}).Where("name = ?", name).Update("enabled", enabled).Error
}

func (s *Store) ListProviderKeys(providerName string) ([]ProviderKey, error) {
	var out []ProviderKey
	q := s.db.Order("provider_name, id")
	if providerName != "" {
		q = q.Where("provider_name = ?", providerName)
	}
	err := q.Find(&out).Error
	return out, err
}

func (s *Store) UpsertProviderKey(k ProviderKey) (int64, error) {
	if strings.TrimSpace(k.ProviderName) == "" {
		return 0, errors.New("provider_name is required")
	}
	if strings.TrimSpace(k.Alias) == "" {
		return 0, errors.New("alias is required")
	}
	if strings.TrimSpace(k.APIKey) == "" {
		return 0, errors.New("api_key is required")
	}
	if k.ID > 0 {
		if err := s.db.Model(&ProviderKey{}).Where("id = ?", k.ID).Updates(map[string]any{
			"provider_name": k.ProviderName,
			"alias":         k.Alias,
			"api_key":       k.APIKey,
			"enabled":       k.Enabled,
		}).Error; err != nil {
			return 0, err
		}
		return k.ID, nil
	}
	if err := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "provider_name"}, {Name: "alias"}},
		DoUpdates: clause.AssignmentColumns([]string{"api_key", "enabled"}),
	}).Create(&k).Error; err != nil {
		return 0, err
	}
	return k.ID, nil
}

func (s *Store) DeleteProviderKey(id int64) error {
	return s.db.Where("id = ?", id).Delete(&ProviderKey{}).Error
}

func (s *Store) UpdateProviderKeyHealth(id int64, consecutiveErrs int, cooldownUntil int64, lastStatus string) error {
	return s.db.Model(&ProviderKey{}).Where("id = ?", id).Updates(map[string]any{
		"consecutive_errs": consecutiveErrs,
		"cooldown_until":   cooldownUntil,
		"last_status":      lastStatus,
	}).Error
}

func (s *Store) SetProviderKeyEnabled(id int64, enabled bool) error {
	return s.db.Model(&ProviderKey{}).Where("id = ?", id).Update("enabled", enabled).Error
}

func (s *Store) ListAPIKeys() ([]APIKey, error) {
	var out []APIKey
	err := s.db.Order("id").Find(&out).Error
	return out, err
}

func (s *Store) UpsertAPIKey(k APIKey) (int64, error) {
	if strings.TrimSpace(k.KeyValue) == "" {
		return 0, errors.New("key_value is required")
	}
	if strings.TrimSpace(k.Name) == "" {
		return 0, errors.New("name is required")
	}
	if k.RateMultiplier <= 0 {
		k.RateMultiplier = 1
	}

	if k.ID > 0 {
		if err := s.db.Model(&APIKey{}).Where("id = ?", k.ID).Updates(map[string]any{
			"key_value":       k.KeyValue,
			"name":            k.Name,
			"tenant_name":     defaultStr(k.TenantName, "default"),
			"balance_micros":  k.BalanceMicros,
			"max_rpm":         k.MaxRPM,
			"max_tpm":         k.MaxTPM,
			"allowed_models":  k.AllowedModels,
			"rate_multiplier": k.RateMultiplier,
			"enabled":         k.Enabled,
		}).Error; err != nil {
			return 0, err
		}
		return k.ID, nil
	}
	if k.TenantName == "" {
		k.TenantName = "default"
	}
	if err := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key_value"}},
		DoUpdates: clause.AssignmentColumns([]string{"name", "tenant_name", "balance_micros", "max_rpm", "max_tpm", "allowed_models", "rate_multiplier", "enabled"}),
	}).Create(&k).Error; err != nil {
		return 0, err
	}
	return k.ID, nil
}

func (s *Store) DeleteAPIKey(id int64) error {
	return s.db.Where("id = ?", id).Delete(&APIKey{}).Error
}

func (s *Store) FindAPIKeyByValue(key string) (APIKey, error) {
	var k APIKey
	err := s.db.Where("key_value = ?", key).First(&k).Error
	return k, err
}

func (s *Store) TopupAPIKeyBalance(key string, deltaMicros int64, reason, ref string) error {
	if deltaMicros == 0 {
		return nil
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&APIKey{}).Where("key_value = ?", key).Update("balance_micros", gorm.Expr("balance_micros + ?", deltaMicros)).Error; err != nil {
			return err
		}
		return tx.Create(&BillingTxn{
			APIKey:      key,
			DeltaMicros: deltaMicros,
			Reason:      defaultStr(reason, "topup"),
			Ref:         ref,
		}).Error
	})
}

func (s *Store) DeductAPIKeyBalance(key string, deltaMicros int64, reason, ref string) error {
	if deltaMicros <= 0 {
		return nil
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&APIKey{}).Where("key_value = ? AND balance_micros >= ?", key, deltaMicros).Update("balance_micros", gorm.Expr("balance_micros - ?", deltaMicros))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errors.New("insufficient balance")
		}
		return tx.Create(&BillingTxn{
			APIKey:      key,
			DeltaMicros: -deltaMicros,
			Reason:      defaultStr(reason, "usage"),
			Ref:         ref,
		}).Error
	})
}

func (s *Store) ListBillingTxns(apiKey string, limit int) ([]BillingTxn, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	var out []BillingTxn
	q := s.db.Order("id desc").Limit(limit)
	if apiKey != "" {
		q = q.Where("api_key = ?", apiKey)
	}
	err := q.Find(&out).Error
	return out, err
}

func (s *Store) ListRules() ([]Rule, error) {
	var out []Rule
	err := s.db.Order("priority asc, id asc").Find(&out).Error
	return out, err
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
		if err := s.db.Model(&Rule{}).Where("id = ?", r.ID).Updates(map[string]any{
			"name":         r.Name,
			"priority":     r.Priority,
			"enabled":      r.Enabled,
			"condition_js": r.ConditionJS,
			"action_json":  r.ActionJSON,
		}).Error; err != nil {
			return 0, err
		}
		return r.ID, nil
	}
	if err := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{"priority", "enabled", "condition_js", "action_json"}),
	}).Create(&r).Error; err != nil {
		return 0, err
	}
	return r.ID, nil
}

func (s *Store) DeleteRule(id int64) error {
	return s.db.Where("id = ?", id).Delete(&Rule{}).Error
}

func (s *Store) ListSchedules() ([]GroupSchedule, error) {
	var out []GroupSchedule
	err := s.db.Order("group_name asc, id asc").Find(&out).Error
	return out, err
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
		if err := s.db.Model(&GroupSchedule{}).Where("id = ?", sc.ID).Updates(map[string]any{
			"group_name":   sc.GroupName,
			"weekday_mask": sc.WeekdayMask,
			"start_hhmm":   sc.StartHHMM,
			"end_hhmm":     sc.EndHHMM,
			"multiplier":   sc.Multiplier,
			"enabled":      sc.Enabled,
		}).Error; err != nil {
			return 0, err
		}
		return sc.ID, nil
	}
	if err := s.db.Create(&sc).Error; err != nil {
		return 0, err
	}
	return sc.ID, nil
}

func (s *Store) DeleteSchedule(id int64) error {
	return s.db.Where("id = ?", id).Delete(&GroupSchedule{}).Error
}

func (s *Store) AddUsage(u UsageRecord) error {
	return s.db.Create(&u).Error
}

func (s *Store) UsageSummaryLast24h() (map[string]int64, error) {
	since := time.Now().Add(-24 * time.Hour).Unix()
	type row struct {
		APIKey string
		Total  int64
	}
	var rows []row
	if err := s.db.Model(&UsageRecord{}).
		Select("api_key, SUM(total_tokens) as total").
		Where("at_unix >= ?", since).
		Group("api_key").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(rows))
	for _, r := range rows {
		out[r.APIKey] = r.Total
	}
	return out, nil
}

func (s *Store) ExportRuntimeConfig() (RuntimeConfigExport, error) {
	providers, err := s.ListProviders()
	if err != nil {
		return RuntimeConfigExport{}, err
	}
	pkeys, err := s.ListProviderKeys("")
	if err != nil {
		return RuntimeConfigExport{}, err
	}
	apikeys, err := s.ListAPIKeys()
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
		ProviderKeys: pkeys,
		APIKeys:      apikeys,
		Rules:        rules,
		Schedules:    schedules,
	}, nil
}

func (s *Store) ReplaceRulesAndSchedules(rules []Rule, schedules []GroupSchedule) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("1 = 1").Delete(&Rule{}).Error; err != nil {
			return err
		}
		if len(rules) > 0 {
			for _, r := range rules {
				if strings.TrimSpace(r.Name) == "" || strings.TrimSpace(r.ConditionJS) == "" || strings.TrimSpace(r.ActionJSON) == "" {
					continue
				}
				if r.Priority <= 0 {
					r.Priority = 100
				}
				if err := tx.Create(&Rule{
					Name:        r.Name,
					Priority:    r.Priority,
					Enabled:     r.Enabled,
					ConditionJS: r.ConditionJS,
					ActionJSON:  r.ActionJSON,
				}).Error; err != nil {
					return err
				}
			}
		}
		if err := tx.Where("1 = 1").Delete(&GroupSchedule{}).Error; err != nil {
			return err
		}
		if len(schedules) > 0 {
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
				if err := tx.Create(&GroupSchedule{
					GroupName:   sc.GroupName,
					WeekdayMask: sc.WeekdayMask,
					StartHHMM:   sc.StartHHMM,
					EndHHMM:     sc.EndHHMM,
					Multiplier:  sc.Multiplier,
					Enabled:     sc.Enabled,
				}).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (s *Store) UpsertTenant(t Tenant) (int64, error) {
	if strings.TrimSpace(t.Name) == "" {
		return 0, errors.New("tenant name required")
	}
	if t.ID > 0 {
		if err := s.db.Model(&Tenant{}).Where("id = ?", t.ID).Updates(map[string]any{
			"name":    t.Name,
			"enabled": t.Enabled,
		}).Error; err != nil {
			return 0, err
		}
		return t.ID, nil
	}
	if err := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{"enabled"}),
	}).Create(&t).Error; err != nil {
		return 0, err
	}
	return t.ID, nil
}

func (s *Store) ListTenants() ([]Tenant, error) {
	var out []Tenant
	err := s.db.Order("name asc").Find(&out).Error
	return out, err
}

func (s *Store) DeleteTenant(id int64) error {
	return s.db.Where("id = ?", id).Delete(&Tenant{}).Error
}

func (s *Store) IsTenantEnabled(name string) bool {
	if name == "" {
		name = "default"
	}
	var t Tenant
	if err := s.db.Where("name = ?", name).First(&t).Error; err != nil {
		if name == "default" {
			return true
		}
		return false
	}
	return t.Enabled
}

func (s *Store) UpsertAdminUser(u AdminUser) (int64, error) {
	if strings.TrimSpace(u.Username) == "" {
		return 0, errors.New("username required")
	}
	u.Role = normalizeRole(u.Role)
	if u.ID > 0 {
		if err := s.db.Model(&AdminUser{}).Where("id = ?", u.ID).Updates(map[string]any{
			"username":      u.Username,
			"password_hash": u.PasswordHash,
			"role":          u.Role,
			"enabled":       u.Enabled,
		}).Error; err != nil {
			return 0, err
		}
		return u.ID, nil
	}
	if u.Token == "" {
		u.Token = randomToken()
	}
	if err := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "username"}},
		DoUpdates: clause.AssignmentColumns([]string{"password_hash", "role", "enabled"}),
	}).Create(&u).Error; err != nil {
		return 0, err
	}
	return u.ID, nil
}

func (s *Store) ListAdminUsers() ([]AdminUser, error) {
	var out []AdminUser
	err := s.db.Order("id asc").Find(&out).Error
	return out, err
}

func (s *Store) AdminLogin(username, password string) (AdminUser, error) {
	var u AdminUser
	if err := s.db.Where("username = ? AND enabled = ?", username, true).First(&u).Error; err != nil {
		return AdminUser{}, err
	}
	if u.PasswordHash != sha256Hex(password) {
		return AdminUser{}, errors.New("invalid username or password")
	}
	u.Token = randomToken()
	if err := s.db.Model(&AdminUser{}).Where("id = ?", u.ID).Update("token", u.Token).Error; err != nil {
		return AdminUser{}, err
	}
	return u, nil
}

func (s *Store) VerifyAdminToken(token string) (AdminUser, error) {
	var u AdminUser
	err := s.db.Where("token = ? AND enabled = ?", token, true).First(&u).Error
	return u, err
}

func (s *Store) SetAdminEnabled(id int64, enabled bool) error {
	return s.db.Model(&AdminUser{}).Where("id = ?", id).Update("enabled", enabled).Error
}

func (s *Store) UpsertPriceRule(p PriceRule) (int64, error) {
	if strings.TrimSpace(p.ModelPrefix) == "" {
		return 0, errors.New("model_prefix required")
	}
	if p.ID > 0 {
		if err := s.db.Model(&PriceRule{}).Where("id = ?", p.ID).Updates(map[string]any{
			"model_prefix":        p.ModelPrefix,
			"price_per1_m_micros": p.PricePer1MMicros,
			"enabled":             p.Enabled,
		}).Error; err != nil {
			return 0, err
		}
		return p.ID, nil
	}
	if err := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "model_prefix"}},
		DoUpdates: clause.AssignmentColumns([]string{"price_per1_m_micros", "enabled"}),
	}).Create(&p).Error; err != nil {
		return 0, err
	}
	return p.ID, nil
}

func (s *Store) ListPriceRules() ([]PriceRule, error) {
	var out []PriceRule
	err := s.db.Order("length(model_prefix) desc").Find(&out).Error
	return out, err
}

func (s *Store) DeletePriceRule(id int64) error {
	return s.db.Where("id = ?", id).Delete(&PriceRule{}).Error
}

func (s *Store) ResolveModelPriceMicros(model string, fallback int64) int64 {
	var items []PriceRule
	if err := s.db.Where("enabled = ?", true).Order("length(model_prefix) desc").Find(&items).Error; err != nil {
		return fallback
	}
	for _, it := range items {
		if it.ModelPrefix == "*" || strings.HasPrefix(model, it.ModelPrefix) {
			return it.PricePer1MMicros
		}
	}
	return fallback
}

func (s *Store) AddAudit(actor, action, target string, detail any) {
	raw := "{}"
	if detail != nil {
		if b, err := json.Marshal(detail); err == nil {
			raw = string(b)
		}
	}
	_ = s.db.Create(&AuditLog{
		Actor:      actor,
		Action:     action,
		Target:     target,
		DetailJSON: raw,
	}).Error
}

func (s *Store) ListAudit(limit int) ([]AuditLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var out []AuditLog
	err := s.db.Order("id desc").Limit(limit).Find(&out).Error
	return out, err
}

func sha256Hex(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])
}

func randomToken() string {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("tok_%d", time.Now().UnixNano())
	}
	return "adm_" + hex.EncodeToString(b)
}

func normalizeRole(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "owner":
		return "owner"
	case "admin":
		return "admin"
	default:
		return "viewer"
	}
}

func defaultStr(v, d string) string {
	if strings.TrimSpace(v) == "" {
		return d
	}
	return v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

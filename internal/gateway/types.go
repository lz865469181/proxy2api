package gateway

import (
	"math/rand"
	"strings"
	"sync/atomic"
	"time"
)

type runtimeSnapshot struct {
	providers map[string]providerState
	keys      map[string]gatewayKey
	rules     []ruleState
	schedules []groupSchedule
	priceDefaultMicros int64
}

type providerState struct {
	Name            string
	BaseURL         string
	Weight          int
	Models          []string
	ModelMap        map[string]string
	MaxRPM          int
	MaxTPM          int
	Enabled         bool
	GroupName       string
	RecoverInterval time.Duration
	Keys            []providerKeyState
}

type providerKeyState struct {
	ID              int64
	Alias           string
	APIKey          string
	Enabled         bool
	ConsecutiveErrs int
	CooldownUntil   time.Time
	LastStatus      string
}

type gatewayKey struct {
	Key            string
	Name           string
	TenantName     string
	BalanceMicros  int64
	MaxRPM         int
	MaxTPM         int
	AllowedModels  []string
	RateMultiplier float64
	Enabled        bool
}

type ruleState struct {
	ID        int64
	Name      string
	Priority  int
	Enabled   bool
	Condition string
	Action    ruleAction
}

type ruleAction struct {
	ForceProvider  string  `json:"force_provider"`
	Deny           bool    `json:"deny"`
	RateMultiplier float64 `json:"rate_multiplier"`
}

type groupSchedule struct {
	GroupName   string
	WeekdayMask map[time.Weekday]bool
	StartMinute int
	EndMinute   int
	Multiplier  float64
	Enabled     bool
}

type stickyBind struct {
	Provider string
	Updated  time.Time
}

type parsedRequest struct {
	Model          string `json:"model"`
	EstimatedToken int
}

type atomicSnapshot struct {
	v atomic.Value
}

func (a *atomicSnapshot) Set(s runtimeSnapshot) {
	a.v.Store(s)
}

func (a *atomicSnapshot) Get() runtimeSnapshot {
	v := a.v.Load()
	if v == nil {
		return runtimeSnapshot{
			providers: map[string]providerState{},
			keys:      map[string]gatewayKey{},
		}
	}
	return v.(runtimeSnapshot)
}

func supportsModel(models []string, model string) bool {
	if len(models) == 0 {
		return true
	}
	for _, m := range models {
		if m == model {
			return true
		}
	}
	return false
}

func weightedProviderPick(items []providerState) providerState {
	if len(items) == 1 {
		return items[0]
	}
	total := 0
	for _, it := range items {
		if it.Weight <= 0 {
			total++
			continue
		}
		total += it.Weight
	}
	if total <= 0 {
		return items[rand.Intn(len(items))]
	}
	r := rand.Intn(total)
	acc := 0
	for _, it := range items {
		w := it.Weight
		if w <= 0 {
			w = 1
		}
		acc += w
		if r < acc {
			return it
		}
	}
	return items[len(items)-1]
}

func parseWeekdayMask(mask string) map[time.Weekday]bool {
	out := map[time.Weekday]bool{}
	parts := strings.Split(mask, ",")
	for _, p := range parts {
		switch strings.TrimSpace(p) {
		case "1":
			out[time.Monday] = true
		case "2":
			out[time.Tuesday] = true
		case "3":
			out[time.Wednesday] = true
		case "4":
			out[time.Thursday] = true
		case "5":
			out[time.Friday] = true
		case "6":
			out[time.Saturday] = true
		case "7":
			out[time.Sunday] = true
		}
	}
	return out
}

func parseHHMM(s string) int {
	if len(s) != 5 {
		return 0
	}
	h := int(s[0]-'0')*10 + int(s[1]-'0')
	m := int(s[3]-'0')*10 + int(s[4]-'0')
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0
	}
	return h*60 + m
}

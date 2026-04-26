package gateway

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"

	"proxy2api/internal/config"
)

type redisLimiter struct {
	client    *redis.Client
	keyPrefix string
	script    *redis.Script
}

func newRedisLimiter(cfg config.RedisConfig) (*redisLimiter, error) {
	opt := &redis.Options{
		Addr:        cfg.Addr,
		Password:    cfg.Password,
		DB:          cfg.DB,
		DialTimeout: time.Duration(cfg.DialTimeout) * time.Second,
		ReadTimeout: time.Duration(cfg.ReadTimeout) * time.Second,
	}
	client := redis.NewClient(opt)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	return &redisLimiter{
		client:    client,
		keyPrefix: cfg.KeyPrefix,
		script: redis.NewScript(`
local req_key = KEYS[1]
local tok_key = KEYS[2]
local req_delta = tonumber(ARGV[1])
local tok_delta = tonumber(ARGV[2])
local max_req = tonumber(ARGV[3])
local max_tok = tonumber(ARGV[4])
local ttl = tonumber(ARGV[5])

local req_cur = tonumber(redis.call("GET", req_key) or "0")
local tok_cur = tonumber(redis.call("GET", tok_key) or "0")

if max_req > 0 and (req_cur + req_delta) > max_req then
  return 0
end
if max_tok > 0 and (tok_cur + tok_delta) > max_tok then
  return 0
end

if req_delta > 0 then
  redis.call("INCRBY", req_key, req_delta)
  redis.call("EXPIRE", req_key, ttl)
end
if tok_delta > 0 then
  redis.call("INCRBY", tok_key, tok_delta)
  redis.call("EXPIRE", tok_key, ttl)
end
return 1
`),
	}, nil
}

func (l *redisLimiter) Allow(key string, reqDelta, tokDelta, maxReq, maxTok int) bool {
	if maxReq <= 0 && maxTok <= 0 {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	minuteWindow := time.Now().UTC().Format("200601021504")
	base := l.keyPrefix + key + ":" + minuteWindow
	keys := []string{base + ":req", base + ":tok"}
	res, err := l.script.Run(ctx, l.client, keys, reqDelta, tokDelta, maxReq, maxTok, 65).Int()
	if err != nil {
		return false
	}
	return res == 1
}

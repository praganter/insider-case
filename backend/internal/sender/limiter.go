package sender

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// Limiter is a token bucket per channel kept in Redis, so every worker process
// shares one budget. Burst 1 spaces messages evenly (100/s means one every
// 10 ms), which keeps any one-second window at or below the rate.
type Limiter struct {
	Redis *redis.Client
	Rate  int // messages per second per channel
	Burst int
}

// The script reads Redis' own clock, so worker clock skew cannot matter. It
// returns 0 when a token was taken, otherwise the milliseconds to wait.
var take = redis.NewScript(`
local rate = tonumber(ARGV[1])
local burst = tonumber(ARGV[2])
local t = redis.call('TIME')
local now = t[1] * 1000 + math.floor(t[2] / 1000)
local s = redis.call('HMGET', KEYS[1], 'tokens', 'ts')
local tokens = tonumber(s[1])
local ts = tonumber(s[2])
if tokens == nil then tokens = burst; ts = now end
tokens = math.min(burst, tokens + (now - ts) * rate / 1000)
local wait = 0
if tokens >= 1 then tokens = tokens - 1 else wait = math.ceil((1 - tokens) * 1000 / rate) end
redis.call('HSET', KEYS[1], 'tokens', tostring(tokens), 'ts', now)
redis.call('PEXPIRE', KEYS[1], 10000)
return wait
`)

// Wait blocks until the channel has a token for this message.
func (l *Limiter) Wait(ctx context.Context, channel string) error {
	for {
		wait, err := take.Run(ctx, l.Redis, []string{"ratelimit:" + channel}, l.Rate, l.Burst).Int()
		if err != nil {
			return err
		}
		if wait == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(wait) * time.Millisecond):
		}
	}
}

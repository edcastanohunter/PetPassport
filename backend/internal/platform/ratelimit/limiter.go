package ratelimit

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/redis/go-redis/v9"

	ppredis "github.com/edcastanohunter/petpassport/backend/internal/platform/redis"
)

var bypassMetric = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "petpassport_ratelimit_bypass_total",
		Help: "Total de solicitudes que omitieron rate limit por fallo o circuit breaker de Redis (fail-open)",
	},
	[]string{"route_group", "reason"},
)

// Lua script para sliding window atómica.
// KEYS[1]: clave Redis (ej: pp:rl:user:123:commands)
// ARGV[1]: timestamp actual en milisegundos
// ARGV[2]: ventana en milisegundos
// ARGV[3]: límite máximo de solicitudes
// ARGV[4]: identificador único de la solicitud
// Retorna: {allowed (1/0), remaining, resetSeconds}
const slidingWindowLua = `
local key = KEYS[1]
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local clearBefore = now - window

redis.call('ZREMRANGEBYSCORE', key, '-inf', clearBefore)
local current = redis.call('ZCARD', key)

local resetSeconds = math.ceil(window / 1000)
if current < limit then
    redis.call('ZADD', key, now, ARGV[4])
    redis.call('EXPIRE', key, resetSeconds + 1)
    return {1, limit - current - 1, resetSeconds}
else
    local oldest = redis.call('ZRANGE', key, 0, 0, 'WITHSCORES')
    if #oldest >= 2 then
        local oldestTime = tonumber(oldest[2])
        resetSeconds = math.max(1, math.ceil((oldestTime + window - now) / 1000))
    end
    return {0, 0, resetSeconds}
end
`

// Result representa el resultado de la comprobación de rate limit.
type Result struct {
	Allowed            bool
	Limit              int64
	Remaining          int64
	ResetSeconds       int64
	RetryAfterSeconds  int64
	RouteGroup         string
}

// Limiter ejecuta la comprobación de rate limit con fail-open.
type Limiter struct {
	client  *ppredis.Client
	luaHash string
}

// NewLimiter construye un Limiter con el cliente Redis.
func NewLimiter(client *ppredis.Client) *Limiter {
	l := &Limiter{client: client}
	return l
}

// Check comprueba si la clave dada excede el límite en la ventana indicada.
// Si Redis falla o el circuit breaker está abierto, aplica fail-open: permite la petición
// e incrementa la métrica petpassport_ratelimit_bypass_total.
func (l *Limiter) Check(ctx context.Context, key string, routeGroup string, limit int64, window time.Duration) (Result, error) {
	defaultReset := int64(math.Ceil(window.Seconds()))
	if l.client == nil {
		bypassMetric.WithLabelValues(routeGroup, "nil_client").Inc()
		return Result{
			Allowed:      true,
			Limit:        limit,
			Remaining:    limit,
			ResetSeconds: defaultReset,
			RouteGroup:   routeGroup,
		}, nil
	}

	nowMs := time.Now().UnixMilli()
	windowMs := window.Milliseconds()
	reqID := uuid.New().String()

	var res []interface{}
	err := l.client.Do("ratelimit", func(rc *redis.Client) error {
		script := redis.NewScript(slidingWindowLua)
		r, err := script.Run(ctx, rc, []string{key}, nowMs, windowMs, limit, reqID).Result()
		if err != nil {
			return err
		}
		if slice, ok := r.([]interface{}); ok {
			res = slice
		} else {
			return fmt.Errorf("unexpected lua result format: %T", r)
		}
		return nil
	})

	if err != nil {
		// Fail-open: si Redis falla o el breaker está abierto, permitir
		reason := "error"
		if l.client.Breaker().State() != ppredis.StateClosed {
			reason = "breaker_open"
		}
		bypassMetric.WithLabelValues(routeGroup, reason).Inc()
		return Result{
			Allowed:      true,
			Limit:        limit,
			Remaining:    limit,
			ResetSeconds: defaultReset,
			RouteGroup:   routeGroup,
		}, nil
	}

	if len(res) < 3 {
		return Result{
			Allowed:      true,
			Limit:        limit,
			Remaining:    limit,
			ResetSeconds: defaultReset,
			RouteGroup:   routeGroup,
		}, nil
	}

	allowedInt, _ := toInt64(res[0])
	remaining, _ := toInt64(res[1])
	resetSec, _ := toInt64(res[2])

	allowed := allowedInt == 1
	var retryAfter int64
	if !allowed {
		retryAfter = resetSec
		if retryAfter <= 0 {
			retryAfter = 1
		}
	}

	return Result{
		Allowed:           allowed,
		Limit:             limit,
		Remaining:         remaining,
		ResetSeconds:      resetSec,
		RetryAfterSeconds: retryAfter,
		RouteGroup:        routeGroup,
	}, nil
}

func toInt64(v interface{}) (int64, error) {
	switch n := v.(type) {
	case int64:
		return n, nil
	case int:
		return int64(n), nil
	case string:
		return strconv.ParseInt(n, 10, 64)
	default:
		return 0, fmt.Errorf("unknown type: %T", v)
	}
}

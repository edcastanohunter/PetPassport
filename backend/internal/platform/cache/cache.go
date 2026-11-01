package cache

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	goredis "github.com/redis/go-redis/v9"
	"github.com/vmihailenco/msgpack/v5"
	"golang.org/x/sync/singleflight"

	platformredis "github.com/edcastanohunter/petpassport/backend/internal/platform/redis"
)

var (
	// ErrCacheMiss se devuelve cuando la clave no existe en la caché.
	ErrCacheMiss = errors.New("cache: key not found")

	metricCacheRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "petpassport",
		Subsystem: "cache",
		Name:      "requests_total",
		Help:      "Total de peticiones a la caché por familia de clave y resultado (hit_l1, hit_redis, miss, bypass, error).",
	}, []string{"key_family", "result"})
)

// ChannelInvalidate es el canal Pub/Sub para invalidar cachés L1 entre réplicas (ARQ-006).
const ChannelInvalidate = "pp:cache:invalidate"

// SchemaVersion es el prefijo de versión de esquema para el versionado de claves en Redis.
const SchemaVersion = "pp:v1"

// KeyFamily extrae la familia de la clave (ej. "membership", "petaccess", "pet") para métricas.
func KeyFamily(key string) string {
	parts := strings.Split(key, ":")
	if len(parts) >= 3 && parts[0] == "pp" {
		return parts[2]
	}
	return "other"
}

// L1Entry almacena un valor en la caché en memoria L1.
type L1Entry struct {
	value     []byte
	expiresAt time.Time
}

// L1Cache es una caché en memoria acotada con TTL corto (5 s) para membership y petaccess (ARQ-006).
type L1Cache struct {
	mu         sync.RWMutex
	entries    map[string]L1Entry
	ttl        time.Duration
	maxEntries int
}

const defaultMaxL1Entries = 10000

// NewL1Cache crea una nueva caché L1 en memoria acotada.
func NewL1Cache(ttl time.Duration) *L1Cache {
	if ttl <= 0 {
		ttl = 5 * time.Second
	}
	return &L1Cache{
		entries:    make(map[string]L1Entry),
		ttl:        ttl,
		maxEntries: defaultMaxL1Entries,
	}
}

// Get obtiene el valor de L1 si no ha expirado.
func (l *L1Cache) Get(key string) ([]byte, bool) {
	l.mu.RLock()
	entry, ok := l.entries[key]
	l.mu.RUnlock()

	if !ok {
		return nil, false
	}
	if time.Now().After(entry.expiresAt) {
		l.mu.Lock()
		delete(l.entries, key)
		l.mu.Unlock()
		return nil, false
	}
	return entry.value, true
}

// Set guarda un valor en L1 controlando el tamaño máximo para evitar fugas de memoria.
func (l *L1Cache) Set(key string, val []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.entries) >= l.maxEntries {
		now := time.Now()
		// Purgar claves expiradas primero
		for k, v := range l.entries {
			if now.After(v.expiresAt) {
				delete(l.entries, k)
			}
		}
		// Si todavía excede el límite, desalojar claves para mantener la capacidad acotada
		if len(l.entries) >= l.maxEntries {
			for k := range l.entries {
				delete(l.entries, k)
				break
			}
		}
	}

	l.entries[key] = L1Entry{
		value:     val,
		expiresAt: time.Now().Add(l.ttl),
	}
}

// Delete elimina una clave de L1.
func (l *L1Cache) Delete(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

// Clear vacía toda la caché L1.
func (l *L1Cache) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = make(map[string]L1Entry)
}

// Cache gestiona la caché distribuida L2 (Redis) + L1 (memoria) con single-flight e invalidación.
type Cache struct {
	redisClient *platformredis.Client
	l1          *L1Cache
	sf          singleflight.Group
}

// NewCache construye un gestor de caché.
func NewCache(rClient *platformredis.Client) *Cache {
	return &Cache{
		redisClient: rClient,
		l1:          NewL1Cache(5 * time.Second),
	}
}

// New es un alias de NewCache.
func New(rClient *platformredis.Client) *Cache {
	return NewCache(rClient)
}

// L1 devuelve la caché en memoria L1.
func (c *Cache) L1() *L1Cache {
	return c.l1
}

// Get recupera un valor de L1 o Redis y lo deserializa con msgpack.
func (c *Cache) Get(ctx context.Context, key string, dest any) error {
	family := KeyFamily(key)

	// 1. Consultar L1 (solo si está disponible)
	if c.l1 != nil {
		if raw, ok := c.l1.Get(key); ok {
			metricCacheRequests.WithLabelValues(family, "hit_l1").Inc()
			return msgpack.Unmarshal(raw, dest)
		}
	}

	// 2. Si el cliente Redis no está configurado, fallo silencioso a miss
	if c.redisClient == nil {
		metricCacheRequests.WithLabelValues(family, "bypass").Inc()
		return ErrCacheMiss
	}

	// 3. Consultar Redis protegido por circuit breaker
	var raw []byte
	err := c.redisClient.Do("cache_get", func(rdb *goredis.Client) error {
		val, getErr := rdb.Get(ctx, key).Bytes()
		if getErr != nil {
			return getErr
		}
		raw = val
		return nil
	})

	if err != nil {
		if errors.Is(err, goredis.Nil) {
			metricCacheRequests.WithLabelValues(family, "miss").Inc()
			return ErrCacheMiss
		}
		if errors.Is(err, platformredis.ErrCircuitOpen) {
			metricCacheRequests.WithLabelValues(family, "bypass").Inc()
		} else {
			metricCacheRequests.WithLabelValues(family, "error").Inc()
		}
		// Fail-open: Redis no disponible se trata como miss para no romper al usuario (ARQ-005)
		return ErrCacheMiss
	}

	metricCacheRequests.WithLabelValues(family, "hit_redis").Inc()

	// Guardar en L1 para la siguiente petición dentro del TTL de 5 s
	if c.l1 != nil {
		c.l1.Set(key, raw)
	}

	return msgpack.Unmarshal(raw, dest)
}

// Set serializa con msgpack y almacena un valor en Redis con el TTL indicado.
func (c *Cache) Set(ctx context.Context, key string, val any, ttl time.Duration) error {
	family := KeyFamily(key)
	raw, err := msgpack.Marshal(val)
	if err != nil {
		return fmt.Errorf("cache: marshal msgpack failed: %w", err)
	}

	// Guardar en L1
	if c.l1 != nil {
		c.l1.Set(key, raw)
	}

	if c.redisClient == nil {
		return nil
	}

	err = c.redisClient.Do("cache_set", func(rdb *goredis.Client) error {
		return rdb.Set(ctx, key, raw, ttl).Err()
	})
	if err != nil {
		metricCacheRequests.WithLabelValues(family, "error").Inc()
		// Fail-open: error de escritura en Redis no bloquea la operación principal
		return nil
	}

	return nil
}

// Delete elimina una o más claves de L1 y de Redis.
func (c *Cache) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	for _, k := range keys {
		if c.l1 != nil {
			c.l1.Delete(k)
		}
	}

	if c.redisClient == nil {
		return nil
	}

	return c.redisClient.Do("cache_del", func(rdb *goredis.Client) error {
		return rdb.Del(ctx, keys...).Err()
	})
}

// Invalidate publica la clave invalidada en Pub/Sub y la borra de Redis y L1 local.
func (c *Cache) Invalidate(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	_ = c.Delete(ctx, keys...)

	if c.redisClient == nil {
		return nil
	}

	for _, k := range keys {
		_ = c.redisClient.Do("cache_publish", func(rdb *goredis.Client) error {
			return rdb.Publish(ctx, ChannelInvalidate, k).Err()
		})
	}
	return nil
}

// SubscribeInvalidations escucha el canal Pub/Sub para invalidar L1 cuando otra réplica borra una clave.
// Maneja reconexión automática ante cortes de Redis y vacía L1 al reconectar para evitar stale reads.
func (c *Cache) SubscribeInvalidations(ctx context.Context) error {
	if c.redisClient == nil || c.l1 == nil {
		return nil
	}

	go func() {
		backoff := 1 * time.Second
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			rdb := c.redisClient.RDB()
			pubsub := rdb.Subscribe(ctx, ChannelInvalidate)
			ch := pubsub.Channel()

			// Al (re)conectar, vaciar L1 para no mantener entradas obsoletas perdidas durante la desconexión
			c.l1.Clear()

			disconnected := false
			for !disconnected {
				select {
				case <-ctx.Done():
					_ = pubsub.Close()
					return
				case msg, ok := <-ch:
					if !ok {
						disconnected = true
						break
					}
					if msg != nil && msg.Payload != "" {
						c.l1.Delete(msg.Payload)
					}
				}
			}

			_ = pubsub.Close()

			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
				if backoff < 30*time.Second {
					backoff *= 2
				}
			}
		}
	}()

	return nil
}

// Fetch utiliza singleflight para cargar el dato de la fuente en caso de cache miss, evitando estampidas.
func (c *Cache) Fetch(ctx context.Context, key string, ttl time.Duration, dest any, loadFn func(ctx context.Context) (any, error)) error {
	err := c.Get(ctx, key, dest)
	if err == nil {
		return nil // Cache hit
	}
	if !errors.Is(err, ErrCacheMiss) {
		// En caso de otro error no recuperable
		return err
	}

	// Cache miss: singleflight call con contexto desacoplado de cancelaciones individuales de clientes
	val, err, _ := c.sf.Do(key, func() (any, error) {
		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()

		loaded, loadErr := loadFn(loadCtx)
		if loadErr != nil {
			return nil, loadErr
		}
		_ = c.Set(ctx, key, loaded, ttl)
		return loaded, nil
	})

	if err != nil {
		return err
	}

	// Copiar al destino
	raw, marshalErr := msgpack.Marshal(val)
	if marshalErr != nil {
		return marshalErr
	}
	return msgpack.Unmarshal(raw, dest)
}

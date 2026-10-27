// Package redisx：Redis 客户端初始化（工单11/15/17）
// 设计要点：启动时探测 Redis 可用性；不可用时全局降级为内存实现（MemStore），
// 保证本地无 Redis 也能完整运行；上层通过 Store 接口访问，不感知具体实现。
package redisx

import (
	"context"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Store 司机状态/简单KV 抽象（工单15/17：司机在线状态存储）
type Store interface {
	Set(ctx context.Context, key, val string, ttl time.Duration) error
	Get(ctx context.Context, key string) (string, error)
	Del(ctx context.Context, key string) error
	SetNX(ctx context.Context, key, val string, ttl time.Duration) (bool, error)
	Available() bool
}

type redisStore struct{ rdb *redis.Client }

func (s *redisStore) Set(ctx context.Context, k, v string, ttl time.Duration) error {
	return s.rdb.Set(ctx, k, v, ttl).Err()
}
func (s *redisStore) Get(ctx context.Context, k string) (string, error) {
	return s.rdb.Get(ctx, k).Result()
}
func (s *redisStore) Del(ctx context.Context, k string) error { return s.rdb.Del(ctx, k).Err() }
func (s *redisStore) SetNX(ctx context.Context, k, v string, ttl time.Duration) (bool, error) {
	return s.rdb.SetNX(ctx, k, v, ttl).Result()
}
func (s *redisStore) Available() bool { return true }

type memStore struct {
	mu   sync.RWMutex
	data map[string]memVal
}

type memVal struct {
	v    string
	dead time.Time
}

func (m *memStore) Set(_ context.Context, k, v string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	dead := time.Time{}
	if ttl > 0 {
		dead = time.Now().Add(ttl)
	}
	m.data[k] = memVal{v, dead}
	return nil
}
func (m *memStore) Get(_ context.Context, k string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	it, ok := m.data[k]
	if !ok || (!it.dead.IsZero() && time.Now().After(it.dead)) {
		return "", ErrNotFound
	}
	return it.v, nil
}
func (m *memStore) Del(_ context.Context, k string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, k)
	return nil
}
func (m *memStore) SetNX(_ context.Context, k, v string, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if it, ok := m.data[k]; ok && (it.dead.IsZero() || time.Now().Before(it.dead)) {
		return false, nil
	}
	dead := time.Time{}
	if ttl > 0 {
		dead = time.Now().Add(ttl)
	}
	m.data[k] = memVal{v, dead}
	return true, nil
}
func (m *memStore) Available() bool { return false }

// ErrNotFound 键不存在
var ErrNotFound = errNotFound{}

type errNotFound struct{}

func (errNotFound) Error() string { return "key not found" }

// RDB 全局 Redis 客户端（Available()==false 时为 nil）
var (
	RDB   *redis.Client
	store Store
	once  sync.Once
)

// Init 初始化：REDIS_ADDR（默认 localhost:6379），探测失败降级内存
func Init() Store {
	once.Do(func() {
		addr := os.Getenv("REDIS_ADDR")
		if addr == "" {
			addr = "localhost:6379"
		}
		rdb := redis.NewClient(&redis.Options{Addr: addr, DialTimeout: 500 * time.Millisecond})
		ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
		defer cancel()
		if err := rdb.Ping(ctx).Err(); err != nil {
			log.Printf("[redisx] Redis(%s) 不可用: %v —— 降级为内存实现(内存GEO/内存事件总线)", addr, strings.TrimSpace(err.Error()))
			store = &memStore{data: map[string]memVal{}}
			return
		}
		log.Printf("[redisx] Redis(%s) 已连接", addr)
		RDB = rdb
		store = &redisStore{rdb: rdb}
	})
	return store
}

// S 获取全局 Store
func S() Store {
	if store == nil {
		Init()
	}
	return store
}

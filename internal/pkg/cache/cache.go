// 工单：全栈开发-网约车-07-营销管理
// 缓存机制：对于频繁访问但不常变动的数据（如优惠券列表）使用缓存减少数据库查询压力。
// 采用接口化设计，生产环境可无缝替换为 Redis 实现。
package cache

import (
	"sync"
	"time"
)

// Cache 缓存接口（可替换 Redis 实现）
type Cache interface {
	Get(key string) (interface{}, bool)
	Set(key string, val interface{}, ttl time.Duration)
	Delete(key string)
}

type item struct {
	val      interface{}
	expireAt time.Time
}

// MemoryCache 基于 sync.Map 的进程内 TTL 缓存
type MemoryCache struct {
	m sync.Map
}

func NewMemoryCache() *MemoryCache {
	return &MemoryCache{}
}

func (c *MemoryCache) Get(key string) (interface{}, bool) {
	v, ok := c.m.Load(key)
	if !ok {
		return nil, false
	}
	it := v.(*item)
	if time.Now().After(it.expireAt) {
		c.m.Delete(key)
		return nil, false
	}
	return it.val, true
}

func (c *MemoryCache) Set(key string, val interface{}, ttl time.Duration) {
	c.m.Store(key, &item{val: val, expireAt: time.Now().Add(ttl)})
}

func (c *MemoryCache) Delete(key string) {
	c.m.Delete(key)
}

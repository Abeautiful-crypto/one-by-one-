// 工单09：多数据源工厂（GVA 多数据源路由能力）
// 提供主库 + 从库连接管理，以及基于 context 的数据源路由（读写分离 / 多租户预留）。
package datasource

import (
	"context"
	"sync"

	"gorm.io/gorm"
)

// CtxKey 上下文键
type ctxKey string

const dsKey ctxKey = "datasource"

// Manager 多数据源管理器（单例，工厂模式）
type Manager struct {
	mu       sync.RWMutex
	primary  *gorm.DB
	replicas []*gorm.DB
	rr       int
}

var defaultMgr = &Manager{}

// Init 注册主库与若干从库（工单09：主从切换 / 读写分离基础）
func Init(primary *gorm.DB, replicas ...*gorm.DB) {
	defaultMgr.mu.Lock()
	defaultMgr.primary = primary
	defaultMgr.replicas = replicas
	defaultMgr.mu.Unlock()
}

// Primary 主库（写）
func Primary() *gorm.DB {
	defaultMgr.mu.RLock()
	defer defaultMgr.mu.RUnlock()
	return defaultMgr.primary
}

// Replica 读库（从库轮询；无从库时回落主库，保证可用性）
func Replica() *gorm.DB {
	defaultMgr.mu.RLock()
	defer defaultMgr.mu.RUnlock()
	if len(defaultMgr.replicas) == 0 {
		return defaultMgr.primary
	}
	db := defaultMgr.replicas[defaultMgr.rr%len(defaultMgr.replicas)]
	defaultMgr.rr++
	return db
}

// WithDB 将指定 DB 注入 context（多租户 / 数据源透传）
func WithDB(ctx context.Context, db *gorm.DB) context.Context {
	return context.WithValue(ctx, dsKey, db)
}

// FromCtx 从 context 取数据源，缺省回落主库
func FromCtx(ctx context.Context) *gorm.DB {
	if db, ok := ctx.Value(dsKey).(*gorm.DB); ok && db != nil {
		return db
	}
	return Primary()
}

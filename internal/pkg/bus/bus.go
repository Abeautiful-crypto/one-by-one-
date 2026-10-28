// Package bus：轻量事件总线（工单11：Redis Stream 构建事件总线；工单15/17：订单广播给附近司机）
// Redis 可用 → Redis Stream（XADD/XREAD）；降级 → 内存 channel 广播。订阅回调均异步安全。
package bus

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"ride-hailing/internal/pkg/redisx"

	"github.com/redis/go-redis/v9"
)

// Event 事件结构（跨模块统一信封）
type Event struct {
	Type      string          `json:"type"`                // 如 order.created / order.accepted / driver.status
	Payload   json.RawMessage `json:"payload"`             // 业务负载
	Timestamp int64           `json:"timestamp"`           // 秒级时间戳
}

// Handler 订阅回调
type Handler func(e Event)

// Bus 事件总线抽象
type Bus interface {
	Publish(ctx context.Context, e Event) error
	Subscribe(types []string, h Handler) // types 为空表示全部
	Available() bool
}

const streamKey = "ride:events" // Redis Stream key

type redisBus struct {
	rdb *redis.Client
	mu  sync.RWMutex
	subs []sub
}

type sub struct {
	types map[string]bool
	h     Handler
}

func (b *redisBus) Publish(ctx context.Context, e Event) error {
	if e.Timestamp == 0 {
		e.Timestamp = time.Now().Unix()
	}
	raw, _ := json.Marshal(e)
	return b.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		Values: map[string]interface{}{"data": string(raw)},
	}).Err()
}

func (b *redisBus) Subscribe(types []string, h Handler) {
	m := map[string]bool{}
	for _, t := range types {
		m[t] = true
	}
	b.mu.Lock()
	b.subs = append(b.subs, sub{types: m, h: h})
	b.mu.Unlock()
}

func (b *redisBus) Available() bool { return true }

// Start 启动 Stream 消费循环（XREAD BLOCK），从最后一条开始读
func (b *redisBus) Start() {
	go func() {
		lastID := "$"
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			res, err := b.rdb.XRead(ctx, &redis.XReadArgs{
				Streams: []string{streamKey, lastID},
				Block:   3 * time.Second,
				Count:   100,
			}).Result()
			cancel()
			if err == redis.Nil || len(res) == 0 {
				continue
			}
			if err != nil {
				time.Sleep(time.Second)
				continue
			}
			for _, s := range res {
				for _, msg := range s.Messages {
					lastID = msg.ID
					raw := msg.Values["data"].(string)
					var e Event
					if json.Unmarshal([]byte(raw), &e) == nil {
						b.dispatch(e)
					}
				}
			}
		}
	}()
}

func (b *redisBus) dispatch(e Event) {
	b.mu.RLock()
	subs := append([]sub(nil), b.subs...)
	b.mu.RUnlock()
	for _, s := range subs {
		if len(s.types) == 0 || s.types[e.Type] {
			func() {
				defer func() { _ = recover() }() // 单订阅者 panic 不影响总线
				s.h(e)
			}()
		}
	}
}

// ---- 内存降级实现 ----

type memBus struct {
	mu   sync.RWMutex
	subs []sub
}

func (m *memBus) Publish(_ context.Context, e Event) error {
	if e.Timestamp == 0 {
		e.Timestamp = time.Now().Unix()
	}
	m.mu.RLock()
	subs := append([]sub(nil), m.subs...)
	m.mu.RUnlock()
	for _, s := range subs {
		if len(s.types) == 0 || s.types[e.Type] {
			go func(h Handler) {
				defer func() { _ = recover() }()
				h(e)
			}(s.h)
		}
	}
	return nil
}

func (m *memBus) Subscribe(types []string, h Handler) {
	mm := map[string]bool{}
	for _, t := range types {
		mm[t] = true
	}
	m.mu.Lock()
	m.subs = append(m.subs, sub{types: mm, h: h})
	m.mu.Unlock()
}

func (m *memBus) Available() bool { return false }

var (
	b     Bus
	onceB sync.Once
)

// I 获取全局事件总线并（Redis 模式下）启动消费
func I() Bus {
	onceB.Do(func() {
		if redisx.S().Available() {
			rb := &redisBus{rdb: redisx.RDB}
			rb.Start()
			b = rb
		} else {
			b = &memBus{}
		}
		log.Printf("[bus] 事件总线就绪 (redis=%v)", b.Available())
	})
	return b
}

// 事件类型常量（工单11/15/16/17 状态变更广播）
const (
	EvOrderCreated     = "order.created"     // 新订单：广播给在线司机（工单15/17 推送）
	EvOrderAccepted    = "order.accepted"    // 已接单（工单15：其他司机提示“已被接走”）
	EvOrderRejected    = "order.rejected"    // 拒单重新派发
	EvOrderStatus      = "order.status"      // 通用状态变更（工单16 行程开始/到达/账单/取消）
	EvDriverStatus     = "driver.status"     // 司机上下线广播
	EvDriverLocation   = "driver.location"   // 司机位置上报（工单11 订单跟踪）
	EvWeatherAlert     = "weather.alert"     // 天气预警推送（工单10）
	EvSOS             = "sos.alert"        // 紧急求助（工单13）
	EvAppPush         = "app.push"         // 应用内推送（乘客/司机/管理员）
)

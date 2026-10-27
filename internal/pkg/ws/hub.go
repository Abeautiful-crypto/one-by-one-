// Package ws：WebSocket 连接管理（工单11/15/17 订单实时推送；工单19 数字人长连接底座）
// 能力：注册/注销、按 key 定向发送、全员广播、服务端心跳(ping/pong)、写超时保护、
// 防强踢（工单19：同一用户允许多连接并存，旧连接不被新连接强制断开，避免会话丢失）。
package ws

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const (
	writeWait  = 10 * time.Second // 单次写超时
	pongWait   = 60 * time.Second // 读超时=心跳存活窗口
	pingPeriod = 30 * time.Second // 心跳周期（< pongWait）
	maxMsgSize = 1 << 20          // 1MB
)

// Conn 一条业务连接
type Conn struct {
	ID     string                 // 连接唯一标识
	UserID string                 // 业务标识（driverId / dh 会话）
	Conn   *websocket.Conn
	Send   chan []byte            // 出站队列
	hub    *Hub
	mu     sync.Mutex
	closed bool
}

// Hub 连接管理中心
type Hub struct {
	mu      sync.RWMutex
	clients map[string]*Conn // connID -> Conn
	byUser  map[string]map[*Conn]bool // userID -> 多连接（防强踢：并存）
	nextID  int64
	idMu    sync.Mutex
}

// NewHub 创建 Hub 并启动守护
func NewHub() *Hub {
	return &Hub{clients: map[string]*Conn{}, byUser: map[string]map[*Conn]bool{}}
}

var global = NewHub()

// Global 全局 Hub 单例（订单推送/数字人会话共用底座）
func Global() *Hub { return global }

// OnMessage 消息回调（由各业务设置）
var OnMessage func(c *Conn, raw []byte)

// OnClose 连接关闭回调
var OnClose func(c *Conn)

func (h *Hub) next() string {
	h.idMu.Lock()
	defer h.idMu.Unlock()
	h.nextID++
	return time.Now().Format("20060102150405") + "-" + itoa(h.nextID)
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// Upgrade 升级 HTTP 为 WebSocket 并注册连接（gin handler 内调用）
func (h *Hub) Upgrade(c *gin.Context, userID string) (*Conn, bool) {
	up := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true }, // 生产应校验 Origin（WebSocket 鉴权与限流，工单11）
	}
	raw, err := up.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return nil, false
	}
	conn := &Conn{ID: h.next(), UserID: userID, Conn: raw, Send: make(chan []byte, 256), hub: h}
	raw.SetReadLimit(maxMsgSize)
	_ = raw.SetReadDeadline(time.Now().Add(pongWait))
	raw.SetPongHandler(func(string) error {
		return raw.SetReadDeadline(time.Now().Add(pongWait)) // 心跳保活
	})
	h.mu.Lock()
	h.clients[conn.ID] = conn
	if h.byUser[userID] == nil {
		h.byUser[userID] = map[*Conn]bool{}
	}
	h.byUser[userID][conn] = true // 防强踢：新旧连接并存
	h.mu.Unlock()

	go h.writePump(conn)
	go h.readPump(conn)
	return conn, true
}

func (h *Hub) remove(c *Conn) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.mu.Unlock()
	_ = c.Conn.Close()
	close(c.Send)
	h.mu.Lock()
	delete(h.clients, c.ID)
	if set := h.byUser[c.UserID]; set != nil {
		delete(set, c)
		if len(set) == 0 {
			delete(h.byUser, c.UserID)
		}
	}
	h.mu.Unlock()
	if OnClose != nil {
		OnClose(c)
	}
}

// readPump 读循环：超时即视为异常断线（工单19：正确识别并处理异常断线）
func (h *Hub) readPump(c *Conn) {
	defer h.remove(c)
	for {
		_, raw, err := c.Conn.ReadMessage()
		if err != nil {
			return // 异常断线/正常关闭统一走 remove
		}
		if OnMessage != nil {
			OnMessage(c, raw)
		}
	}
}

// writePump 写循环：心跳 ping + 出站队列
func (h *Hub) writePump(c *Conn) {
	ticker := time.NewTicker(pingPeriod)
	defer func() { ticker.Stop(); h.remove(c) }()
	for {
		select {
		case msg, ok := <-c.Send:
			_ = c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.Conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			if err := c.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// SendTo 定向发送（按 userID，投递到其全部连接）
func (h *Hub) SendTo(userID string, msg []byte) {
	h.mu.RLock()
	set := h.byUser[userID]
	conns := make([]*Conn, 0, len(set))
	for c := range set {
		conns = append(conns, c)
	}
	h.mu.RUnlock()
	for _, c := range conns {
		h.trySend(c, msg)
	}
}

// Broadcast 广播全体连接（订单大厅推送等）
func (h *Hub) Broadcast(msg []byte) {
	h.mu.RLock()
	conns := make([]*Conn, 0, len(h.clients))
	for _, c := range h.clients {
		conns = append(conns, c)
	}
	h.mu.RUnlock()
	for _, c := range conns {
		h.trySend(c, msg)
	}
}

// OnlineCount 当前在线连接数
func (h *Hub) OnlineCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func (h *Hub) trySend(c *Conn, msg []byte) {
	defer func() { _ = recover() }() // 已关闭通道安全忽略
	select {
	case c.Send <- msg:
	default: // 队列满：背压丢弃（限流策略，工单11 速率节流）
	}
}

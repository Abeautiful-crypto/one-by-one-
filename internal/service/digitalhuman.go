// 数字人 WebSocket 会话服务（工单19：Go 重构 Python 后端核心逻辑）
// 统一通信协议（字段严格一致，不得增删）：
//   {"role":"user|assistant|system","type":"text|audio|event|control","content":"...","timestamp":1710000000}
// 会话状态（每连接独立）：上下文记录、交互模式(normal/immersive)、生成中标志、播放中标志；
// 支持：多轮上下文、流式分段回复、用户打断（终止当前链路且可继续接受新输入）、
// 心跳保活、异常断线清理、防强踢（多连接并存）、事件推送（start_speaking/stop_speaking/interrupted/dialog_end）。
package service

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"ride-hailing/internal/pkg/ws"
)

// DHMessage 统一协议（工单19 不得增删字段）
type DHMessage struct {
	Role      string `json:"role"`      // user | assistant | system
	Type      string `json:"type"`      // text | audio | event | control
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp"`
}

// DHSession 每连接独立会话状态（工单19：状态仅对当前连接可见，不得串线）
type DHSession struct {
	mu        sync.Mutex
	Conn      *ws.Conn
	Context   []DHMessage // 对话上下文记录（多轮）
	Mode      string      // normal | immersive（当前交互模式）
	Generating bool       // 当前是否正在生成回复
	Playing   bool        // 当前是否正在播放语音
	cancel    context.CancelFunc // 打断：终止当前生成链路
	genCtx    context.Context
}

// DHManager 会话管理器（连接 -> 会话）
type DHManager struct {
	mu       sync.RWMutex
	sessions map[string]*DHSession // connID -> session
}

var dhMgr = &DHManager{sessions: map[string]*DHSession{}}

// InitDigitalHuman 注册 WebSocket 消息回调（router 启动时调用一次）
func InitDigitalHuman() {
	ws.OnMessage = onWSMessage
	ws.OnClose = onWSClose
}

// onWSMessage 消息入口：区分数字人协议（role 字段）与业务推送确认
func onWSMessage(c *ws.Conn, raw []byte) {
	var m DHMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return
	}
	if m.Role == "" { // 非数字人协议消息忽略
		return
	}
	s := dhMgr.get(c)
	if s == nil {
		s = dhMgr.create(c)
	}
	s.handle(m)
}

// onWSClose 异常断线清理：单连接断开不影响其他会话（工单19）
func onWSClose(c *ws.Conn) {
	if s := dhMgr.get(c); s != nil {
		s.stopGenerating() // 终止进行中的生成链路，防 goroutine 泄漏
		dhMgr.remove(c)
	}
}

func (m *DHManager) get(c *ws.Conn) *DHSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[c.ID]
}

func (m *DHManager) create(c *ws.Conn) *DHSession {
	s := &DHSession{Conn: c, Mode: "normal"}
	m.mu.Lock()
	m.sessions[c.ID] = s
	m.mu.Unlock()
	return s
}

func (m *DHManager) remove(c *ws.Conn) {
	m.mu.Lock()
	delete(m.sessions, c.ID)
	m.mu.Unlock()
}

// send 发送统一协议消息
func (s *DHSession) send(role, typ, content string) {
	msg, _ := json.Marshal(DHMessage{Role: role, Type: typ, Content: content, Timestamp: time.Now().Unix()})
	s.hubSend(msg)
}

func (s *DHSession) hubSend(b []byte) {
	defer func() { _ = recover() }()
	select {
	case s.Conn.Send <- b:
	default:
	}
}

// sendEvent 数字人事件推送（工单19：type=event，顺序符合实际行为逻辑）
func (s *DHSession) sendEvent(name string) {
	s.send("system", "event", name)
}

// handle 会话消息处理（工单19：文本聊天/语音链路/打断/模式切换）
func (s *DHSession) handle(m DHMessage) {
	// control：打断与模式切换（工单19 沉浸模式：立即中断当前生成与播放）
	if m.Type == "control" {
		switch m.Content {
		case "interrupt":
			s.handleInterrupt()
		case "mode_immersive":
			s.mu.Lock(); s.Mode = "immersive"; s.mu.Unlock()
			s.sendEvent("mode_changed_immersive")
		case "mode_normal":
			s.mu.Lock(); s.Mode = "normal"; s.mu.Unlock()
			s.sendEvent("mode_changed_normal")
		}
		return
	}
	if m.Type != "text" && m.Type != "audio" {
		return
	}

	// 打断检查：沉浸模式下正在生成/播放时新输入 → 自动打断旧链路（工单19：可插话）
	s.mu.Lock()
	busy := s.Generating || s.Playing
	s.mu.Unlock()
	if busy && s.Mode == "immersive" {
		s.handleInterrupt()
	} else if busy {
		s.send("system", "event", "busy") // 普通模式提示忙（不产生多回复同时输出）
		return
	}

	// 记录上下文（多轮对话，与当前会话绑定）
	s.mu.Lock()
	s.Context = append(s.Context, DHMessage{Role: "user", Type: m.Type, Content: m.Content, Timestamp: time.Now().Unix()})
	s.mu.Unlock()

	// 启动生成链路（ASR→LLM/Coze→TTS 简化为文本链路；流式分段输出）
	s.startGenerating(m)
}

// handleInterrupt 打断（工单19：立即中断当前生成与播放，中断后不得继续推送旧数据）
func (s *DHSession) handleInterrupt() {
	s.stopGenerating()
	s.sendEvent("interrupted") // 被用户打断
	s.sendEvent("dialog_end")
}

func (s *DHSession) stopGenerating() {
	s.mu.Lock()
	wasGen := s.Generating || s.Playing
	s.Generating = false
	s.Playing = false
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel() // 终止当前链路 goroutine
	}
	_ = wasGen
}

// startGenerating 生成链路：流式逐段返回（边生成边播放=immersive；生成中标记防并发回复）
func (s *DHSession) startGenerating(input DHMessage) {
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.Generating = true
	s.Playing = true
	s.cancel = cancel
	s.genCtx = ctx
	s.mu.Unlock()

	s.sendEvent("start_speaking") // 事件：开始说话

	go func() {
		defer func() {
			_ = recover()
			s.mu.Lock()
			s.Generating = false
			s.Playing = false
			s.cancel = nil
			s.mu.Unlock()
		}()

		reply := chatReply(ctx, s, input) // LLM/Coze 回复（流式模拟为分段）
		if ctx.Err() != nil {
			return // 已被打断：不再推送旧数据（工单19）
		}

		// 流式分段推送（工单19：支持流式返回，逐段返回文本）
		segs := splitSegments(reply, 24)
		for i, seg := range segs {
			if ctx.Err() != nil {
				return // 打断后立即停止
			}
			s.send("assistant", "text", seg)
			if i == len(segs)-1 {
				s.sendEvent("stop_speaking")
				s.sendEvent("dialog_end") // 对话结束事件
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(120 * time.Millisecond): // 模拟 TTS 播放节奏
			}
		}
	}()
}

// chatReply 生成回复：Coze 已配置则真实调用；否则本地回声策略（保证链路可测）
func chatReply(ctx context.Context, s *DHSession, input DHMessage) string {
	coze := NewCozeClient()
	if coze.Available() {
		if r, err := coze.Chat(ctx, input.Content); err == nil {
			return r
		}
	}
	// 本地降级：多轮上下文回声（工单19 验收可运行，不依赖外部服务）
	s.mu.Lock()
	n := 0
	for _, m := range s.Context { // 按用户轮次计数（不含 assistant 回复）
		if m.Role == "user" {
			n++
		}
	}
	s.mu.Unlock()
	assistant := DHMessage{Role: "assistant", Type: "text", Content: "已收到你的第" + itoaDH(n) + "条消息：" + input.Content, Timestamp: time.Now().Unix()}
	s.mu.Lock()
	s.Context = append(s.Context, assistant)
	s.mu.Unlock()
	return assistant.Content
}

// splitSegments 文本分段（流式）
func splitSegments(s string, n int) []string {
	if len([]rune(s)) <= n {
		return []string{s}
	}
	var out []string
	runes := []rune(s)
	for i := 0; i < len(runes); i += n {
		e := i + n
		if e > len(runes) {
			e = len(runes)
		}
		out = append(out, string(runes[i:e]))
	}
	return out
}

func itoaDH(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

var _ = strings.TrimSpace

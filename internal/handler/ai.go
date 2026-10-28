// AI 出行助手（工单10）与数字人会话（工单19）接口
package handler

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"ride-hailing/internal/config"
	"ride-hailing/internal/middleware"
	"ride-hailing/internal/model"
	"ride-hailing/internal/pkg/jwtutil"
	"ride-hailing/internal/pkg/resp"
	"ride-hailing/internal/pkg/ws"
	"ride-hailing/internal/service"

	"github.com/gin-gonic/gin"
)

// AIHandler AI 出行助手接口
type AIHandler struct{}

// ChatRequest Coze 智能体对话请求
type ChatRequest struct {
	Message string `json:"message" binding:"required"`
}

// Chat AI 对话（Coze 未配置时降级本地策略回复）
func (h *AIHandler) Chat(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	coze := service.NewCozeClient()
	if coze.Available() {
		if r, err := coze.Chat(c.Request.Context(), req.Message); err == nil {
			resp.OK(c, gin.H{"provider": "coze", "reply": r})
			return
		}
	}
	// 本地降级：基于规则回复（天气/积水/路线关键词路由到对应能力）
	reply := service.LocalAIReply(req.Message)
	resp.OK(c, gin.H{"provider": "local", "reply": reply})
}

// RainRoute Function Calling：calculate_rain_route 雨天智能路线（工单10 阶段3）
func (h *AIHandler) RainRoute(c *gin.Context) {
	var p service.RainRouteParams
	if err := c.ShouldBindJSON(&p); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	if p.Preference == "" {
		p.Preference = "safest"
	}
	if p.Rainfall == "" {
		if _, err := service.GetWeatherAlert("上海"); err == nil {
			p.Rainfall = "heavy" // 有活跃预警视为大雨
		} else {
			p.Rainfall = "light"
		}
	}
	routes := service.CalculateRainRoute(p)
	resp.OK(c, gin.H{"routes": routes, "recommended": routes[0]})
}

// WeatherAlert 查询天气预警（Function Calling 回调天气预报）
func (h *AIHandler) WeatherAlert(c *gin.Context) {
	a, err := service.GetWeatherAlert(c.DefaultQuery("city", "上海"))
	if err != nil {
		resp.OK(c, gin.H{"active": false, "message": "当前无生效预警"})
		return
	}
	resp.OK(c, gin.H{"active": true, "alert": a})
}

type pushAlertReq struct {
	City        string `json:"city" binding:"required"`
	Level       string `json:"level" binding:"required,oneof=yellow orange red"`
	Type        string `json:"type"`
	Title       string `json:"title" binding:"required"`
	Impact      string `json:"impact"`
	ActiveHours float64 `json:"activeHours"`
}

// PushWeatherAlert 发布天气预警并推送在线用户（工单10）
func (h *AIHandler) PushWeatherAlert(c *gin.Context) {
	var req pushAlertReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	a := &model.WeatherAlert{
		City: req.City, Level: req.Level, Type: req.Type,
		Title: req.Title, Impact: req.Impact,
	}
	if err := service.PushWeatherAlert(a); err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, a)
}

// Shelters 避雨上车点推荐（工单10：200米内带顶棚位置）
func (h *AIHandler) Shelters(c *gin.Context) {
	lng := parseF(c.Query("lng"), 121.4737)
	lat := parseF(c.Query("lat"), 31.2304)
	pts, err := service.RecommendShelters(c.DefaultQuery("city", "上海"), lng, lat)
	if err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, pts)
}

type floodReq struct {
	RoadName string  `json:"roadName" binding:"required"`
	DepthCm  float64 `json:"depthCm"`
	Lng      float64 `json:"lng"`
	Lat      float64 `json:"lat"`
	PhotoURL string  `json:"photoUrl"`
}

// ReportFlood 积水上报（工单10：更新途经订单+发补偿券+上报市政）
func (h *AIHandler) ReportFlood(c *gin.Context) {
	uid := middleware.GetUserID(c)
	var req floodReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	f := &model.FloodReport{
		ReporterID: uid, RoadName: req.RoadName, DepthCm: req.DepthCm,
		Lng: req.Lng, Lat: req.Lat, PhotoURL: req.PhotoURL,
	}
	saved, affected, err := service.ReportFlood(f)
	if err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"report": saved, "affectedOrders": affected, "coupon": "¥10 绕行补偿券已发放"})
}

// RedAlertCheck 暴雨红色预警安全校验（工单10：驾龄>5年等）
func (h *AIHandler) RedAlertCheck(c *gin.Context) {
	driverID := middleware.GetUserID(c)
	if since := c.Query("licenseSince"); since != "" {
		if t, err := timeParse(since); err == nil {
			ok, msg := service.RedAlertSafetyCheck(driverID, t)
			resp.OK(c, gin.H{"allowed": ok, "message": msg})
			return
		}
	}
	ok, msg := service.RedAlertSafetyCheck(driverID, nowMinusYears(6))
	resp.OK(c, gin.H{"allowed": ok, "message": msg})
}

// DHHandler 数字人会话接口（工单19）
type DHHandler struct{}

// WsDigitalHuman WebSocket 接入点（会话状态每连接独立、防强踢、心跳保活）
// 鉴权：浏览器原生 WS 无法带自定义头，故支持 Header(Authorization) 或 query(token) 透传。
func (h *DHHandler) WsDigitalHuman(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		if ah := c.GetHeader("Authorization"); ah != "" {
			token = strings.TrimPrefix(ah, "Bearer ")
		}
	}
	if token == "" {
		resp.Unauthorized(c, "缺少鉴权 token（Header 或 ?token=）")
		return
	}
	claims, err := jwtutil.ParseToken(token, config.Load().JWTSecret)
	if err != nil {
		resp.Unauthorized(c, "token 无效或已过期")
		return
	}
	uid := strconv.FormatUint(uint64(claims.UserID), 10)
	userKey := "dh-" + uid + "-" + c.Query("sid")
	if _, ok := ws.Global().Upgrade(c, userKey); ok {
		// 连接生命周期由 hub 管理；会话在 OnMessage/OnClose 中创建销毁
	}
}

// DhStats 数字人在线会话统计
func (h *DHHandler) DhStats(c *gin.Context) {
	resp.OK(c, gin.H{"onlineConnections": ws.Global().OnlineCount()})
}

// DHStatus 数字人/Coze 智能体配置状态（工单18：前端据此选择本地/云端模式）
func (h *AIHandler) DHStatus(c *gin.Context) {
	coze := service.NewCozeClient()
	resp.OK(c, gin.H{"cozeAvailable": coze.Available(), "mode": map[bool]string{true: "coze", false: "local"}[coze.Available()]})
}

// ---- 辅助 ----

func parseF(s string, def float64) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return def
	}
	return v
}

func timeParse(s string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, errors.New("invalid time")
}

func nowMinusYears(y float64) time.Time {
	return time.Now().AddDate(0, 0, -int(y*365))
}

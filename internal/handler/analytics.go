// 工单：全栈开发-网约车-06-数据分析
// 数据分析接口：订单量分析（日/周/月）、用户转化率（漏斗+取消原因）、复购周期分析。
package handler

import (
	"time"

	"ride-hailing/internal/pkg/resp"
	"ride-hailing/internal/service"

	"github.com/gin-gonic/gin"
)

type AnalyticsHandler struct{}

func parseRange(c *gin.Context) (time.Time, time.Time) {
	var start, end time.Time
	if s := c.Query("start"); s != "" {
		if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
			start = t
		}
	}
	if e := c.Query("end"); e != "" {
		if t, err := time.ParseInLocation("2006-01-02", e, time.Local); err == nil {
			end = t.AddDate(0, 0, 1)
		}
	}
	return start, end
}

// OrderAnalysis 订单量分析（工单06：总量统计、增长率、平均订单值、有效/无效/优惠券订单数）
func (h *AnalyticsHandler) OrderAnalysis(c *gin.Context) {
	g := c.DefaultQuery("granularity", "day") // day/week/month
	start, end := parseRange(c)
	r, err := service.AnalyzeOrders(g, start, end)
	if err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, r) // 汇总响应 5 秒内（SQL 聚合 + 索引保障）
}

// ConversionAnalysis 用户转化率分析（工单06）
// 转化率 =（产生购买行为的客户人数 ÷ 所有注册 app 的人数）× 100%
func (h *AnalyticsHandler) ConversionAnalysis(c *gin.Context) {
	start, end := parseRange(c)
	r, err := service.AnalyzeConversion(start, end)
	if err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, r)
}

// RepurchaseAnalysis 复购周期分析（工单06：首次订单时间、后续订单时间、复购次数、周期）
func (h *AnalyticsHandler) RepurchaseAnalysis(c *gin.Context) {
	start, end := parseRange(c)
	r, err := service.AnalyzeRepurchase(start, end)
	if err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, r)
}

// TrackVisit 行为埋点上报（工单06：访问记录、点击流数据、停留时间）
func (h *AnalyticsHandler) TrackVisit(c *gin.Context) {
	var req struct {
		Page      string `json:"page" binding:"required"`
		Action    string `json:"action" binding:"omitempty,oneof=view click stay"`
		StayMs    int    `json:"stayMs"`
		DeviceID  string `json:"deviceId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	v := service.VisitFromContext(c, req.Page, req.Action, req.StayMs, req.DeviceID)
	resp.OK(c, gin.H{"id": v.ID})
}

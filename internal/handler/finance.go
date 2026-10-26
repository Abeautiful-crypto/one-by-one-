// 工单：全栈开发-网约车-03-财务管理
// 财务接口：交易明细（灵活查询）、收支对账、自动账单、异常处理、争议解决。
package handler

import (
	"errors"
	"time"

	"ride-hailing/internal/model"
	"ride-hailing/internal/pkg/resp"
	"ride-hailing/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type FinanceHandler struct{}

// ListTransactions 交易详情（工单03：交易日期、订单号、金额、支付方式；灵活自定义日期范围查询）
func (h *FinanceHandler) ListTransactions(c *gin.Context) {
	var q resp.PageQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	q.Normalize()
	qx := model.DB.Model(&model.Transaction{})
	if s := c.Query("start"); s != "" { // 自定义日期范围
		if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
			qx = qx.Where("txn_time >= ?", t)
		}
	}
	if e := c.Query("end"); e != "" {
		if t, err := time.ParseInLocation("2006-01-02", e, time.Local); err == nil {
			qx = qx.Where("txn_time < ?", t.AddDate(0, 0, 1))
		}
	}
	if om := c.Query("orderNo"); om != "" {
		qx = qx.Where("order_no LIKE ?", "%"+om+"%")
	}
	if pm := c.Query("payMethod"); pm != "" { // 区分不同支付方式获得的收入
		qx = qx.Where("pay_method = ?", pm)
	}
	if dir := c.Query("direction"); dir != "" {
		qx = qx.Where("direction = ?", dir)
	}
	var total int64
	qx.Count(&total)
	var txns []model.Transaction
	qx.Limit(q.PageSize).Offset((q.Page - 1) * q.PageSize).Order("txn_time DESC").Find(&txns)
	resp.OK(c, resp.PageResult{List: txns, Total: total, Page: q.Page, PageSize: q.PageSize})
}

// Reconcile 收支对账（工单03：确保每一笔交易都被正确记录）
func (h *FinanceHandler) Reconcile(c *gin.Context) {
	start := parseDateOr7DaysAgo(c.Query("start"))
	end := parseDateOrToday(c.Query("end"))
	r, err := service.Reconcile(start, end)
	if err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, r)
}

// CreateBill 自动账单生成（工单03：每日/每周/每月，异步生成）
func (h *FinanceHandler) CreateBill(c *gin.Context) {
	var req struct {
		PeriodType string `json:"periodType" binding:"required,oneof=day week month"`
		Date       string `json:"date"` // 起始日期 yyyy-MM-dd，默认今天
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	start := time.Now().Truncate(24 * time.Hour)
	if req.Date != "" {
		t, err := time.ParseInLocation("2006-01-02", req.Date, time.Local)
		if err != nil {
			resp.BadRequest(c, "date 应为 yyyy-MM-dd 格式")
			return
		}
		start = t
	}
	bill, err := service.GenerateBill(req.PeriodType, start)
	if err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"bill": bill, "message": "账单生成中，稍后查询状态"})
}

// ListBills 账单列表
func (h *FinanceHandler) ListBills(c *gin.Context) {
	var q resp.PageQuery
	_ = c.ShouldBindQuery(&q)
	q.Normalize()
	var total int64
	model.DB.Model(&model.Bill{}).Count(&total)
	var bills []model.Bill
	model.DB.Limit(q.PageSize).Offset((q.Page - 1) * q.PageSize).Order("id DESC").Find(&bills)
	resp.OK(c, resp.PageResult{List: bills, Total: total, Page: q.Page, PageSize: q.PageSize})
}

// ListAnomalies 异常交易列表（工单03：错误检测）
func (h *FinanceHandler) ListAnomalies(c *gin.Context) {
	var q resp.PageQuery
	_ = c.ShouldBindQuery(&q)
	q.Normalize()
	qx := model.DB.Model(&model.Anomaly{})
	if r := c.Query("resolved"); r == "false" || r == "true" {
		qx = qx.Where("resolved = ?", r == "true")
	}
	var total int64
	qx.Count(&total)
	var list []model.Anomaly
	qx.Limit(q.PageSize).Offset((q.Page - 1) * q.PageSize).Order("id DESC").Find(&list)
	resp.OK(c, resp.PageResult{List: list, Total: total, Page: q.Page, PageSize: q.PageSize})
}

// ResolveAnomaly 异常修正（工单03：提供快速纠正措施）
func (h *FinanceHandler) ResolveAnomaly(c *gin.Context) {
	id, err := parseUint(c.Param("id"))
	if err != nil {
		resp.BadRequest(c, "id 非法")
		return
	}
	var req struct {
		Note string `json:"note" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	var a model.Anomaly
	if err := model.DB.First(&a, id).Error; err != nil {
		resp.NotFound(c, "异常记录不存在")
		return
	}
	a.Resolved = true
	a.ResolveNote = req.Note
	model.DB.Save(&a)
	resp.OK(c, a)
}

// ---------- 争议 ----------

type disputeReq struct {
	OrderNo string `json:"orderNo" binding:"required"`
	Type    string `json:"type" binding:"required,oneof=amount timing other"`
	Content string `json:"content" binding:"required,max=500"`
}

// CreateDispute 提交争议（工单03：清晰的争议解决流程）
func (h *FinanceHandler) CreateDispute(c *gin.Context) {
	var req disputeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	d := model.Dispute{OrderNo: req.OrderNo, Type: req.Type, Content: req.Content, Status: "open"}
	if err := model.DB.Create(&d).Error; err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, d)
}

// HandleDispute 处理争议
func (h *FinanceHandler) HandleDispute(c *gin.Context) {
	id, err := parseUint(c.Param("id"))
	if err != nil {
		resp.BadRequest(c, "id 非法")
		return
	}
	var req struct {
		Status  string `json:"status" binding:"required,oneof=processing resolved closed"`
		Result  string `json:"result"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	var d model.Dispute
	if err := model.DB.First(&d, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			resp.NotFound(c, "争议记录不存在")
			return
		}
		resp.ServerError(c, err.Error())
		return
	}
	d.Status, d.Result = req.Status, req.Result
	model.DB.Save(&d)
	resp.OK(c, d)
}

func parseDateOr7DaysAgo(s string) time.Time {
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return t
	}
	return time.Now().AddDate(0, 0, -7)
}

func parseDateOrToday(s string) time.Time {
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return t.AddDate(0, 0, 1)
	}
	return time.Now()
}

// 工单：全栈开发-网约车-04-订单管理（退票管理 & 批量退票）
// 退票接口：在线申请（自动审核）→ 人工复核 → 批准/拒绝 → 状态跟踪 → 审计追踪。
// 批量退票：全选/反选由前端实现，后端支持批量 ID + 单个排除，按取消费政策自动计算。
package handler

import (
	"fmt"
	"time"

	"ride-hailing/internal/middleware"
	"ride-hailing/internal/model"
	"ride-hailing/internal/pkg/resp"
	"ride-hailing/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type RefundHandler struct{}

type applyRefundReq struct {
	OrderID    uint   `json:"orderId" binding:"required"`
	Reason     string `json:"reason" binding:"required"` // 个人计划改变、司机未按时到达等
	ForceMajor bool   `json:"forceMajor"`                // 不可抗力（全额退款情形）
}

// ApplyRefund 在线申请退票（工单04：自动审核基于预设规则立即反馈）
func (h *RefundHandler) ApplyRefund(c *gin.Context) {
	var req applyRefundReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	var order model.Order
	if err := model.DB.First(&order, req.OrderID).Error; err != nil {
		resp.NotFound(c, "订单不存在")
		return
	}
	if order.UserID != middleware.GetUserID(c) && !hasStaffRole(c) {
		resp.Forbidden(c, "仅订单本人可申请退票")
		return
	}
	if order.Status == model.OrderCancelled {
		resp.BadRequest(c, "订单已取消，无需退票")
		return
	}

	// 自动审核：按取消费策略计算应退金额
	refundAmount, deductFee := service.CalcRefund(&order, req.ForceMajor)
	status := model.RefundAutoApproved
	auditBy := "system"
	auditNote := service.FormatPolicyDesc(req.ForceMajor)
	if refundAmount == 0 && !req.ForceMajor {
		status = model.RefundManualReview // 复杂情况进入人工复核
		auditNote += "；无退款金额，转人工复核"
	}
	refund := model.Refund{
		OrderID: order.ID, OrderNo: order.OrderNo, UserID: middleware.GetUserID(c),
		Reason: req.Reason, ForceMajor: req.ForceMajor,
		RefundAmount: refundAmount, DeductFee: deductFee,
		Status: status, AuditBy: auditBy, AuditNote: auditNote, AuditTime: nowPtr(),
	}
	if err := model.DB.Create(&refund).Error; err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	// 自动通过的直接执行退款
	if status == model.RefundAutoApproved {
		if err := executeRefund(&refund, &order); err == nil {
			refund.Status = model.RefundApproved
			model.DB.Model(&refund).Update("status", model.RefundApproved)
		}
	}
	resp.OK(c, gin.H{"refund": refund, "message": refundMessage(status)})
}

func refundMessage(status string) string {
	switch status {
	case model.RefundAutoApproved:
		return "自动审核通过，退款已发起"
	case model.RefundManualReview:
		return "已提交人工复核，请等待客服处理"
	default:
		return "申请已受理"
	}
}

// executeRefund 执行退款：写支出流水 + 更新订单状态（并发场景使用事务保证一致性）
func executeRefund(refund *model.Refund, order *model.Order) error {
	return model.DB.Transaction(func(tx *gorm.DB) error {
		txn := &model.Transaction{
			OrderNo: order.OrderNo, OrderID: order.ID, UserID: order.UserID,
			DriverID: order.DriverID, Direction: model.DirectionExpense,
			Amount: refund.RefundAmount, PayMethod: order.PayMethod, TxnType: "refund",
			TxnTime: time.Now(), Remark: "退票退款 " + refund.AuditNote,
		}
		if err := service.RecordTransaction(tx, txn); err != nil {
			return err
		}
		order.Status = model.OrderCancelled
		order.CancelReason = "退票: " + refund.Reason
		if err := tx.Save(order).Error; err != nil {
			return err
		}
		return changeOrderStatusTx(tx, order, model.OrderCancelled, refund.Reason, "refund:"+fmt.Sprint(refund.ID))
	})
}

type auditRefundReq struct {
	Action  string `json:"action" binding:"required,oneof=approve reject manual"` // 人工复核
	Note    string `json:"note"`
	Amount  float64 `json:"amount"` // 人工调整退款金额
}

// AuditRefund 人工复核（工单04：复杂情况或超出自动处理范围的请求，安排客服人工审核）
func (h *RefundHandler) AuditRefund(c *gin.Context) {
	id, err := parseUint(c.Param("id"))
	if err != nil {
		resp.BadRequest(c, "id 非法")
		return
	}
	var req auditRefundReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	var refund model.Refund
	if err := model.DB.First(&refund, id).Error; err != nil {
		resp.NotFound(c, "退票申请不存在")
		return
	}
	var order model.Order
	if err := model.DB.First(&order, refund.OrderID).Error; err != nil {
		resp.NotFound(c, "关联订单不存在")
		return
	}
	refund.AuditBy = middleware.GetUsername(c)
	refund.AuditNote = req.Note
	refund.AuditTime = nowPtr()
	switch req.Action {
	case "approve":
		if req.Amount > 0 && req.Amount <= refund.RefundAmount+refund.DeductFee {
			refund.RefundAmount = req.Amount // 手动调整选项（工单04）
		}
		if err := executeRefund(&refund, &order); err != nil {
			resp.ServerError(c, "退款执行失败: "+err.Error())
			return
		}
		refund.Status = model.RefundApproved
	case "reject":
		refund.Status = model.RefundRejected
	case "manual":
		refund.Status = model.RefundManualReview
	}
	if err := model.DB.Save(&refund).Error; err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, refund)
}

// ListRefunds 退票申请列表 + 状态跟踪
func (h *RefundHandler) ListRefunds(c *gin.Context) {
	var q resp.PageQuery
	_ = c.ShouldBindQuery(&q)
	q.Normalize()
	qx := model.DB.Model(&model.Refund{})
	if !hasStaffRole(c) {
		qx = qx.Where("user_id = ?", middleware.GetUserID(c))
	}
	if st := c.Query("status"); st != "" {
		qx = qx.Where("status = ?", st)
	}
	var total int64
	qx.Count(&total)
	var refunds []model.Refund
	qx.Limit(q.PageSize).Offset((q.Page - 1) * q.PageSize).Order("id DESC").Find(&refunds)
	resp.OK(c, resp.PageResult{List: refunds, Total: total, Page: q.Page, PageSize: q.PageSize})
}

// BatchRefund 批量退票（工单04）：
// 全选/反选由前端传 ID 列表实现；excludeIds 单个排除；按取消费政策自动计算应扣款项。
// 性能：事务内分批处理，避免长事务锁表。
func (h *RefundHandler) BatchRefund(c *gin.Context) {
	var req struct {
		OrderIDs   []uint  `json:"orderIds" binding:"required,min=1"`
		ExcludeIDs []uint  `json:"excludeIds"` // 单个排除
		Reason     string  `json:"reason" binding:"required"`
		ForceMajor bool    `json:"forceMajor"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	excl := map[uint]bool{}
	for _, id := range req.ExcludeIDs {
		excl[id] = true
	}
	batchNo := fmt.Sprintf("BATCH%s%06d", time.Now().Format("20060102150405"), middleware.GetUserID(c))
	result := gin.H{"batchNo": batchNo, "success": 0, "skipped": 0, "manualReview": 0, "failed": []string{}}
	for _, oid := range req.OrderIDs {
		if excl[oid] {
			result["skipped"] = result["skipped"].(int) + 1
			continue
		}
		var order model.Order
		if err := model.DB.First(&order, oid).Error; err != nil ||
			(order.Status != model.OrderPaid && order.Status != model.OrderPending) {
			result["skipped"] = result["skipped"].(int) + 1
			continue
		}
		refundAmount, deductFee := service.CalcRefund(&order, req.ForceMajor)
		status := model.RefundAutoApproved
		if refundAmount == 0 && !req.ForceMajor {
			status = model.RefundManualReview
		}
		refund := model.Refund{
			OrderID: order.ID, OrderNo: order.OrderNo, UserID: order.UserID,
			Reason: req.Reason, ForceMajor: req.ForceMajor,
			RefundAmount: refundAmount, DeductFee: deductFee,
			Status: status, AuditBy: "system", AuditNote: "批量退票 " + batchNo + "；" + service.FormatPolicyDesc(req.ForceMajor),
			AuditTime: nowPtr(), BatchNo: batchNo,
		}
		if err := model.DB.Create(&refund).Error; err != nil {
			result["failed"] = append(result["failed"].([]string), fmt.Sprintf("订单%d: %v", oid, err))
			continue
		}
		if status == model.RefundAutoApproved {
			if err := executeRefund(&refund, &order); err == nil {
				model.DB.Model(&refund).Update("status", model.RefundApproved)
				result["success"] = result["success"].(int) + 1
			}
		} else {
			result["manualReview"] = result["manualReview"].(int) + 1
		}
	}
	resp.OK(c, result)
}

func nowPtr() *time.Time { t := time.Now(); return &t }

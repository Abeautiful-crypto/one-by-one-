// 工单：全栈开发-网约车-04-订单管理（退票管理）
// 退票/退款申请：在线申请、自动审核、人工复核、状态跟踪、审计追踪。
package model

import "time"

// 退票状态（工单04：待审核、已批准、已拒绝 + 人工复核）
const (
	RefundPending      = "pending"       // 待审核
	RefundAutoApproved = "auto_approved" // 自动审核通过
	RefundManualReview = "manual_review" // 人工复核中
	RefundApproved     = "approved"      // 已批准（已退款）
	RefundRejected     = "rejected"      // 已拒绝
)

// Refund 退票申请
type Refund struct {
	BaseModel
	OrderID    uint       `gorm:"index;not null" json:"orderId"`
	OrderNo    string     `gorm:"size:32;index" json:"orderNo"`
	UserID     uint       `gorm:"index;not null" json:"userId"` // 申请人
	Reason     string     `gorm:"size:255;not null" json:"reason"` // 取消原因：个人计划改变、司机未按时到达等
	ForceMajor bool       `json:"forceMajor"`   // 是否不可抗力（工单04：全额退款情形）
	RefundAmount float64  `json:"refundAmount"` // 应退金额（按取消费政策计算）
	DeductFee  float64    `json:"deductFee"`    // 扣除的取消费
	Status     string     `gorm:"size:20;index;default:pending" json:"status"`
	AuditBy    string     `gorm:"size:64" json:"auditBy"`     // 审核人（自动审核标记为 system）
	AuditNote  string     `gorm:"size:255" json:"auditNote"`  // 审核意见（审计追踪）
	AuditTime  *time.Time `json:"auditTime"`
	BatchNo    string     `gorm:"size:32;index" json:"batchNo"` // 批量退票批次号
}

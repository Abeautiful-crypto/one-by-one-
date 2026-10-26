// 工单：全栈开发-网约车-03-财务管理
// 交易流水、自动账单、异常检测、争议处理、收支对账。
package model

import "time"

// 资金方向
const (
	DirectionIncome   = "income"   // 收入
	DirectionExpense  = "expense"  // 支出（退款、补贴等）
)

// Transaction 交易流水（工单03：精确记录每笔交易，实时同步保证数据一致）
type Transaction struct {
	BaseModel
	TradeNo     string    `gorm:"size:32;uniqueIndex;not null" json:"tradeNo"` // 交易号
	OrderNo     string    `gorm:"size:32;index" json:"orderNo"`                // 相关订单号
	OrderID     uint      `gorm:"index" json:"orderId"`
	UserID      uint      `gorm:"index" json:"userId"`  // 乘客 ID
	DriverID    uint      `gorm:"index" json:"driverId"` // 司机 ID
	Direction   string    `gorm:"size:10;not null" json:"direction"` // income/expense
	Amount      float64   `json:"amount"`
	PayMethod   string    `gorm:"size:20" json:"payMethod"` // credit_card/e_wallet/wechat/alipay（工单03：区分支付方式）
	TxnType     string    `gorm:"size:20" json:"txnType"`   // pay/refund/subsidy
	Status      string    `gorm:"size:20;default:success" json:"status"`
	TxnTime     time.Time `gorm:"index" json:"txnTime"`
	Remark      string    `gorm:"size:255" json:"remark"`
}

// Bill 自动生成的账单（工单03：每日/每周/每月账单，后台异步生成）
type Bill struct {
	BaseModel
	PeriodType  string    `gorm:"size:10;not null" json:"periodType"` // day/week/month
	PeriodStart time.Time `json:"periodStart"`
	PeriodEnd   time.Time `json:"periodEnd"`
	TotalIncome float64   `json:"totalIncome"`
	TotalExpense float64  `json:"totalExpense"`
	OrderCount  int64     `json:"orderCount"`
	Status      string    `gorm:"size:20;default:generating" json:"status"` // generating/done/failed
}

// Anomaly 异常交易记录（工单03：自动检测重复扣款、金额不符等）
type Anomaly struct {
	BaseModel
	Type      string    `gorm:"size:32;not null" json:"type"`  // duplicate_payment/amount_mismatch
	OrderNo   string    `gorm:"size:32;index" json:"orderNo"`
	Detail    string    `gorm:"size:500" json:"detail"`
	Resolved  bool      `gorm:"default:false" json:"resolved"`
	ResolvedBy string   `gorm:"size:64" json:"resolvedBy"`
	ResolveNote string  `gorm:"size:255" json:"resolveNote"`
}

// Dispute 争议处理（工单03：清晰的争议解决流程）
type Dispute struct {
	BaseModel
	OrderNo   string    `gorm:"size:32;index" json:"orderNo"`
	UserID    uint      `gorm:"index" json:"userId"`
	Type      string    `gorm:"size:32" json:"type"`    // amount/timing/other
	Content   string    `gorm:"size:500" json:"content"`
	Status    string    `gorm:"size:20;default:open" json:"status"` // open/processing/resolved/closed
	Result    string    `gorm:"size:500" json:"result"`
	Handler   string    `gorm:"size:64" json:"handler"`
}

// 工单：全栈开发-网约车-04-订单管理
// 订单（班车/顺风车统一）、订单状态历史、双向评价。
package model

import "time"

// 订单类型
const (
	OrderTypeBus     = "bus"     // 班车票
	OrderTypeCarpool = "carpool" // 顺风车
)

// 订单状态（工单04）
const (
	OrderPending   = "pending"   // 待接单/待支付
	OrderPaid      = "paid"      // 已支付/待出行
	OrderOngoing   = "ongoing"   // 进行中
	OrderFinished  = "finished"  // 已完成
	OrderCancelled = "cancelled" // 已取消（记录取消原因）
)

// Order 订单：工单04 订单中包含班车和顺风车的订单，通过筛选区分订单以及详情信息
type Order struct {
	BaseModel
	OrderNo      string    `gorm:"size:32;uniqueIndex;not null" json:"orderNo"` // 订单编号，唯一
	OrderType    string    `gorm:"size:20;index" json:"orderType"`              // bus/carpool
	UserID       uint      `gorm:"index" json:"userId"`                         // 乘客
	DriverID     uint      `gorm:"index" json:"driverId"`                       // 司机
	ShiftID      uint      `gorm:"index" json:"shiftId"`                        // 班车班次（班车订单）
	TripID       uint      `gorm:"index" json:"tripId"`                         // 顺风车行程（顺风车订单）
	StartName    string    `gorm:"size:64" json:"startName"`
	EndName      string    `gorm:"size:64" json:"endName"`
	ContactName  string    `gorm:"size:64" json:"contactName"` // 乘客信息
	ContactPhone string    `gorm:"size:20" json:"contactPhone"`
	Amount       float64   `json:"amount"`                     // 应付金额
	PaidAmount   float64   `json:"paidAmount"`                 // 实付金额
	Discount     float64   `json:"discount"`                   // 优惠金额
	CouponID     uint      `gorm:"index" json:"couponId"`      // 使用的优惠券
	PayMethod    string    `gorm:"size:20" json:"payMethod"`   // wechat/alipay/bank_card（工单03：多支付方式）
	Status       string    `gorm:"size:20;index" json:"status"`
	CancelReason string    `gorm:"size:255" json:"cancelReason"` // 取消原因标注
	FinishTime   *time.Time `json:"finishTime"`
	Remark       string    `gorm:"size:255" json:"remark"`
}

// OrderStatusLog 订单状态历史变更记录（工单04：记录所有变化的时间点和原因）
type OrderStatusLog struct {
	BaseModel
	OrderID   uint   `gorm:"index;not null" json:"orderId"`
	FromStatus string `gorm:"size:20" json:"fromStatus"`
	ToStatus   string `gorm:"size:20;not null" json:"toStatus"`
	Reason    string `gorm:"size:255" json:"reason"`
	Operator  string `gorm:"size:64" json:"operator"` // 操作人（系统/用户名）
}

// Review 双向评价（工单01/04：乘客评价司机服务质量，司机反馈乘客行为表现）
type Review struct {
	BaseModel
	OrderID  uint   `gorm:"index;not null" json:"orderId"`
	FromUID  uint   `gorm:"index;not null" json:"fromUid"` // 评价人
	ToUID    uint   `gorm:"index;not null" json:"toUid"`   // 被评价人
	Role     string `gorm:"size:20;not null" json:"role"`  // passenger_to_driver / driver_to_passenger
	Score    int    `json:"score"`                         // 1-5
	Content  string `gorm:"size:500" json:"content"`
}

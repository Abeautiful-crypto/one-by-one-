// 工单12/13/14/18：乘客端域模型（账户信息、常用地址、支付方式、反馈、紧急求助、打赏、发票）
package model

import "time"

// UserAddress 常用地址（工单12/13：家庭/公司/常用地址管理）
type UserAddress struct {
	BaseModel
	UserID    uint   `gorm:"index" json:"userId"`
	Label     string `gorm:"size:20" json:"label"`    // home/work/other
	Name      string `gorm:"size:64" json:"name"`     // 地址名称
	Phone     string `gorm:"size:20" json:"phone"`    // 联系人
	Address   string `gorm:"size:255" json:"address"` // 详细地址
	Lng       float64 `json:"lng"`
	Lat       float64 `json:"lat"`
	IsDefault bool   `gorm:"default:false" json:"isDefault"`
}

func (UserAddress) TableName() string { return "user_addresses" }

// PaymentMethod 支付方式（工单12：添加/编辑/删除支付方法）
type PaymentMethod struct {
	BaseModel
	UserID   uint   `gorm:"index" json:"userId"`
	Type     string `gorm:"size:20" json:"type"`   // bank/alipay/wechat
	Account  string `gorm:"size:64" json:"account"` // 卡号/账号（掩码存储）
	Holder   string `gorm:"size:64" json:"holder"`  // 持卡人
	Expiry   string `gorm:"size:12" json:"expiry"`
	IsDefault bool  `gorm:"default:false" json:"isDefault"`
}

func (PaymentMethod) TableName() string { return "payment_methods" }

// UserFeedback 帮助与反馈（工单12）
type UserFeedback struct {
	BaseModel
	UserID  uint   `gorm:"index" json:"userId"`
	Type    string `gorm:"size:20" json:"type"` // bug/suggest/complaint
	Content string `gorm:"size:1024" json:"content"`
	Contact string `gorm:"size:64" json:"contact"`
	Status  string `gorm:"size:20;default:open" json:"status"` // open/handled
}

func (UserFeedback) TableName() string { return "user_feedbacks" }

// SOSAlert 紧急求助（工单13：一键求助）
type SOSAlert struct {
	BaseModel
	UserID  uint    `gorm:"index" json:"userId"`
	OrderNo string  `gorm:"size:32" json:"orderNo"`
	Lng     float64 `json:"lng"`
	Lat     float64 `json:"lat"`
	Address string  `gorm:"size:255" json:"address"`
	Status  string  `gorm:"size:20;default:pending" json:"status"` // pending/processing/resolved
}

func (SOSAlert) TableName() string { return "sos_alerts" }

// RideTip 打赏司机（工单14）
type RideTip struct {
	BaseModel
	OrderNo      string  `gorm:"size:32;index" json:"orderNo"`
	PassengerID  uint    `json:"passengerId"`
	DriverID     uint    `json:"driverId"`
	Amount       float64 `json:"amount"`
	Thanks       string  `gorm:"size:255" json:"thanks"`
}

func (RideTip) TableName() string { return "ride_tips" }

// Invoice 发票申请（工单14）
type Invoice struct {
	BaseModel
	UserID  uint    `gorm:"index" json:"userId"`
	OrderNo string  `gorm:"size:32" json:"orderNo"`
	Type    string  `gorm:"size:20" json:"type"` // electronic/paper
	Title   string  `gorm:"size:128" json:"title"`
	TaxNo   string  `gorm:"size:32" json:"taxNo"`
	Email   string  `gorm:"size:128" json:"email"`
	Status  string  `gorm:"size:20;default:pending" json:"status"` // pending/issued
}

func (Invoice) TableName() string { return "invoices" }

// RideReview 行程评价（工单14：5星+评论+匿名+审核）
type RideReview struct {
	BaseModel
	OrderNo   string `gorm:"size:32;index" json:"orderNo"`
	PassengerID uint  `json:"passengerId"`
	DriverID  uint   `json:"driverId"`
	Rating    int    `gorm:"default:5" json:"rating"`      // 1~5 星
	Comment   string `gorm:"size:1024" json:"comment"`
	Anonymous bool   `gorm:"default:false" json:"anonymous"` // 匿名评价
	Status    string `gorm:"size:20;default:pending" json:"status"` // pending/approved/rejected（审核）
}

func (RideReview) TableName() string { return "ride_reviews" }

// CancelRecord 取消记录（工单14：限制滥用——频繁取消计数）
type CancelRecord struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	UserID     uint      `gorm:"index" json:"userId"`
	OrderNo    string    `gorm:"size:32" json:"orderNo"`
	Reason     string    `gorm:"size:255" json:"reason"`
	Compensation float64 `json:"compensation"`
	CreatedAt  time.Time `json:"createdAt"`
}

func (CancelRecord) TableName() string { return "cancel_records" }

// 工单：全栈开发-网约车-07-营销管理
// 优惠券类型、用户优惠券、推广发码、推荐奖励、收藏。
package model

import "time"

// 优惠券类型（工单07：类型多样化）
const (
	CouponDiscount = "discount" // 折扣券：按比例减免
	CouponCash     = "cash"     // 现金券：直接抵扣固定金额
	CouponFreeRide = "free"     // 免费乘车券：特定条件下一次免费乘车
)

// UserCoupon 状态
const (
	CouponUnused   = "unused"
	CouponUsed     = "used"
	CouponExpired  = "expired"
	CouponFrozen   = "frozen" // 部分退款冻结处理
)

// Coupon 优惠券模板（管理员创建与配置）
type Coupon struct {
	BaseModel
	Name        string     `gorm:"size:128;not null;index" json:"name"` // 名称
	Type        string     `gorm:"size:20;not null;index" json:"type"`  // discount/cash/free
	Value       float64    `json:"value"`                               // 折扣券: 折扣率(如0.8)；现金券: 抵扣金额；免费券: 最高免费额度
	MinConsume  float64    `json:"minConsume"`                          // 最低消费要求
	MaxDiscount float64    `json:"maxDiscount"`                         // 折扣券最高优惠上限
	City        string     `gorm:"size:64" json:"city"`                 // 适用城市（空=不限）
	VehicleType string     `gorm:"size:20" json:"vehicleType"`          // 适用车型 economy/luxury（空=不限）
	Stackable   bool       `json:"stackable"`                           // 是否可叠加使用（默认不可叠加）
	ValidFrom   time.Time  `json:"validFrom"`                           // 有效期起
	ValidTo     time.Time  `json:"validTo"`                             // 有效期止
	TotalCount  int        `json:"totalCount"`                          // 发放总量
	IssuedCount int        `json:"issuedCount"`                         // 已发放
	Status      string     `gorm:"size:20;default:online" json:"status"` // online/offline（发布状态）
	Desc        string     `gorm:"size:255" json:"desc"`
}

// UserCoupon 用户持有的优惠券
type UserCoupon struct {
	BaseModel
	UserID   uint      `gorm:"index;not null" json:"userId"`
	CouponID uint      `gorm:"index;not null" json:"couponId"`
	Coupon   *Coupon   `gorm:"foreignKey:CouponID" json:"coupon,omitempty"` // 关联模板（Preload 用）
	Code     string    `gorm:"size:32;uniqueIndex;not null" json:"code"` // 券码
	Status   string    `gorm:"size:20;index;default:unused" json:"status"`
	Source   string    `gorm:"size:20" json:"source"` // auto/manual/activity/referral（自动分配/手动发放/推广活动/推荐奖励）
	UsedAt   *time.Time `json:"usedAt"`
	OrderID  uint      `gorm:"index" json:"orderId"`
	ExpireAt time.Time `json:"expireAt"`
}

// CouponCode 推广活动优惠码（工单07：推广发码 + 推荐奖励）
type CouponCode struct {
	BaseModel
	Code       string     `gorm:"size:32;uniqueIndex;not null" json:"code"` // 优惠码
	CouponID   uint       `gorm:"index;not null" json:"couponId"`
	InviterUID uint       `gorm:"index" json:"inviterUid"` // 邀请人（推荐奖励双方得券）
	BindUID    uint       `gorm:"index" json:"bindUid"`    // 领取人
	Used       bool       `json:"used"`
	UsedAt     *time.Time `json:"usedAt"`
}

// CouponFavorite 优惠券收藏（工单07：收藏功能，方便日后查看和使用）
type CouponFavorite struct {
	BaseModel
	UserID   uint    `gorm:"index;not null" json:"userId"`
	CouponID uint    `gorm:"index;not null" json:"couponId"`
	Coupon   *Coupon `gorm:"foreignKey:CouponID" json:"coupon,omitempty"`
}

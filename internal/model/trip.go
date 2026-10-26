// 工单：全栈开发-网约车-01-顺风车管理
// 顺风车行程发布、紧急求助、行程分享、特殊需求。
package model

import "time"

// Trip 顺风车行程（乘客发布）
type Trip struct {
	BaseModel
	UserID       uint       `gorm:"index;not null" json:"userId"`  // 发布人（乘客）
	StartName    string     `gorm:"size:64" json:"startName"`      // 起点输入/选择
	EndName      string     `gorm:"size:64" json:"endName"`        // 终点输入/选择
	StartLng     float64    `json:"startLng"`
	StartLat     float64    `json:"startLat"`
	EndLng       float64    `json:"endLng"`
	EndLat       float64    `json:"endLat"`
	DepartTime   time.Time  `gorm:"index" json:"departTime"`       // 即时请求或未来预约
	Immediate    bool       `json:"immediate"`                     // true 即时出行 / false 预约
	SeatCount    int        `json:"seatCount"`
	SpecialNeeds string     `gorm:"size:255" json:"specialNeeds"`  // 特殊需求：婴儿座椅、宠物携带等
	Price        float64    `json:"price"`                         // 出价
	MatchedTrip  uint       `gorm:"index" json:"matchedTrip"`      // 匹配的司机行程（自动推荐算法结果）
	Status       string     `gorm:"size:20;index;default:published" json:"status"` // published/matched/ongoing/finished/cancelled
}

// DriverTrip 司机发布的顺风车行程（顺路接单）
type DriverTrip struct {
	BaseModel
	DriverID    uint      `gorm:"index;not null" json:"driverId"`
	VehicleID   uint      `json:"vehicleId"`
	StartName   string    `gorm:"size:64" json:"startName"`
	EndName     string    `gorm:"size:64" json:"endName"`
	DepartTime  time.Time `gorm:"index" json:"departTime"`
	Seats       int       `json:"seats"`
	Price       float64   `json:"price"`
	Status      string    `gorm:"size:20;default:published" json:"status"`
}

// SOSRecord 紧急求助记录（工单01：一键联系客服或当地警方）
type SOSRecord struct {
	BaseModel
	UserID    uint      `gorm:"index;not null" json:"userId"`
	TripID    uint      `gorm:"index" json:"tripId"` // 关联行程
	Type      string    `gorm:"size:20" json:"type"` // police/customer_service
	Location  string    `gorm:"size:255" json:"location"`
	Status    string    `gorm:"size:20;default:pending" json:"status"` // pending/handled/closed
	HandledBy uint      `json:"handledBy"`
	HandleNote string   `gorm:"size:255" json:"handleNote"`
}

// TripShare 行程分享（工单01：将行程详情分享给朋友或家人，增加安全感）
type TripShare struct {
	BaseModel
	TripID     uint       `gorm:"index;not null" json:"tripId"`
	ShareToken string     `gorm:"size:64;uniqueIndex;not null" json:"shareToken"` // 分享令牌
	ExpireAt   time.Time  `json:"expireAt"`
	ViewCount  int        `json:"viewCount"`
}

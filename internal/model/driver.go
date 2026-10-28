// 工单：全栈开发-网约车-01-顺风车管理 & 05-人员管理
// 司机资料、车辆信息、证件上传验证与年检/保险到期提醒。
package model

import "time"

// DriverProfile 司机扩展资料（工单01：实名制注册 + 证件验证；工单05：司机管理）
type DriverProfile struct {
	BaseModel
	UserID        uint   `gorm:"uniqueIndex;not null" json:"userId"`          // 关联用户
	LicenseNo     string `gorm:"size:32" json:"licenseNo"`                    // 驾驶证号
	LicenseExpiry *time.Time `json:"licenseExpiry"`                          // 驾照有效期
	ServiceScore  float64 `gorm:"default:5" json:"serviceScore"`            // 服务评分
	ReviewCount   int     `gorm:"default:0" json:"reviewCount"`            // 评价数（工单14）
	OnlineStatus  string `gorm:"size:20;default:offline" json:"onlineStatus"` // online/offline（工单05：工作状态）
	VerifyStatus  string `gorm:"size:20;default:pending" json:"verifyStatus"` // pending/approved/rejected 实名审核状态
}

// Vehicle 车辆信息（工单01：品牌、型号、颜色、车牌号、外观照片备案）
type Vehicle struct {
	BaseModel
	DriverID      uint   `gorm:"index;not null" json:"driverId"`        // 司机 UserID
	Brand         string `gorm:"size:64" json:"brand"`                  // 品牌
	Model         string `gorm:"size:64" json:"model"`                  // 型号
	Color         string `gorm:"size:32" json:"color"`                  // 颜色
	PlateNo       string `gorm:"size:20;uniqueIndex;not null" json:"plateNo"` // 车牌号
	PhotoURL      string `gorm:"size:255" json:"photoURL"`              // 车辆外观照片
	VehicleType   string `gorm:"size:20;default:economy" json:"vehicleType"`  // economy/luxury（工单07：车型适用范围）
	InspectionDue *time.Time `json:"inspectionDue"`                     // 年检到期（重要日期提醒）
	InsuranceDue  *time.Time `json:"insuranceDue"`                      // 保险到期（重要日期提醒）
	Status        string `gorm:"size:20;default:active" json:"status"`  // active/inactive
}

// DriverDoc 证件上传与验证（工单01：身份证件、驾驶证、行驶证官方文件验证）
type DriverDoc struct {
	BaseModel
	UserID       uint      `gorm:"index;not null" json:"userId"`
	DocType      string    `gorm:"size:20;not null" json:"docType"` // id_card/license/vehicle_license
	FileURL      string    `gorm:"size:255" json:"fileURL"`         // 证件文件地址（HTTPS 传输）
	VerifyStatus string    `gorm:"size:20;default:pending" json:"verifyStatus"` // pending/approved/rejected
	Remark       string    `gorm:"size:255" json:"remark"`
	VerifiedAt   *time.Time `json:"verifiedAt"`
}

// DueReminder 车辆年检/保险到期提醒（内存态计算返回，不落库）
type DueReminder struct {
	VehicleID uint   `json:"vehicleId"`
	PlateNo   string `json:"plateNo"`
	Type      string `json:"type"` // inspection/insurance
	DueDate   time.Time `json:"dueDate"`
	DaysLeft  int    `json:"daysLeft"`
}

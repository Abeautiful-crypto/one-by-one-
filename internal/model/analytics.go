// 工单：全栈开发-网约车-06-数据分析
// 用户访问/点击流数据（转化率漏斗分析基础数据）、异步导出任务、操作审计日志。
package model

import "time"

// UserVisit 用户行为记录（工单06：访问记录、页面路径、停留时间）
type UserVisit struct {
	BaseModel
	UserID     uint      `gorm:"index" json:"userId"` // 未注册访客为0
	DeviceID   string    `gorm:"size:64;index" json:"deviceId"`
	Page       string    `gorm:"size:64;index" json:"page"`  // home/service_select/vehicle_detail/pay...
	Action     string    `gorm:"size:20" json:"action"`      // view/click/stay
	StayMs     int       `json:"stayMs"`                     // 停留时间
	VisitedAt  time.Time `gorm:"index" json:"visitedAt"`
}

// ExportTask 异步导出任务（工单01：耗时导出后台异步处理，通知用户任务完成情况）
type ExportTask struct {
	BaseModel
	TaskNo    string     `gorm:"size:32;uniqueIndex;not null" json:"taskNo"`
	UserID    uint       `gorm:"index;not null" json:"userId"` // 谁执行了导出（操作日志，审计目的）
	BizType   string     `gorm:"size:32" json:"bizType"`       // orders/drivers/transactions...
	Params    string     `gorm:"size:500" json:"params"`
	Status    string     `gorm:"size:20;default:processing" json:"status"` // processing/done/failed
	FileURL   string     `gorm:"size:255" json:"fileURL"`                  // 下载链接（HTTPS 传输）
	ErrorMsg  string     `gorm:"size:500" json:"errorMsg"`                 // 失败原因（异常日志）
	FinishedAt *time.Time `json:"finishedAt"`
}

// OperationLog 操作审计日志（工单01：记录谁在何时导出/操作了哪些数据，用于审计）
type OperationLog struct {
	BaseModel
	UserID   uint       `gorm:"index" json:"userId"`
	Username string     `gorm:"size:64" json:"username"`
	Method   string     `gorm:"size:10" json:"method"`
	Path     string     `gorm:"size:255" json:"path"`
	IP       string     `gorm:"size:64" json:"ip"`
	Body     string     `gorm:"size:1000" json:"body"`
	Status   int        `json:"status"` // HTTP 状态码
	CostMs   int64      `json:"costMs"`
}

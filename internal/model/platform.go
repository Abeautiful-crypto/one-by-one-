// 工单09：GVA 框架能力配套模型（动态菜单、定时任务审计）
package model

import "time"

// Menu 动态菜单（工单09：后台可配置菜单，刷新多实例一致）
type Menu struct {
	BaseModel
	ParentID  uint   `gorm:"index" json:"parentId"`
	Name      string `gorm:"size:64" json:"name"`
	Path      string `gorm:"size:128" json:"path"`
	Component string `gorm:"size:128" json:"component"`
	Icon      string `gorm:"size:64" json:"icon"`
	Sort      int    `json:"sort"`
	Roles     string `gorm:"size:255" json:"roles"` // 逗号分隔可见角色
}

func (Menu) TableName() string { return "menus" }

// JobAudit 定时任务执行审计（工单09：Xxl Job 参数在线更新即时生效并记录审计历史）
type JobAudit struct {
	BaseModel
	JobName  string `gorm:"size:64;index" json:"jobName"`
	Operator string `gorm:"size:64" json:"operator"`
	Params   string `gorm:"type:text" json:"params"`
	Result   string `gorm:"size:64" json:"result"`
	DurationMs int64 `json:"durationMs"`
}

func (JobAudit) TableName() string { return "job_audits" }

// JobAuditRecord 用于列表返回（避免循环 import）
type JobAuditRecord struct {
	ID        uint      `json:"id"`
	JobName   string    `json:"jobName"`
	Operator  string    `json:"operator"`
	Params    string    `json:"params"`
	Result    string    `json:"result"`
	DurationMs int64    `json:"durationMs"`
	CreatedAt time.Time `json:"createdAt"`
}

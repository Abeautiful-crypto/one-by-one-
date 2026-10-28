// 工单09：GVA 框架能力接口（定时任务管理、动态菜单、审计日志）
package handler

import (
	"strconv"

	"ride-hailing/internal/middleware"
	"ride-hailing/internal/model"
	"ride-hailing/internal/pkg/resp"
	"ride-hailing/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// PlatformHandler 平台管理接口（工单09）
type PlatformHandler struct{}

// ListJobs 任务列表（含当前参数与最近执行结果）
func (h *PlatformHandler) ListJobs(c *gin.Context) {
	resp.OK(c, service.ListJobs())
}

type jobRunReq struct {
	Params map[string]interface{} `json:"params"`
}

// RunJob 立即执行任务
func (h *PlatformHandler) RunJob(c *gin.Context) {
	name := c.Param("name")
	var req jobRunReq
	_ = c.ShouldBindJSON(&req)
	if err := service.RunJob(name, middleware.GetUsername(c), req.Params); err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"message": "任务已执行", "job": name})
}

// UpdateJobParams 在线更新任务参数（即时生效）
func (h *PlatformHandler) UpdateJobParams(c *gin.Context) {
	name := c.Param("name")
	var req jobRunReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数错误")
		return
	}
	if err := service.UpdateJobParams(name, req.Params, middleware.GetUsername(c)); err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"message": "参数已更新并即时生效", "job": name})
}

// JobAudits 任务审计历史
func (h *PlatformHandler) JobAudits(c *gin.Context) {
	resp.OK(c, service.ListJobAudits(c.Query("jobName")))
}

// MenuList 动态菜单列表（工单09：后台配置、刷新多实例一致）
func (h *PlatformHandler) MenuList(c *gin.Context) {
	var list []model.Menu
	model.DB.Order("sort asc").Find(&list)
	resp.OK(c, buildMenuTree(list, 0))
}

func buildMenuTree(list []model.Menu, parentID uint) []gin.H {
	out := []gin.H{}
	for _, m := range list {
		if m.ParentID == parentID {
			node := gin.H{
				"id": m.ID, "name": m.Name, "path": m.Path, "component": m.Component,
				"icon": m.Icon, "sort": m.Sort, "roles": m.Roles,
				"children": buildMenuTree(list, m.ID),
			}
			out = append(out, node)
		}
	}
	return out
}

type menuReq struct {
	ParentID  uint   `json:"parentId"`
	Name      string `json:"name" binding:"required"`
	Path      string `json:"path"`
	Component string `json:"component"`
	Icon      string `json:"icon"`
	Sort      int    `json:"sort"`
	Roles     string `json:"roles"`
}

// SaveMenu 新增/更新菜单（动态配置）
func (h *PlatformHandler) SaveMenu(c *gin.Context) {
	var req menuReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	menu := model.Menu{
		ParentID: req.ParentID, Name: req.Name, Path: req.Path,
		Component: req.Component, Icon: req.Icon, Sort: req.Sort, Roles: req.Roles,
	}
	if id, _ := parseUint(c.Query("id")); id > 0 {
		menu.ID = id
		model.DB.Model(&model.Menu{}).Where("id = ?", id).Updates(menu)
	} else {
		model.DB.Create(&menu)
	}
	resp.OK(c, menu)
}

// AuditLogs 操作/权限审计日志（工单09：权限拦截点日志可追溯）
func (h *PlatformHandler) AuditLogs(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	pageSize := 20
	offset := (page - 1) * pageSize
	var list []model.OrderAudit
	var total int64
	model.DB.Model(&model.OrderAudit{}).Count(&total)
	model.DB.Order("created_at desc").Offset(offset).Limit(pageSize).Find(&list)
	resp.OK(c, gin.H{"list": list, "total": total, "page": page, "pageSize": pageSize})
}

var _ = gorm.ErrRecordNotFound

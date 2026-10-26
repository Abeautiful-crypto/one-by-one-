// 工单：全栈开发-网约车-01-顺风车管理（性能优化）/ 03-财务管理
// 异步导出接口：创建任务、查询状态、下载文件（权限控制：仅任务创建人可下载）。
package handler

import (
	"errors"
	"os"
	"path/filepath"

	"ride-hailing/internal/middleware"
	"ride-hailing/internal/model"
	"ride-hailing/internal/pkg/resp"
	"ride-hailing/internal/service"

	"github.com/gin-gonic/gin"
)

type ExportHandler struct{}

type createExportReq struct {
	BizType string `json:"bizType" binding:"required,oneof=orders drivers transactions"`
	Params  string `json:"params"`
}

// CreateTask 创建异步导出任务（工单01：后台异步处理，通知用户任务完成情况）
func (h *ExportHandler) CreateTask(c *gin.Context) {
	var req createExportReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	task, err := service.CreateExportTask(middleware.GetUserID(c), req.BizType, req.Params)
	if err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"taskNo": task.TaskNo, "status": task.Status})
}

// GetTask 查询任务状态
func (h *ExportHandler) GetTask(c *gin.Context) {
	task, err := service.GetExportTask(c.Param("taskNo"), middleware.GetUserID(c))
	if err != nil {
		resp.NotFound(c, "任务不存在")
		return
	}
	resp.OK(c, task)
}

// ListTasks 我的导出任务列表
func (h *ExportHandler) ListTasks(c *gin.Context) {
	var list []model.ExportTask
	model.DB.Where("user_id = ?", middleware.GetUserID(c)).Order("id DESC").Limit(50).Find(&list)
	resp.OK(c, list)
}

// Download 下载导出文件（HTTPS 协议传输）
func (h *ExportHandler) Download(c *gin.Context) {
	task, err := service.GetExportTask(c.Param("taskNo"), middleware.GetUserID(c))
	if err != nil {
		resp.NotFound(c, "任务不存在")
		return
	}
	if task.Status != "done" {
		resp.BadRequest(c, "任务尚未完成: "+task.Status)
		return
	}
	if task.FileURL == "" {
		resp.BadRequest(c, "文件不可用")
		return
	}
	path := filepath.Join(service.ExportDir(), filepath.Base(task.FileURL))
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		resp.NotFound(c, "导出文件已清理")
		return
	}
	c.File(path) // 注意：生产环境经 HTTPS 传输并考虑对导出文件本身加密保护
}

// 工单：全栈开发-网约车-05-人员管理
// 人员管理接口：员工管理（企业内部人员）、司机管理（顺风车司机）、用户管理（乘客）。
// 支持：关键字搜索、分页、批量启用/禁用/删除、CSV/JSON 导入校验、多角色绑定。
// 调度员有独立管理界面（独立账号登录，角色校验见路由）。
package handler

import (
	"errors"

	"ride-hailing/internal/model"
	"ride-hailing/internal/pkg/resp"
	"ride-hailing/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type PersonnelHandler struct{}

type userPageReq struct {
	resp.PageQuery
	Keyword  string `form:"keyword"`  // 姓名/手机号/用户名关键字搜索
	Role     string `form:"role"`     // 角色编码筛选
	Status   string `form:"status"`   // enabled/disabled
}

// ListUsers 分页 + 关键字搜索（工单05：司机信息展示与关键字搜索）
func (h *PersonnelHandler) ListUsers(c *gin.Context) {
	var req userPageReq
	if err := c.ShouldBindQuery(&req); err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	req.Normalize()
	q := model.DB.Model(&model.User{})
	if req.Keyword != "" {
		kw := "%" + req.Keyword + "%"
		q = q.Where("real_name LIKE ? OR phone LIKE ? OR username LIKE ? OR email LIKE ?", kw, kw, kw, kw)
	}
	if req.Status != "" {
		q = q.Where("status = ?", req.Status)
	}
	if req.Role != "" {
		q = q.Joins("JOIN user_roles ur ON ur.user_id = users.id JOIN roles r ON r.id = ur.role_id").
			Where("r.code = ?", req.Role)
	}
	var total int64
	q.Count(&total)
	var users []model.User
	if err := q.Preload("Roles").
		Omit("password").
		Limit(req.PageSize).Offset((req.Page - 1) * req.PageSize).
		Order("id DESC").Find(&users).Error; err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, resp.PageResult{List: users, Total: total, Page: req.Page, PageSize: req.PageSize})
}

type upsertUserReq struct {
	Username string   `json:"username" binding:"required,min=3,max=64"`
	Password string   `json:"password" binding:"omitempty,min=6"`
	RealName string   `json:"realName" binding:"required"`
	Phone    string   `json:"phone" binding:"required,len=11"`
	Email    string   `json:"email" binding:"omitempty,email"`
	IdCard   string   `json:"idCard" binding:"omitempty,len=18"`
	Status   string   `json:"status" binding:"omitempty,oneof=enabled disabled"`
	Roles    []string `json:"roles" binding:"omitempty,dive,required"` // 多角色
}

// CreateUser 设置员工（工单05：设置员工；多角色绑定）
func (h *PersonnelHandler) CreateUser(c *gin.Context) {
	var req upsertUserReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	pwd := req.Password
	if pwd == "" {
		pwd = "Abc@123456"
	}
	if err := service.CreateUserWithRole(req.Username, pwd, req.RealName, req.Phone, req.Email, req.IdCard, req.Roles); err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"message": "创建成功"})
}

// UpdateUser 更新用户（含角色调整）
func (h *PersonnelHandler) UpdateUser(c *gin.Context) {
	id := c.Param("id")
	var req upsertUserReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := tx.First(&user, id).Error; err != nil {
			return err
		}
		user.RealName, user.Phone, user.Email, user.IdCard = req.RealName, req.Phone, req.Email, req.IdCard
		if req.Status != "" {
			user.Status = req.Status
		}
		if req.Password != "" {
			if err := user.SetPassword(req.Password); err != nil {
				return err
			}
		}
		if err := tx.Save(&user).Error; err != nil {
			return err
		}
		if len(req.Roles) > 0 {
			if err := tx.Where("user_id = ?", user.ID).Delete(&model.UserRole{}).Error; err != nil {
				return err
			}
			return service.BindRolesPublic(tx, user.ID, req.Roles)
		}
		return nil
	})
	if err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"message": "更新成功"})
}

type batchUserReq struct {
	IDs   []uint `json:"ids" binding:"required,min=1"`
	Action string `json:"action" binding:"required,oneof=enable disable delete"` // 批量操作
}

// BatchUsers 批量启用/禁用/删除（工单05：批量操作）
func (h *PersonnelHandler) BatchUsers(c *gin.Context) {
	var req batchUserReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	var affected int64
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		switch req.Action {
		case "enable":
			r := tx.Model(&model.User{}).Where("id IN ?", req.IDs).Update("status", "enabled")
			affected = r.RowsAffected
			return r.Error
		case "disable":
			r := tx.Model(&model.User{}).Where("id IN ?", req.IDs).Update("status", "disabled")
			affected = r.RowsAffected
			return r.Error
		default: // delete
			r := tx.Where("id IN ?", req.IDs).Delete(&model.User{})
			affected = r.RowsAffected
			if err := tx.Where("user_id IN ?", req.IDs).Delete(&model.UserRole{}).Error; err != nil {
				return err
			}
			return r.Error
		}
	})
	if err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"affected": affected})
}

type importReq struct {
	Role string `form:"role"` // 默认导入角色
}

// ImportUsers 数据导入（工单05：CSV/Excel、JSON 文件支持 + 数据验证与清洗 + 错误报告）
func (h *PersonnelHandler) ImportUsers(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		resp.BadRequest(c, "请上传文件字段 file（支持 .csv / .json）")
		return
	}
	if err := checkImportExt(file.Filename); err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	f, err := file.Open()
	if err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	defer f.Close()

	var rows []service.ImportPersonRow
	switch {
	case hasExt(file.Filename, ".json"):
		rows, err = service.ParsePersonJSON(f)
	default:
		rows, err = service.ParsePersonCSV(f)
	}
	if err != nil {
		resp.BadRequest(c, "文件解析失败: "+err.Error())
		return
	}
	if h.role(c) == "" && c.Query("role") != "" {
		// 导入统一角色（可选）
		for i := range rows {
			if rows[i].Role == "" {
				rows[i].Role = c.Query("role")
			}
		}
	}
	result := service.ImportPersons(rows)
	resp.OK(c, result) // 详细的错误报告：列出所有不符合要求的数据项及修改建议
}

func (h *PersonnelHandler) role(c *gin.Context) string { return "" }

// ListRoles 角色列表（RBAC 配置）
func (h *PersonnelHandler) ListRoles(c *gin.Context) {
	var roles []model.Role
	model.DB.Order("id").Find(&roles)
	resp.OK(c, roles)
}

// ListDrivers 司机列表（工单05：司机列表功能）
func (h *PersonnelHandler) ListDrivers(c *gin.Context) {
	var req userPageReq
	if err := c.ShouldBindQuery(&req); err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	req.Normalize()
	q := model.DB.Model(&model.DriverProfile{}).
		Joins("JOIN users ON users.id = driver_profiles.user_id")
	if req.Keyword != "" {
		kw := "%" + req.Keyword + "%"
		q = q.Where("users.real_name LIKE ? OR users.phone LIKE ?", kw, kw)
	}
	var total int64
	q.Count(&total)
	type driverRow struct {
		model.DriverProfile
		Username string `json:"username"`
		RealName string `json:"realName"`
		Phone    string `json:"phone"`
		Status   string `json:"status"`
	}
	var rows []driverRow
	if err := q.Select("driver_profiles.*, users.username, users.real_name, users.phone, users.status").
		Limit(req.PageSize).Offset((req.Page - 1) * req.PageSize).
		Order("driver_profiles.id DESC").Scan(&rows).Error; err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, resp.PageResult{List: rows, Total: total, Page: req.Page, PageSize: req.PageSize})
}

// checkImportExt / hasExt 文件格式支持：CSV / JSON（工单05）
func checkImportExt(name string) error {
	if hasExt(name, ".csv") || hasExt(name, ".json") || hasExt(name, ".txt") {
		return nil
	}
	return errors.New("仅支持 .csv / .json 文件")
}

func hasExt(name, ext string) bool {
	return len(name) >= len(ext) && name[len(name)-len(ext):] == ext
}

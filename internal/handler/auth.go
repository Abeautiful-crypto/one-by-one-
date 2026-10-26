// 工单：全栈开发-网约车-05-人员管理 / 01-顺风车管理（实名制注册）
// 认证接口：登录、注册（实名制）、当前用户信息。
package handler

import (
	"errors"
	"time"

	"ride-hailing/internal/config"
	"ride-hailing/internal/middleware"
	"ride-hailing/internal/model"
	"ride-hailing/internal/pkg/jwtutil"
	"ride-hailing/internal/pkg/resp"
	"ride-hailing/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type AuthHandler struct{ Cfg *config.Config }

type loginReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Login 登录签发 JWT（含角色，RBAC）
func (h *AuthHandler) Login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	var user model.User
	if err := model.DB.Where("username = ?", req.Username).First(&user).Error; err != nil {
		resp.Unauthorized(c, "用户名或密码错误")
		return
	}
	if !user.CheckPassword(req.Password) {
		resp.Unauthorized(c, "用户名或密码错误")
		return
	}
	if user.Status != "enabled" {
		resp.Forbidden(c, "账号已被禁用")
		return
	}
	var roles []model.Role
	model.DB.Model(&user).Association("Roles").Find(&roles)
	codes := make([]string, 0, len(roles))
	for _, r := range roles {
		codes = append(codes, r.Code)
	}
	token, err := jwtutil.GenerateToken(user.ID, user.Username, codes, h.Cfg.JWTSecret)
	if err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	now := time.Now()
	model.DB.Model(&user).Update("last_login", &now)
	resp.OK(c, gin.H{"token": token, "user": gin.H{
		"id": user.ID, "username": user.Username, "realName": user.RealName, "roles": codes,
	}})
}

type registerReq struct {
	Username string `json:"username" binding:"required,min=3,max=64"`
	Password string `json:"password" binding:"required,min=6"`
	RealName string `json:"realName" binding:"required"` // 实名制注册：提供真实身份信息
	Phone    string `json:"phone" binding:"required,len=11"`
	IdCard   string `json:"idCard" binding:"required,len=18"` // 身份证号实名验证
	Role     string `json:"role" binding:"omitempty,oneof=passenger carpool_driver bus_driver"`
}

// Register 实名制注册（工单01：所有用户进行实名制注册，提供真实身份信息）
func (h *AuthHandler) Register(c *gin.Context) {
	var req registerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "注册参数校验失败: "+err.Error())
		return
	}
	role := req.Role
	if role == "" {
		role = "passenger"
	}
	if err := service.CreateUserWithRole(req.Username, req.Password, req.RealName, req.Phone, "", req.IdCard, []string{role}); err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"message": "注册成功，请登录"})
}

// Me 当前登录用户信息
func (h *AuthHandler) Me(c *gin.Context) {
	var user model.User
	if err := model.DB.Preload("Roles").First(&user, middleware.GetUserID(c)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			resp.NotFound(c, "用户不存在")
			return
		}
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, user)
}

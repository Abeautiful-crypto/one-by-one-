// 工单：全栈开发-网约车-05-人员管理 / 01-顺风车管理
// RBAC 权限控制中间件：图片要求"多角色管理模型；独立管理界面"。
// RequireRole 校验当前用户至少拥有其中一个角色；RequireAdmin 仅管理员。
package middleware

import (
	"ride-hailing/internal/pkg/resp"

	"github.com/gin-gonic/gin"
)

// RequireRole 角色校验（任一命中即通过）
func RequireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userRoles := GetRoles(c)
		for _, need := range roles {
			for _, has := range userRoles {
				if need == has {
					c.Next()
					return
				}
			}
		}
		resp.Forbidden(c, "无权限访问该资源")
		c.Abort()
	}
}

// RequireAdmin 管理端接口统一要求 admin（或指定放行的管理角色）
func RequireAdmin() gin.HandlerFunc {
	return RequireRole("admin")
}

// RequireStaff 管理端通用：admin、调度员、验票员等内部员工角色
func RequireStaff() gin.HandlerFunc {
	return RequireRole("admin", "dispatcher", "ticket_checker", "contact")
}

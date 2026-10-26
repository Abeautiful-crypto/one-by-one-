// 工单：全栈开发-网约车-05-人员管理
// JWT 认证中间件：解析令牌并将用户信息注入上下文。
package middleware

import (
	"strings"

	"ride-hailing/internal/config"
	"ride-hailing/internal/pkg/jwtutil"
	"ride-hailing/internal/pkg/resp"

	"github.com/gin-gonic/gin"
)

const (
	CtxUserID   = "ctxUserID"
	CtxUsername = "ctxUsername"
	CtxRoles    = "ctxRoles"
)

// JWTAuth 校验 Authorization: Bearer <token>
func JWTAuth(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			resp.Unauthorized(c, "缺少认证令牌")
			c.Abort()
			return
		}
		claims, err := jwtutil.ParseToken(strings.TrimPrefix(h, "Bearer "), cfg.JWTSecret)
		if err != nil {
			resp.Unauthorized(c, "令牌无效或已过期")
			c.Abort()
			return
		}
		c.Set(CtxUserID, claims.UserID)
		c.Set(CtxUsername, claims.Username)
		c.Set(CtxRoles, claims.Roles)
		c.Next()
	}
}

func GetUserID(c *gin.Context) uint {
	if v, ok := c.Get(CtxUserID); ok {
		return v.(uint)
	}
	return 0
}

func GetUsername(c *gin.Context) string {
	if v, ok := c.Get(CtxUsername); ok {
		return v.(string)
	}
	return ""
}

func GetRoles(c *gin.Context) []string {
	if v, ok := c.Get(CtxRoles); ok {
		return v.([]string)
	}
	return nil
}

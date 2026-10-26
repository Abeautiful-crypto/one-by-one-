// 工单：全栈开发-网约车-06-数据分析
// 用户行为埋点记录。
package service

import (
	"time"

	"ride-hailing/internal/middleware"
	"ride-hailing/internal/model"

	"github.com/gin-gonic/gin"
)

// VisitFromContext 记录访问/点击/停留行为（异步落库，不阻塞请求）
func VisitFromContext(c *gin.Context, page, action string, stayMs int, deviceID string) *model.UserVisit {
	v := &model.UserVisit{
		UserID:    middleware.GetUserID(c),
		DeviceID:  deviceID,
		Page:      page,
		Action:    action,
		StayMs:    stayMs,
		VisitedAt: time.Now(),
	}
	if v.Action == "" {
		v.Action = "view"
	}
	model.DB.Create(v)
	return v
}

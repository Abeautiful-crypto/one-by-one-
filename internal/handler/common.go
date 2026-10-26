// 工单：全栈开发-网约车-公共
// handler 公共工具函数。
package handler

import (
	"strconv"
	"time"

	"ride-hailing/internal/model"
	"ride-hailing/internal/pkg/cache"
)

// parseUint 字符串转 uint
func parseUint(s string) (uint, error) {
	v, err := strconv.ParseUint(s, 10, 64)
	return uint(v), err
}

// parseTimeStr 解析 RFC3339 时间
func parseTimeStr(s string) (time.Time, error) {
	return time.Parse(time.RFC3339, s)
}

// AppCache 全局缓存实例（工单07：频繁访问但不常变动的数据使用缓存；可替换 Redis）
var AppCache = cache.NewMemoryCache()

// changeOrderStatus 订单状态流转 + 状态历史记录（工单04：历史变更记录）
func changeOrderStatus(order *model.Order, toStatus, reason, operator string) error {
	from := order.Status
	order.Status = toStatus
	err := model.DB.Save(order).Error
	if err != nil {
		return err
	}
	return model.DB.Create(&model.OrderStatusLog{
		OrderID: order.ID, FromStatus: from, ToStatus: toStatus, Reason: reason, Operator: operator,
	}).Error
}

// 工单：全栈开发-网约车-04-订单管理（批量退票/退票规则）
// 退票取消费策略 —— 策略模式实现：
//   - 全额退款情形：不可抗力事件导致的服务中断（工单04 规则定义）
//   - 按时间段取消费：根据平台设定的不同时间段内的取消费标准自动计算应扣款项
//   - 部分退款逻辑：明确计算方法及适用条件
package service

import (
	"fmt"
	"time"

	"ride-hailing/internal/model"
)

// RefundPolicy 退款策略接口（策略模式）
type RefundPolicy interface {
	Name() string
	// Calculate 计算退款金额与取消费；departTime 为发车/出发时间，amount 为订单金额
	Calculate(amount float64, departTime time.Time, now time.Time) (refundAmount, deductFee float64)
}

// FullRefundPolicy 全额退款策略：不可抗力
type FullRefundPolicy struct{}

func (FullRefundPolicy) Name() string { return "force_major_full_refund" }
func (FullRefundPolicy) Calculate(amount float64, _ time.Time, _ time.Time) (float64, float64) {
	return amount, 0
}

// TimeBasedPolicy 按时间段的取消费策略（部分退款逻辑）
type TimeBasedPolicy struct {
	// 距发车 24 小时以上：免收取消费
	// 2~24 小时：收取 50% 取消费
	// 2 小时以内：收取 100% 取消费（不退款）
}

func (TimeBasedPolicy) Name() string { return "time_based_fee" }

func (TimeBasedPolicy) Calculate(amount float64, departTime time.Time, now time.Time) (float64, float64) {
	switch d := departTime.Sub(now); {
	case d > 24*time.Hour:
		return amount, 0
	case d > 2*time.Hour:
		fee := amount * 0.5
		return amount - fee, fee
	default:
		return 0, amount
	}
}

// refundPolicyFactory 工厂：根据场景选择策略
func resolvePolicy(forceMajor bool) RefundPolicy {
	if forceMajor {
		return FullRefundPolicy{}
	}
	return TimeBasedPolicy{}
}

// CalcRefund 计算退票金额（供自动审核与批量退票调用）
func CalcRefund(order *model.Order, forceMajor bool) (refundAmount, deductFee float64) {
	depart := time.Now().Add(48 * time.Hour) // 兜底：无明确出发时间默认 48h 后
	if order.OrderType == model.OrderTypeBus && order.ID > 0 {
		var shift model.Shift
		if err := model.DB.First(&shift, order.ShiftID).Error; err == nil {
			depart = shift.DepartTime
		}
	}
	if order.TripID > 0 {
		var trip model.Trip
		if err := model.DB.First(&trip, order.TripID).Error; err == nil {
			depart = trip.DepartTime
		}
	}
	return resolvePolicy(forceMajor).Calculate(order.PaidAmount, depart, time.Now())
}

// FormatPolicyDesc 输出策略说明（审计追踪用）
func FormatPolicyDesc(forceMajor bool) string {
	return fmt.Sprintf("退票策略: %s", resolvePolicy(forceMajor).Name())
}

// 工单：全栈开发-网约车-07-营销管理
// 优惠券计算策略 —— 策略模式 + 工厂模式：
//   - 折扣券：按比例减免，设置最高优惠上限
//   - 现金券：直接抵扣固定金额
//   - 免费乘车券：特定条件下一次免费乘车
// 使用规则校验：最低消费、不可叠加、适用范围（城市/车型）、有效期。
package service

import (
	"errors"
	"fmt"
	"time"

	"ride-hailing/internal/model"
)

// CouponStrategy 优惠券策略接口（策略模式）
type CouponStrategy interface {
	Type() string
	// Calc 计算优惠金额；amount 为订单金额，c 为优惠券模板
	Calc(c *model.Coupon, amount float64) (discount float64, err error)
}

// DiscountStrategy 折扣券
type DiscountStrategy struct{}

func (DiscountStrategy) Type() string { return model.CouponDiscount }
func (DiscountStrategy) Calc(c *model.Coupon, amount float64) (float64, error) {
	if c.Value <= 0 || c.Value >= 1 {
		return 0, errors.New("折扣率配置非法，应在 (0,1) 区间")
	}
	d := amount * (1 - c.Value)
	if c.MaxDiscount > 0 && d > c.MaxDiscount {
		d = c.MaxDiscount
	}
	return round2(d), nil
}

// CashStrategy 现金券
type CashStrategy struct{}

func (CashStrategy) Type() string { return model.CouponCash }
func (CashStrategy) Calc(c *model.Coupon, amount float64) (float64, error) {
	if c.Value > amount {
		return 0, errors.New("现金券面值不能超过订单金额")
	}
	return round2(c.Value), nil
}

// FreeRideStrategy 免费乘车券（特定条件下一次免费乘车，不超过最高免费额度）
type FreeRideStrategy struct{}

func (FreeRideStrategy) Type() string { return model.CouponFreeRide }
func (FreeRideStrategy) Calc(c *model.Coupon, amount float64) (float64, error) {
	if amount > c.Value && c.Value > 0 {
		return round2(c.Value), nil // 超出部分需用户补差
	}
	return round2(amount), nil
}

// couponStrategyRegistry 策略注册表（工厂模式）
var couponStrategyRegistry = map[string]CouponStrategy{
	model.CouponDiscount: DiscountStrategy{},
	model.CouponCash:     CashStrategy{},
	model.CouponFreeRide: FreeRideStrategy{},
}

// GetCouponStrategy 工厂方法：按类型获取策略
func GetCouponStrategy(t string) (CouponStrategy, error) {
	s, ok := couponStrategyRegistry[t]
	if !ok {
		return nil, fmt.Errorf("未知优惠券类型: %s", t)
	}
	return s, nil
}

// ValidateAndCalc 使用规则校验并计算优惠：
// 有效期、状态、最低消费、适用范围（城市/车型）、不可叠加在调用方保证（同一订单仅传一张券）。
func ValidateAndCalc(c *model.Coupon, amount float64, city, vehicleType string) (float64, error) {
	now := time.Now()
	if now.Before(c.ValidFrom) || now.After(c.ValidTo) {
		return 0, errors.New("优惠券不在有效期内")
	}
	if c.Status != "online" {
		return 0, errors.New("优惠券未发布或已下线")
	}
	if c.MinConsume > 0 && amount < c.MinConsume {
		return 0, fmt.Errorf("未达到最低消费金额 %.2f", c.MinConsume)
	}
	if c.City != "" && city != "" && c.City != city {
		return 0, errors.New("优惠券不适用于当前城市")
	}
	if c.VehicleType != "" && vehicleType != "" && c.VehicleType != vehicleType {
		return 0, errors.New("优惠券不适用于当前车型")
	}
	s, err := GetCouponStrategy(c.Type)
	if err != nil {
		return 0, err
	}
	return s.Calc(c, amount)
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

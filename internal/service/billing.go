// 计费引擎（工单16：行程计费规则与账单结算；工单10：雨天费用浮动）
// 费用项口径：起步价、里程费、时长费、等待费、高速费、夜间加价、空驶补偿
package service

import (
	"math"
	"time"

	"ride-hailing/internal/model"
)

// 计价参数（平台标准，工单16 计费从“行程开始”启动）
const (
	dayBaseFare    = 13.0  // 白天起步价（含3公里）
	nightBaseFare  = 16.8  // 夜间起步价（含3公里）
	dayMileageRate = 2.3   // 白天里程费 元/km（超出3公里部分）
	nightMileageRate = 2.76 // 夜间里程费 元/km
	durationRate   = 0.5   // 时长费 元/分钟
	waitRate       = 0.5   // 等待费 元/分钟
	nightSurcharge = 0.2   // 夜间加价系数（22:00~7:00）
	baseKm         = 3.0   // 起步价含里程
	idleCompensation = 5.0 // 行程开始前取消：司机空驶补偿（工单16）
	compensationPerKm = 1.0 // 行程开始后取消：按已行驶里程补偿
)

// activeRainFactor 工单10：雨天浮动（黄色+10%，橙/红+15%），无预警返回 1
func activeRainFactor(city string) (float64, *model.WeatherAlert) {
	var a model.WeatherAlert
	now := time.Now()
	err := model.DB.Where("city = ? AND active_until > ?", city, now).
		Order("active_until desc").First(&a).Error
	if err != nil {
		return 1, nil
	}
	switch a.Level {
	case "yellow":
		return 1.10, &a
	case "orange", "red":
		return 1.15, &a
	}
	return 1, nil
}

// CalcBill 行程账单计算（工单16：到达目的地后停止计费并计算最终费用）
func CalcBill(o *model.RideOrder, tripMinutes, waitMinutes, tollFare float64) *model.TripBill {
	start := time.Now()
	if o.StartAt != nil {
		start = *o.StartAt
	}
	night := isNight(start, 7, 22)
	base, mileageRate := dayBaseFare, dayMileageRate
	if night {
		base, mileageRate = nightBaseFare, nightMileageRate
	}

	// 里程费：超出起步价含程部分
	extraKm := math.Max(o.MileageKm-baseKm, 0)
	mileageFare := extraKm * mileageRate
	// 时长费 + 等待费
	durationFare := tripMinutes * durationRate
	waitFare := waitMinutes * waitRate
	// 夜间加价：起步+里程部分的 20%
	nightExtra := 0.0
	if night {
		nightExtra = (base + mileageFare) * nightSurcharge
	}

	// 工单10：雨天浮动作用于（起步+里程+时长）
	rain, alert := activeRainFactor(o.City)
	subtotal := base + mileageFare + durationFare
	total := subtotal*rain + waitFare + tollFare + nightExtra
	total = math.Round(total*100) / 100

	bill := &model.TripBill{
		OrderID: o.ID, OrderNo: o.OrderNo,
		BaseFare: base, MileageFare: math.Round(mileageFare*100) / 100,
		DurationFare: math.Round(durationFare*100) / 100,
		WaitFare: math.Round(waitFare*100) / 100,
		TollFare: tollFare, NightExtra: math.Round(nightExtra*100) / 100,
		Total: total, IsNight: night,
	}
	if bill.Compensation > 0 {
		bill.Total += bill.Compensation
	}
	_ = alert
	return bill
}

// CalcCancelCompensation 乘客取消订单补偿（工单16：按取消时间点计算空驶费）
// 规则：行程开始(ongoing)前取消 → 固定空驶补偿 5 元；
//       行程开始后取消 → 起步价 + 已行驶里程×补偿单价（上限 50 元）
func CalcCancelCompensation(o *model.RideOrder) float64 {
	if o.StartAt == nil {
		return idleCompensation
	}
	c := dayBaseFare + math.Max(o.MileageKm, 0)*compensationPerKm
	return math.Min(math.Round(c*100)/100, 50)
}

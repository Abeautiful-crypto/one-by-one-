// 工单：全栈开发-网约车-06-数据分析
// 数据分析服务：订单量分析（总量/增长率/平均订单值）、用户转化率（漏斗）、复购周期分析。
// 验收标准：汇总功能响应时间 5 秒内（通过 SQL 聚合 + 索引保障）。
package service

import (
	"fmt"
	"time"

	"ride-hailing/internal/model"
)

// OrderTrend 订单量趋势
type OrderTrend struct {
	Period     string  `json:"period"` // 2026-10-26 / 2026-W43 / 2026-10
	OrderCount int64   `json:"orderCount"`
	Amount     float64 `json:"amount"`
}

// OrderAnalysis 订单量分析结果（工单06）
type OrderAnalysis struct {
	Granularity   string       `json:"granularity"` // day/week/month
	TotalOrders   int64        `json:"totalOrders"`
	TotalAmount   float64      `json:"totalAmount"`
	AvgOrderValue float64      `json:"avgOrderValue"` // 平均订单值
	GrowthRate    float64      `json:"growthRate"`    // 订单增长率 %（与上一周期对比）
	ValidOrders   int64        `json:"validOrders"`   // 有效订单总数（非取消）
	InvalidOrders int64        `json:"invalidOrders"` // 无效订单总数（取消）
	CouponOrders  int64        `json:"couponOrders"`  // 使用优惠券订单总数
	Trend         []OrderTrend `json:"trend"`
}

// AnalyzeOrders 订单量统计（按日/周/月）
func AnalyzeOrders(granularity string, start, end time.Time) (*OrderAnalysis, error) {
	if start.IsZero() || end.IsZero() {
		end = time.Now()
		start = end.AddDate(0, -1, 0)
	}
	a := &OrderAnalysis{Granularity: granularity}

	// 有效/无效订单（工单06 验收：无效订单总数、有效订单总数）
	model.DB.Model(&model.Order{}).Where("created_at >= ? AND created_at < ? AND status <> ?", start, end, model.OrderCancelled).Count(&a.ValidOrders)
	model.DB.Model(&model.Order{}).Where("created_at >= ? AND created_at < ? AND status = ?", start, end, model.OrderCancelled).Count(&a.InvalidOrders)
	a.TotalOrders = a.ValidOrders + a.InvalidOrders
	// 使用优惠券订单总数
	model.DB.Model(&model.Order{}).Where("created_at >= ? AND created_at < ? AND coupon_id > 0", start, end).Count(&a.CouponOrders)

	var amt struct{ Sum float64 }
	model.DB.Model(&model.Order{}).
		Where("created_at >= ? AND created_at < ? AND status IN ?", start, end, []string{model.OrderPaid, model.OrderOngoing, model.OrderFinished}).
		Select("COALESCE(SUM(paid_amount),0) AS sum").Scan(&amt)
	a.TotalAmount = round2(amt.Sum)
	if a.ValidOrders > 0 {
		a.AvgOrderValue = round2(a.TotalAmount / float64(a.ValidOrders))
	}

	// 上一周期对比计算增长率
	dur := end.Sub(start)
	var prevCnt int64
	model.DB.Model(&model.Order{}).
		Where("created_at >= ? AND created_at < ? AND status <> ?", start.Add(-dur), start, model.OrderCancelled).
		Count(&prevCnt)
	if prevCnt > 0 {
		a.GrowthRate = round2(float64(a.ValidOrders-prevCnt) / float64(prevCnt) * 100)
	}

	// 分组趋势
	fmtStr := "%Y-%m-%d"
	switch granularity {
	case "week":
		fmtStr = "%x-W%v"
	case "month":
		fmtStr = "%Y-%m"
	}
	model.DB.Model(&model.Order{}).
		Where("created_at >= ? AND created_at < ?", start, end).
		Select(fmt.Sprintf("strftime('%s', created_at) AS period, COUNT(*) AS order_count, COALESCE(SUM(paid_amount),0) AS amount", fmtStr)).
		Group("period").Order("period").Scan(&a.Trend)
	return a, nil
}

// ConversionAnalysis 用户转化率分析（工单06）
// 转化率 =（产生购买行为的客户人数 ÷ 所有注册 app 的人数）× 100%
type ConversionAnalysis struct {
	RegisteredUsers int64            `json:"registeredUsers"` // 注册人数
	BuyUsers        int64            `json:"buyUsers"`        // 产生购买行为的客户人数
	ConversionRate  float64          `json:"conversionRate"`  // 转化率 %
	ValidOrders     int64            `json:"validOrders"`
	InvalidOrders   int64            `json:"invalidOrders"`
	CouponOrders    int64            `json:"couponOrders"`
	Funnel          []FunnelStep     `json:"funnel"` // 漏斗
	CancelReasons   []ReasonCount    `json:"cancelReasons"` // 取消原因分布
}

// FunnelStep 漏斗步骤（工单06：访问→服务选择→下单→支付→完成）
type FunnelStep struct {
	Step  string `json:"step"`
	Count int64  `json:"count"`
}

// ReasonCount 原因分布
type ReasonCount struct {
	Reason string `json:"reason"`
	Count  int64  `json:"count"`
}

func AnalyzeConversion(start, end time.Time) (*ConversionAnalysis, error) {
	if start.IsZero() || end.IsZero() {
		end = time.Now()
		start = end.AddDate(0, -1, 0)
	}
	r := &ConversionAnalysis{}

	model.DB.Model(&model.User{}).Where("created_at >= ? AND created_at < ?", start, end).Count(&r.RegisteredUsers)
	model.DB.Model(&model.Order{}).Distinct("user_id").
		Where("created_at >= ? AND created_at < ? AND status IN ?", start, end, []string{model.OrderPaid, model.OrderOngoing, model.OrderFinished}).
		Count(&r.BuyUsers)
	if r.RegisteredUsers > 0 {
		r.ConversionRate = round2(float64(r.BuyUsers) / float64(r.RegisteredUsers) * 100)
	}
	model.DB.Model(&model.Order{}).Where("created_at >= ? AND created_at < ? AND status <> ?", start, end, model.OrderCancelled).Count(&r.ValidOrders)
	model.DB.Model(&model.Order{}).Where("created_at >= ? AND created_at < ? AND status = ?", start, end, model.OrderCancelled).Count(&r.InvalidOrders)
	model.DB.Model(&model.Order{}).Where("created_at >= ? AND created_at < ? AND coupon_id > 0", start, end).Count(&r.CouponOrders)

	// 漏斗：基于用户行为记录聚合
	// home(访问) -> service_select(选择服务) -> order(下单) -> pay(支付) -> finish(完成)
	steps := []string{"home", "service_select", "order", "pay", "finish"}
	for i, s := range steps {
		fs := FunnelStep{Step: s}
		if i < 3 { // 前三步基于行为埋点数据
			model.DB.Model(&model.UserVisit{}).Distinct("device_id").
				Where("page = ? AND visited_at >= ? AND visited_at < ?", s, start, end).Count(&fs.Count)
		} else { // 后两步基于订单数据
			status := model.OrderPaid
			if s == "finish" {
				status = model.OrderFinished
			}
			model.DB.Model(&model.Order{}).Distinct("user_id").
				Where("created_at >= ? AND created_at < ? AND status >= ?", start, end, status).Count(&fs.Count)
		}
		r.Funnel = append(r.Funnel, fs)
	}
	model.DB.Model(&model.Order{}).Select("cancel_reason AS reason, COUNT(*) AS count").
		Where("created_at >= ? AND created_at < ? AND status = ? AND cancel_reason <> ''", start, end, model.OrderCancelled).
		Group("cancel_reason").Scan(&r.CancelReasons)
	return r, nil
}

// RepurchaseAnalysis 复购周期分析（工单06：用户忠诚度、复购次数、复购周期）
type RepurchaseAnalysis struct {
	UserCount        int64   `json:"userCount"`        // 有购买行为的用户数
	RepurchaseUsers  int64   `json:"repurchaseUsers"`  // 复购用户数（>1单）
	AvgCycleDays     float64 `json:"avgCycleDays"`     // 平均复购周期（天）
	MaxCycleDays     float64 `json:"maxCycleDays"`
	MinCycleDays     float64 `json:"minCycleDays"`
}

// AnalyzeRepurchase 复购周期分析：
// 首次订单时间、后续订单时间、复购次数（除首次购买外的所有订单数量）。
// 周期在应用层计算，兼容 SQLite / MySQL。
func AnalyzeRepurchase(start, end time.Time) (*RepurchaseAnalysis, error) {
	r := &RepurchaseAnalysis{}
	type row struct {
		UserID    uint      `json:"userId"`
		CreatedAt time.Time `json:"createdAt"`
	}
	var rows []row
	model.DB.Model(&model.Order{}).
		Select("user_id, created_at").
		Where("created_at >= ? AND created_at < ? AND status IN ?", start, end, []string{model.OrderPaid, model.OrderOngoing, model.OrderFinished}).
		Order("user_id, created_at").Scan(&rows)

	userOrders := map[uint][]time.Time{}
	for _, rw := range rows {
		userOrders[rw.UserID] = append(userOrders[rw.UserID], rw.CreatedAt)
	}
	r.UserCount = int64(len(userOrders))

	var totalCycle, cnt float64
	minCycle, maxCycle := -1.0, 0.0
	repurchaseCnt := int64(0)
	for _, ts := range userOrders {
		if len(ts) < 2 {
			continue
		}
		r.RepurchaseUsers++
		repurchaseCnt += int64(len(ts) - 1) // 复购次数
		for i := 1; i < len(ts); i++ {      // 相邻两次下单间隔作为复购周期
			d := ts[i].Sub(ts[i-1]).Hours() / 24
			totalCycle += d
			cnt++
			if minCycle < 0 || d < minCycle {
				minCycle = d
			}
			if d > maxCycle {
				maxCycle = d
			}
		}
	}
	_ = repurchaseCnt
	if cnt > 0 {
		r.AvgCycleDays = round2(totalCycle / cnt)
		r.MaxCycleDays = round2(maxCycle)
		r.MinCycleDays = round2(minCycle)
	}
	return r, nil
}

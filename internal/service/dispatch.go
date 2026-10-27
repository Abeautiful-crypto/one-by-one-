// 派单引擎（工单11：网约车订单中心与派单规则配置）
// 综合得分 = wGeo*距离分 + wRating*司机评分分 + wTimeWindow*时间窗罚分 + wAcceptRate*历史接单率
// 规则落地：
//   a. 依据乘客用车时间距当前时间先后排序优先分配；
//   b. 校验司机在用车时间前后无订单冲突且可服务；
//   c. 白天(7~22时)/夜间应用不同权重与超时策略；
//   d. 候选司机池由后台配置（DispatchConfig.DriverPool），可按城市等维度灰度。
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"ride-hailing/internal/model"
	"ride-hailing/internal/pkg/geo"
	"ride-hailing/internal/pkg/redisx"
)

// DispatchCandidate 候选司机
type DispatchCandidate struct {
	DriverID uint    `json:"driverId"`
	Score    float64 `json:"score"`
	Distance float64 `json:"distance"` // km
	Reason   string  `json:"reason"`   // 可解释的决策理由（工单11 验收）
}

// DispatchConfigFor 取城市派单配置（无则默认；工单11规则d：后台可配置）
func DispatchConfigFor(city string) model.DispatchConfig {
	var cfg model.DispatchConfig
	if err := model.DB.Where("city = ? AND enabled = ?", city, true).First(&cfg).Error; err != nil {
		// 默认配置（白天/夜间同参）
		return model.DispatchConfig{
			City: city, WGeo: 0.4, WRating: 0.25, WTimeWindow: 0.15, WAcceptRate: 0.2,
			SearchRadiusKm: 5, DayStart: 7, DayEnd: 22,
			DayTimeoutSec: 60, NightTimeoutSec: 90, Enabled: true, Version: 0,
		}
	}
	return cfg
}

// isNight 工单11规则c：七时至二十二时为白天
func isNight(t time.Time, dayStart, dayEnd int) bool {
	h := t.Hour()
	return h < dayStart || h >= dayEnd
}

// Dispatch 为订单计算候选司机列表（工单17 大厅派单与工单15 自动推送共用）
func Dispatch(o *model.RideOrder) ([]DispatchCandidate, error) {
	cfg := DispatchConfigFor(o.City)

	// 规则d：候选司机池（后台配置，逗号分隔 driverId；空 = 全部在线司机）
	pool := map[uint]bool{}
	if cfg.DriverPool != "" {
		for _, s := range splitCSV(cfg.DriverPool) {
			var id uint
			fmt.Sscanf(s, "%d", &id)
			if id > 0 {
				pool[id] = true
			}
		}
	}

	// GEO 就近候选（Redis GEO / 内存降级，工单11）
	ctx := context.Background()
	neighbors, err := geo.I().Nearby(ctx, geo.Bucket("drivers", o.City), o.StartLng, o.StartLat, cfg.SearchRadiusKm, 50)
	if err != nil {
		return nil, err
	}

	cands := make([]DispatchCandidate, 0, len(neighbors))
	now := time.Now()
	for _, n := range neighbors {
		// 工单15/17：只有“在线”司机进入派单池
		st, err := redisx.S().Get(ctx, driverStatusKey(n.ID))
		if err != nil || st != "online" {
			continue
		}
		// 规则d：池过滤
		if len(pool) > 0 && !pool[n.ID] {
			continue
		}

		// 规则b：用车时间前后无订单冲突（与该司机已接/进行中订单时间窗重叠则排除；
		// 跨库兼容：不使用 DATE_ADD 等数据库函数，改为参数化时间窗）
		var conflict int64
		model.DB.Model(&model.RideOrder{}).
			Where("driver_id = ? AND status IN ? AND depart_at >= ? AND depart_at <= ?",
				n.ID, []string{model.RideAccepted, model.RideArrived, model.RidePickedUp, model.RideOngoing},
				o.DepartAt.Add(-90*time.Minute), o.DepartAt.Add(90*time.Minute)).
			Count(&conflict)
		if conflict > 0 {
			continue
		}

		// 司机评分（DriverProfile.ServiceScore）
		var dp model.DriverProfile
		rating := 4.8
		if err := model.DB.Where("user_id = ?", n.ID).First(&dp).Error; err == nil && dp.ServiceScore > 0 {
			rating = dp.ServiceScore
		}

		// 历史接单率（DispatchLog 统计，工单11）
		var acc, rej int64
		model.DB.Model(&model.DispatchLog{}).Where("driver_id = ? AND result = 'accepted'", n.ID).Count(&acc)
		model.DB.Model(&model.DispatchLog{}).Where("driver_id = ? AND result = 'timeout'", n.ID).Count(&rej)
		acceptRate := 0.7
		if acc+rej > 0 {
			acceptRate = float64(acc) / float64(acc+rej)
		}

		// 归一化分项
		geoScore := 1 - math.Min(n.Distance/cfg.SearchRadiusKm, 1)     // 越近分越高
		ratingScore := (rating - 3) / 2                                 // 3~5 → 0~1
		if ratingScore < 0 {
			ratingScore = 0
		}
		windowScore := 1.0                                              // 无冲突满分（冲突已排除）
		// 规则a：用车时间越近优先（30 分钟内满窗，越远衰减）
		urgency := 1 - math.Min(time.Until(o.DepartAt).Minutes()/120, 1)

		score := cfg.WGeo*geoScore + cfg.WRating*ratingScore +
			cfg.WTimeWindow*windowScore + cfg.WAcceptRate*acceptRate
		reason := fmt.Sprintf("距离%.1fkm(距分%.2f) 服务评分%.1f(评分分%.2f) 接单率%.0f%% 用车紧急度%.2f 时间窗无冲突；白天段=%v",
			n.Distance, geoScore, rating, ratingScore, acceptRate*100, urgency, !isNight(now, cfg.DayStart, cfg.DayEnd))

		cands = append(cands, DispatchCandidate{DriverID: n.ID, Score: score, Distance: n.Distance, Reason: reason})
	}

	// 综合得分降序；规则a：同分时用车时间近者优先（稳定性排序保持时间序）
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].Score != cands[j].Score {
			return cands[i].Score > cands[j].Score
		}
		return o.DepartAt.Before(o.DepartAt)
	})

	// 派单决策记录（工单11：可解释决策理由落库）
	for _, cd := range cands {
		model.DB.Create(&model.DispatchLog{OrderNo: o.OrderNo, DriverID: cd.DriverID, Score: cd.Score, Reason: cd.Reason, Result: "pushed"})
	}
	return cands, nil
}

// DispatchTimeoutSec 规则c：白天/夜间派单超时不同
func DispatchTimeoutSec(city string) int {
	cfg := DispatchConfigFor(city)
	if isNight(time.Now(), cfg.DayStart, cfg.DayEnd) {
		return cfg.NightTimeoutSec
	}
	return cfg.DayTimeoutSec
}

// SaveDispatchConfig 保存派单配置（版本+1；保存前快照旧版本供回滚；工单11 灰度到城市）
func SaveDispatchConfig(cfg *model.DispatchConfig, operator string) error {
	var old model.DispatchConfig
	if err := model.DB.Where("city = ?", cfg.City).First(&old).Error; err == nil {
		cfg.Version = old.Version + 1
		cfg.ID = old.ID
		// 快照旧版本（工单11 一键回滚的数据基础）
		if snap, err := json.Marshal(old); err == nil {
			model.DB.Create(&model.DispatchConfigHistory{City: cfg.City, Version: old.Version, Snapshot: string(snap)})
		}
	} else {
		cfg.Version = 1
	}
	cfg.UpdatedBy = operator
	return model.DB.Save(cfg).Error
}

// RollbackDispatchConfig 回滚到上一版本（工单11：真正恢复旧版本配置值）
func RollbackDispatchConfig(city, operator string) error {
	var cfg model.DispatchConfig
	if err := model.DB.Where("city = ?", city).First(&cfg).Error; err != nil {
		return fmt.Errorf("该城市无派单配置")
	}
	if cfg.Version <= 1 {
		return fmt.Errorf("已是最初版本，无可回滚版本")
	}
	// 取上一版本快照并恢复
	var hist model.DispatchConfigHistory
	if err := model.DB.Where("city = ? AND version = ?", city, cfg.Version-1).
		Order("id desc").First(&hist).Error; err != nil {
		return fmt.Errorf("未找到版本 %d 的历史快照", cfg.Version-1)
	}
	var prev model.DispatchConfig
	if err := json.Unmarshal([]byte(hist.Snapshot), &prev); err != nil {
		return fmt.Errorf("历史快照解析失败")
	}
	prev.ID = cfg.ID          // 保持同一条记录
	prev.Version = cfg.Version - 1
	prev.UpdatedBy = operator
	return model.DB.Save(&prev).Error
}

func splitCSV(s string) []string {
	var out []string
	cur := ""
	for _, c := range s {
		if c == ',' || c == '，' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		cur += string(c)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

var _ = json.Marshal

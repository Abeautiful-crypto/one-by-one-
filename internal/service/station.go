// 工单：全栈开发-网约车-02-班车管理
// 站点业务规则：
//   1. 上下车站点排重：同一个站点不能同时出现在上车站点或者下车站点的位置；
//   2. 站点排序：按填写的时间（距起点分钟数）自动升序排序，上车站点第一个站点时间为 0。
//      举例：A→B 20min、A→C 40min、A→D 30min => 排序为 ABDC。
package service

import (
	"errors"
	"sort"

	"ride-hailing/internal/model"

	"gorm.io/gorm"
)

// StationInput 站点录入项
type StationInput struct {
	StationName string `json:"stationName" binding:"required"` // 站点名称
	StopType    string `json:"stopType" binding:"required,oneof=up down"` // up/down
	Minutes     int    `json:"minutes" binding:"gte=0"`        // 距起点分钟数
	ArriveTime  string `json:"arriveTime"`                     // 预计到达时间 HH:MM
}

// SortStations 校验排重 + 按时间升序排序（工单02 核心验收逻辑）
func SortStations(inputs []StationInput) ([]StationInput, error) {
	seen := map[string]bool{}
	for _, in := range inputs {
		key := in.StationName + ":" + in.StopType
		if seen[key] {
			return nil, errors.New("站点重复: " + in.StationName)
		}
		seen[key] = true
	}
	// 上车站点第一个站点时间为 0
	for i := range inputs {
		if inputs[i].StopType == "up" {
			inputs[i].Minutes = 0
			break
		}
	}
	sort.SliceStable(inputs, func(i, j int) bool {
		return inputs[i].Minutes < inputs[j].Minutes
	})
	return inputs, nil
}

// ensureStations 确保站点存在（不存在则创建），返回名称->ID 映射
func ensureStations(inputs []StationInput) (map[string]uint, error) {
	m := map[string]uint{}
	for _, in := range inputs {
		if _, ok := m[in.StationName]; ok {
			continue
		}
		var st model.Station
		if err := model.DB.Where("name = ?", in.StationName).First(&st).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				st = model.Station{Name: in.StationName}
				if err := model.DB.Create(&st).Error; err != nil {
					return nil, err
				}
			} else {
				return nil, err
			}
		}
		m[in.StationName] = st.ID
	}
	return m, nil
}

// SaveRouteStations 保存线路站点（先清空后写入，写入排序结果）
func SaveRouteStations(routeID uint, inputs []StationInput) error {
	sorted, err := SortStations(inputs)
	if err != nil {
		return err
	}
	nameToID, err := ensureStations(sorted)
	if err != nil {
		return err
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("route_id = ?", routeID).Delete(&model.RouteStation{}).Error; err != nil {
			return err
		}
		for i, in := range sorted {
			rs := model.RouteStation{
				RouteID:    routeID,
				StationID:  nameToID[in.StationName],
				StopType:   in.StopType,
				Minutes:    in.Minutes,
				ArriveTime: in.ArriveTime,
				Seq:        i + 1,
			}
			if err := tx.Create(&rs).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

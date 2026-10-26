// 工单：全栈开发-网约车-02-班车管理
// 线路、站点、线路-站点关联（上下车站点排重、按时间自动排序）、班次排班。
package model

import "time"

// Route 线路（通勤班车、机场专线等固定线路服务）
type Route struct {
	BaseModel
	Name       string `gorm:"size:128;not null" json:"name"` // 线路名称
	StartName  string `gorm:"size:64" json:"startName"`      // 起点
	EndName    string `gorm:"size:64" json:"endName"`        // 终点
	Desc       string `gorm:"size:255" json:"desc"`
	Status     string `gorm:"size:20;default:enabled" json:"status"`
}

// Station 站点
type Station struct {
	BaseModel
	Name      string  `gorm:"size:64;uniqueIndex;not null" json:"name"` // 站点名称（排重依据）
	Location  string  `gorm:"size:255" json:"location"`                 // 具体位置
	Lng       float64 `json:"lng"`                                      // 经度（地图展示）
	Lat       float64 `json:"lat"`                                      // 纬度
}

// RouteStation 线路站点关联
// 工单02验收标准：
//  1. 上下车站点排重：同一线路同一站点不能同时出现在上车站点或下车站点位置；
//  2. 站点排序：根据选择站点时填写的时间（距起点分钟数）自动升序排序，
//     上车站点第一个站点时间为 0。例：A→B 20min、A→C 40min、A→D 30min => 排序 ABDC。
type RouteStation struct {
	BaseModel
	RouteID    uint    `gorm:"index;not null" json:"routeId"`
	StationID  uint    `gorm:"index;not null" json:"stationId"`
	StopType   string  `gorm:"size:10;not null" json:"stopType"` // up=上车站点 / down=下车站点
	Minutes    int     `json:"minutes"`                          // 距起点行驶分钟数（排序依据）
	ArriveTime string  `gorm:"size:8" json:"arriveTime"`         // 预计到达时间 HH:MM（展示用）
	Seq        int     `json:"seq"`                              // 计算后的顺序号
}

// Shift 班次排班（班车调度）
type Shift struct {
	BaseModel
	RouteID    uint       `gorm:"index;not null" json:"routeId"`
	VehicleID  uint       `gorm:"index" json:"vehicleId"`
	DriverID   uint       `gorm:"index" json:"driverId"`
	DepartTime time.Time  `gorm:"index" json:"departTime"` // 发车时间（前端以日历形式展示，工单04）
	Price      float64    `json:"price"`                   // 票价
	SeatCount  int        `json:"seatCount"`               // 座位数
	SoldCount  int        `json:"soldCount"`               // 已售
	Status     string     `gorm:"size:20;default:scheduled" json:"status"` // scheduled/running/finished/cancelled
}

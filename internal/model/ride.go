// 网约车订单域模型（工单11/15/16/17）
package model

import "time"

// 网约车订单状态机（工单11 状态机 + 工单15/16/17 流转，代码注释标明工单编号）
// pending(待接单) → accepted(已接单) → arrived(到达上车点) → picked_up(接到乘客)
//   → ongoing(行程开始,工单16启动计费) → arrived_dest(到达目的地) → billed(账单确认) → completed(结算完成)
// 任意进行中状态可被取消 → cancelled（工单16 乘客取消含补偿）
const (
	RidePending     = "pending"      // 待接单（工单15：推送时 DB 状态）
	RideAccepted    = "accepted"     // 已接单（去接乘客）
	RideArrived     = "arrived"      // 到达上车点
	RidePickedUp    = "picked_up"    // 接到乘客
	RideOngoing     = "ongoing"      // 行程中（计费中）
	RideArrivedDest = "arrived_dest" // 到达目的地（停止计费）
	RideBilled      = "billed"       // 账单已确认（工单16）
	RideCompleted   = "completed"    // 结算完成
	RideCancelled   = "cancelled"    // 已取消
)

// RideOrder 网约车订单（区别于班线 Order，独立订单域）
type RideOrder struct {
	BaseModel
	OrderNo       string     `gorm:"size:32;uniqueIndex" json:"orderNo"`
	PassengerID   uint       `gorm:"index" json:"passengerId"`
	PassengerName string     `gorm:"size:50" json:"passengerName"`
	PassengerPhone string    `gorm:"size:20" json:"passengerPhone"`
	DriverID      uint       `gorm:"index" json:"driverId"` // 0=未接单（工单15：并发接单仅绑定一个司机ID）
	DriverName    string     `gorm:"size:50" json:"driverName"`
	City          string     `gorm:"size:20;index" json:"city"`
	Status        string     `gorm:"size:20;index:idx_ride_query,priority:2" json:"status"`
	Source        string     `gorm:"size:20" json:"source"` // app/ai_assistant（工单10）
	// 起终点（工单15：订单详情展示起点、终点、预估费用、乘客评分）
	StartName string    `gorm:"size:100" json:"startName"`
	StartLng  float64   `json:"startLng"`
	StartLat  float64   `json:"startLat"`
	DestName  string    `gorm:"size:100" json:"destName"`
	DestLng   float64   `json:"destLng"`
	DestLat   float64   `json:"destLat"`
	DepartAt  time.Time `gorm:"index:idx_ride_query,priority:3" json:"departAt"` // 用车时间（工单11规则a：按用车时间排序）
	DistanceKm float64  `json:"distanceKm"`
	EstimateFare float64 `json:"estimateFare"` // 预估费用（推送含预估金额，工单15/17）
	FinalFare    float64 `json:"finalFare"`
	PassengerScore float64 `json:"passengerScore"` // 乘客评分（工单15/17 列表展示与筛选）
	// 行程节点（工单16：记录开始/结束时间与地点）
	StartAt   *time.Time `json:"startAt"`   // 行程开始时间
	EndAt     *time.Time `json:"endAt"`     // 行程结束时间
	StartAddr string     `gorm:"size:200" json:"startAddr"` // 行程开始地点
	EndAddr   string     `gorm:"size:200" json:"endAddr"`
	MileageKm float64    `json:"mileageKm"` // 实际行驶里程
	// 取消（工单16：原因枚举/补偿/操作方）
	CancelReason string     `gorm:"size:200" json:"cancelReason"`
	CancelBy     string     `gorm:"size:20" json:"cancelBy"` // passenger/driver/system
	CancelAt     *time.Time `json:"cancelAt"`
	Compensation float64    `json:"compensation"` // 司机空驶补偿（工单16）
	VehicleType  string     `gorm:"size:20" json:"vehicleType"` // economy/comfort（工单10 舒适型推荐）
	VehiclePlate string     `gorm:"size:20;index:idx_ride_query,priority:4" json:"vehiclePlate"` // 车牌号（工单11 联合索引维度）
	BillStatus   string     `gorm:"size:20" json:"billStatus"` // none/waiting/confirmed/disputed（工单16）
	Remark       string     `gorm:"size:500" json:"remark"`
}

func (RideOrder) TableName() string { return "ride_orders" }

// TripBill 行程账单（工单16：起步价、里程费、时长费、等待费、高速费、夜间加价、空驶补偿）
type TripBill struct {
	BaseModel
	OrderID     uint    `gorm:"index;not null" json:"orderId"`
	OrderNo     string  `gorm:"size:32;index" json:"orderNo"`
	BaseFare    float64 `json:"baseFare"`    // 起步价
	MileageFare float64 `json:"mileageFare"` // 里程费
	DurationFare float64 `json:"durationFare"` // 时长费
	WaitFare    float64 `json:"waitFare"`    // 等待费
	TollFare    float64 `json:"tollFare"`    // 高速费（司机填报）
	NightExtra  float64 `json:"nightExtra"`  // 夜间加价
	Compensation float64 `json:"compensation"` // 取消空驶补偿（若有）
	Total       float64 `json:"total"`
	IsNight     bool    `json:"isNight"` // 是否夜间计价
	ConfirmedAt *time.Time `json:"confirmedAt"` // 司机确认时间（工单16 确认账单）
	Disputed    bool    `json:"disputed"`
	DisputeReason string `gorm:"size:300" json:"disputeReason"` // 司机异议上报
}

func (TripBill) TableName() string { return "trip_bills" }

// OrderAudit 订单审计（工单11：所有修改操作引入幂等键与操作审计）
type OrderAudit struct {
	BaseModel
	OrderNo  string `gorm:"size:32;index" json:"orderNo"`
	Action   string `gorm:"size:40" json:"action"` // create/accept/reject/cancel/reassign/status/bill...
	Operator string `gorm:"size:50" json:"operator"`
	Detail   string `gorm:"size:500" json:"detail"`
}

func (OrderAudit) TableName() string { return "order_audits" }

// DispatchConfig 派单配置（工单11 派单设置：可配置、版本化、灰度、可回滚）
type DispatchConfig struct {
	BaseModel
	City        string  `gorm:"size:20;uniqueIndex" json:"city"` // 灰度维度：城市
	WGeo        float64 `json:"wGeo"`        // GEO 距离权重
	WRating     float64 `json:"wRating"`     // 司机评分权重
	WTimeWindow float64 `json:"wTimeWindow"` // 时间窗罚分权重
	WAcceptRate float64 `json:"wAcceptRate"` // 历史接单率权重
	SearchRadiusKm float64 `json:"searchRadiusKm"`
	DayStart    int     `json:"dayStart"` // 白天起始（工单11规则c：7时）
	DayEnd      int     `json:"dayEnd"`   // 白天结束（22时）
	DayTimeoutSec  int  `json:"dayTimeoutSec"`  // 白天派单超时
	NightTimeoutSec int `json:"nightTimeoutSec"` // 夜间派单超时
	DriverPool  string  `gorm:"size:500" json:"driverPool"` // 候选司机池（工单11规则d：逗号分隔司机ID，空=全部）
	Version     int     `json:"version"`
	Enabled     bool    `json:"enabled"`
	UpdatedBy   string  `gorm:"size:50" json:"updatedBy"`
}

func (DispatchConfig) TableName() string { return "dispatch_configs" }

// DispatchConfigHistory 派单配置历史快照（工单11：一键回滚到上一版本）
type DispatchConfigHistory struct {
	BaseModel
	City     string `gorm:"size:20;index" json:"city"`
	Version  int    `json:"version"`
	Snapshot string `gorm:"type:text" json:"-"` // 上一版本完整配置 JSON
}

func (DispatchConfigHistory) TableName() string { return "dispatch_config_histories" }

// DispatchLog 派单决策记录（工单11：输出可解释的决策理由）
type DispatchLog struct {
	BaseModel
	OrderNo  string  `gorm:"size:32;index" json:"orderNo"`
	DriverID uint    `json:"driverId"`
	Score    float64 `json:"score"`
	Reason   string  `gorm:"size:500" json:"reason"`
	Result   string  `gorm:"size:20" json:"result"` // pushed/accepted/timeout
}

func (DispatchLog) TableName() string { return "dispatch_logs" }

// FloodReport 道路积水上报（工单10：拍照识别→更新途经订单→补偿券）
type FloodReport struct {
	BaseModel
	ReporterID uint    `json:"reporterId"`
	RoadName   string  `gorm:"size:100" json:"roadName"`
	DepthCm    float64 `json:"depthCm"` // 积水深度（示例约15cm）
	Lng        float64 `json:"lng"`
	Lat        float64 `json:"lat"`
	PhotoURL   string  `gorm:"size:300" json:"photoUrl"`
	Status     string  `gorm:"size:20" json:"status"` // reported/confirmed/cleared
}

func (FloodReport) TableName() string { return "flood_reports" }

// ShelterPoint 避雨上车点（工单10：200米内带顶棚位置推荐）
type ShelterPoint struct {
	BaseModel
	Name    string  `gorm:"size:100" json:"name"`
	Lng     float64 `json:"lng"`
	Lat     float64 `json:"lat"`
	HasRoof bool    `json:"hasRoof"`
	CouponValue float64 `json:"couponValue"` // 专属优惠券（¥5）
	FreeWaitMin int     `json:"freeWaitMin"` // 免费等待分钟（15）
}

func (ShelterPoint) TableName() string { return "shelter_points" }

// WeatherAlert 天气预警（工单10：预警触发推送）
type WeatherAlert struct {
	BaseModel
	City       string `gorm:"size:20;index" json:"city"`
	Level      string `gorm:"size:20" json:"level"` // yellow/orange/red（黄/橙/红色预警）
	Type       string `gorm:"size:20" json:"type"`  // rainstorm/...
	Title      string `gorm:"size:200" json:"title"`
	Impact     string `gorm:"size:500" json:"impact"` // 影响：等待+8min、费用+15%等
	ActiveUntil *time.Time `json:"activeUntil"`
}

func (WeatherAlert) TableName() string { return "weather_alerts" }

// IdemKey 幂等键表（工单11：取消/改派/下单幂等）
type IdemKey struct {
	BaseModel
	Key    string `gorm:"size:64;uniqueIndex" json:"key"`
	Result string `gorm:"size:1000" json:"result"` // 首次执行结果快照
}

func (IdemKey) TableName() string { return "idem_keys" }

// TripTrace 行程轨迹点（工单11：热冷分层——热数据 Redis List，冷数据落本表）
type TripTrace struct {
	BaseModel
	OrderNo string    `gorm:"size:32;index" json:"orderNo"`
	Lng     float64   `json:"lng"`
	Lat     float64   `json:"lat"`
	Speed   float64   `json:"speed"`
	At      time.Time `json:"at"`
}

func (TripTrace) TableName() string { return "trip_traces" }

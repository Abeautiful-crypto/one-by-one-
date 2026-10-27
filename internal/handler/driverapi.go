// 司机端接单 / 订单中心 / 行程账单接口（工单11/15/16/17）
package handler

import (
	"strconv"
	"time"

	"ride-hailing/internal/middleware"
	"ride-hailing/internal/model"
	"ride-hailing/internal/pkg/resp"
	"ride-hailing/internal/pkg/ws"
	"ride-hailing/internal/service"

	"github.com/gin-gonic/gin"
)

// DriverAPIHandler 司机端与网约车订单接口
type DriverAPIHandler struct{}

// ---------------- 司机状态（工单15/17：上线/下线，Redis 存储） ----------------

type onlineReq struct {
	City string  `json:"city"`
	Lng  float64 `json:"lng"`
	Lat  float64 `json:"lat"`
}

// GoOnline 司机上线（只有在线才能接收新订单推送）
func (h *DriverAPIHandler) GoOnline(c *gin.Context) {
	uid := middleware.GetUserID(c)
	var req onlineReq
	_ = c.ShouldBindJSON(&req)
	if req.City == "" {
		req.City = "上海"
	}
	if err := service.DriverGoOnline(uid, req.City, req.Lng, req.Lat); err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"driverId": uid, "status": "online"})
}

// GoOffline 司机下线（下线后不接收任何推送，移出派单池）
func (h *DriverAPIHandler) GoOffline(c *gin.Context) {
	uid := middleware.GetUserID(c)
	var req onlineReq
	_ = c.ShouldBindJSON(&req)
	if req.City == "" {
		req.City = "上海"
	}
	if err := service.DriverGoOffline(uid, req.City); err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"driverId": uid, "status": "offline"})
}

// MyStatus 查询自身状态
func (h *DriverAPIHandler) MyStatus(c *gin.Context) {
	uid := middleware.GetUserID(c)
	resp.OK(c, gin.H{"driverId": uid, "status": service.DriverStatus(uid)})
}

// LocationReport 司机位置上报（工单11 订单跟踪）
func (h *DriverAPIHandler) LocationReport(c *gin.Context) {
	uid := middleware.GetUserID(c)
	type locReq struct {
		City  string  `json:"city"`
		Lng   float64 `json:"lng"`
		Lat   float64 `json:"lat"`
		Speed float64 `json:"speed"`
	}
	var lr locReq
	if err := c.ShouldBindJSON(&lr); err != nil {
		resp.BadRequest(c, "参数错误")
		return
	}
	if lr.City == "" {
		lr.City = "上海"
	}
	if err := service.DriverLocationReport(uid, lr.City, lr.Lng, lr.Lat, lr.Speed); err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"ok": true})
}

// ---------------- 接单大厅（工单17：GET /orders/list） ----------------

// HallOrders 附近可接订单列表（含经纬度地图标记、筛选排序分页）
func (h *DriverAPIHandler) HallOrders(c *gin.Context) {
	uid := middleware.GetUserID(c)
	city := c.DefaultQuery("city", "上海")
	lng, _ := strconv.ParseFloat(c.DefaultQuery("lng", "121.4737"), 64)
	lat, _ := strconv.ParseFloat(c.DefaultQuery("lat", "31.2304"), 64)
	minFare, _ := strconv.ParseFloat(c.DefaultQuery("minFare", "0"), 64)
	minScore, _ := strconv.ParseFloat(c.DefaultQuery("minScore", "0"), 64)
	maxDist, _ := strconv.ParseFloat(c.DefaultQuery("maxDistanceKm", "5"), 64)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "10"))
	list, total, err := service.ListHallOrders(uid, city, lng, lat, minFare, maxDist, minScore, c.DefaultQuery("sortBy", "distance"), page, pageSize)
	if err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"list": list, "total": total, "page": page, "pageSize": pageSize})
}

// ---------------- 抢单/拒单（工单15/17） ----------------

type acceptReq struct {
	OrderNo string `json:"orderNo" binding:"required"`
}

// AcceptOrder 抢单（并发安全：失败返回“订单已被他人接走”）
func (h *DriverAPIHandler) AcceptOrder(c *gin.Context) {
	uid := middleware.GetUserID(c)
	var req acceptReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	driverName := middleware.GetUsername(c)
	plate := ""
	var v model.Vehicle
	if err := model.DB.Where("driver_id = ?", uid).First(&v).Error; err == nil {
		plate = v.PlateNo
	}
	o, err := service.AcceptRideOrder(req.OrderNo, uid, driverName, plate)
	if err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	resp.OK(c, o)
}

type rejectReq struct {
	OrderNo string `json:"orderNo" binding:"required"`
	Reason  string `json:"reason"`
}

// RejectOrder 拒单（记录原因与时间，重新派发）
func (h *DriverAPIHandler) RejectOrder(c *gin.Context) {
	uid := middleware.GetUserID(c)
	var req rejectReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	if err := service.RejectRideOrder(req.OrderNo, uid, req.Reason); err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"message": "已拒单，订单重新派发中"})
}

// ---------------- 行程流程（工单15 接乘客 + 工单16） ----------------

// ArriveAtPickup 到达上车点
func (h *DriverAPIHandler) ArriveAtPickup(c *gin.Context) {
	o, err := service.ArriveAtPickup(c.Param("orderNo"))
	if err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	resp.OK(c, o)
}

// PickupPassenger 接到乘客
func (h *DriverAPIHandler) PickupPassenger(c *gin.Context) {
	o, err := service.PickupPassenger(c.Param("orderNo"))
	if err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	resp.OK(c, o)
}

type startTripReq struct {
	StartAddr string `json:"startAddr"`
}

// StartTrip 行程开始（工单16：记录时间地点、启动计费、司机状态→行程中）
func (h *DriverAPIHandler) StartTrip(c *gin.Context) {
	var req startTripReq
	_ = c.ShouldBindJSON(&req)
	o, _, err := service.StartTrip(c.Param("orderNo"), req.StartAddr)
	if err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	resp.OK(c, o)
}

type finishTripReq struct {
	EndAddr  string  `json:"endAddr"`
	TollFare float64 `json:"tollFare"` // 高速费（司机填报）
}

// FinishTrip 到达目的地（工单16：停止计费、生成账单、通知乘客支付）
func (h *DriverAPIHandler) FinishTrip(c *gin.Context) {
	var req finishTripReq
	_ = c.ShouldBindJSON(&req)
	o, bill, err := service.FinishTrip(c.Param("orderNo"), req.EndAddr, req.TollFare)
	if err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"order": o, "bill": bill})
}

// ConfirmBill 确认账单（工单16：提交平台结算）
func (h *DriverAPIHandler) ConfirmBill(c *gin.Context) {
	bill, err := service.ConfirmBill(c.Param("orderNo"))
	if err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	resp.OK(c, bill)
}

type billDisputeReq struct {
	Reason string `json:"reason" binding:"required"`
}

// DisputeBill 账单异议上报（工单16）
func (h *DriverAPIHandler) DisputeBill(c *gin.Context) {
	var req billDisputeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	if err := service.DisputeBill(c.Param("orderNo"), req.Reason); err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"message": "异议已提交，平台将审核处理"})
}

// ---------------- 订单中心（工单11） ----------------

// CreateRideOrder 乘客下单（幂等键：请求头 X-Idempotency-Key 或 body）
type createRideReq struct {
	PassengerName  string    `json:"passengerName" binding:"required"`
	PassengerPhone string    `json:"passengerPhone" binding:"required"`
	City           string    `json:"city"`
	Source         string    `json:"source"`
	StartName      string    `json:"startName" binding:"required"`
	StartLng       float64   `json:"startLng" binding:"required"`
	StartLat       float64   `json:"startLat" binding:"required"`
	DestName       string    `json:"destName" binding:"required"`
	DestLng        float64   `json:"destLng" binding:"required"`
	DestLat        float64   `json:"destLat" binding:"required"`
	DepartAt       time.Time `json:"departAt"`
	VehicleType    string    `json:"vehicleType"`
	IdempotencyKey string    `json:"idempotencyKey"`
}

func (h *DriverAPIHandler) CreateRideOrder(c *gin.Context) {
	uid := middleware.GetUserID(c)
	var req createRideReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	if req.DepartAt.IsZero() {
		req.DepartAt = time.Now() // 即时单
	}
	if req.City == "" {
		req.City = "上海"
	}
	o := &model.RideOrder{
		PassengerID: uid, PassengerName: req.PassengerName, PassengerPhone: req.PassengerPhone,
		City: req.City, Source: req.Source,
		StartName: req.StartName, StartLng: req.StartLng, StartLat: req.StartLat,
		DestName: req.DestName, DestLng: req.DestLng, DestLat: req.DestLat,
		DepartAt: req.DepartAt, VehicleType: req.VehicleType,
	}
	idem := c.GetHeader("X-Idempotency-Key")
	if idem == "" {
		idem = req.IdempotencyKey
	}
	created, err := service.CreateRideOrder(o, idem)
	if err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, created)
}

// CancelRideOrder 取消订单（乘客/司机；幂等键；补偿计算）
type cancelRideReq struct {
	Reason         string `json:"reason" binding:"required"`
	IdempotencyKey string `json:"idempotencyKey"`
}

func (h *DriverAPIHandler) CancelRideOrder(c *gin.Context) {
	var req cancelRideReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	by := c.DefaultQuery("by", "passenger")
	o, comp, err := service.CancelRideOrder(c.Param("orderNo"), by, req.Reason, req.IdempotencyKey)
	if err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"order": o, "compensation": comp})
}

// UpdateRideDestination 修改目的地（工单16）
type updateDestReq struct {
	DestName string  `json:"destName" binding:"required"`
	Lng      float64 `json:"lng" binding:"required"`
	Lat      float64 `json:"lat" binding:"required"`
}

func (h *DriverAPIHandler) UpdateRideDestination(c *gin.Context) {
	var req updateDestReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	o, err := service.UpdateRideDestination(c.Param("orderNo"), req.DestName, req.Lng, req.Lat)
	if err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	resp.OK(c, o)
}

// UpdateRidePickup 修改乘车点（工单16）
func (h *DriverAPIHandler) UpdateRidePickup(c *gin.Context) {
	var req updateDestReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	o, err := service.UpdateRidePickup(c.Param("orderNo"), req.DestName, req.Lng, req.Lat)
	if err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	resp.OK(c, o)
}

// ListRideOrders 订单列表（工单11：来源+状态+时间+车牌组合筛选、排序、分页+聚合统计）
func (h *DriverAPIHandler) ListRideOrders(c *gin.Context) {
	q := service.RideOrderQuery{
		Status: c.Query("status"), Source: c.Query("source"), Plate: c.Query("plate"),
		SortBy: c.DefaultQuery("sortBy", "created_at"), Order: c.DefaultQuery("order", "desc"),
	}
	if v := c.Query("begin"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			q.Begin = &t
		}
	}
	if v := c.Query("end"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			q.End = &t
		}
	}
	q.Page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	q.PageSize, _ = strconv.Atoi(c.DefaultQuery("pageSize", "10"))
	list, total, stats, err := service.ListRideOrders(q)
	if err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"list": list, "total": total, "stats": stats})
}

// GetRideOrderDetail 订单详情（工单11：基础信息+费用明细+状态变更记录+缓存）
func (h *DriverAPIHandler) GetRideOrderDetail(c *gin.Context) {
	d, err := service.GetRideOrderDetail(c.Param("orderNo"))
	if err != nil {
		resp.NotFound(c, err.Error())
		return
	}
	resp.OK(c, d)
}

// ReassignOrder 订单改派（工单11：触发派单引擎重算并记录链路）
func (h *DriverAPIHandler) ReassignOrder(c *gin.Context) {
	o, err := service.ReassignOrder(c.Param("orderNo"), middleware.GetUsername(c))
	if err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	resp.OK(c, o)
}

// TraceReport 轨迹上报（工单11）
type traceReq struct {
	Lng   float64 `json:"lng"`
	Lat   float64 `json:"lat"`
	Speed float64 `json:"speed"`
}

func (h *DriverAPIHandler) TraceReport(c *gin.Context) {
	var req traceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数错误")
		return
	}
	if err := service.TraceReport(c.Param("orderNo"), req.Lng, req.Lat, req.Speed); err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"ok": true})
}

// TraceReplay 轨迹回放（工单11：时间范围+稀释轨迹点+关键事件）
func (h *DriverAPIHandler) TraceReplay(c *gin.Context) {
	stride, _ := strconv.Atoi(c.DefaultQuery("stride", "1"))
	var begin, end *time.Time
	if v := c.Query("begin"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			begin = &t
		}
	}
	if v := c.Query("end"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			end = &t
		}
	}
	points, events, err := service.TraceReplay(c.Param("orderNo"), begin, end, stride)
	if err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"points": points, "events": events})
}

// ---------------- 派单设置（工单11：后台配置/灰度/回滚） ----------------

// GetDispatchConfig 查询城市派单配置
func (h *DriverAPIHandler) GetDispatchConfig(c *gin.Context) {
	var cfg model.DispatchConfig
	if err := model.DB.Where("city = ?", c.Param("city")).First(&cfg).Error; err != nil {
		resp.OK(c, service.DispatchConfigFor(c.Param("city")))
		return
	}
	resp.OK(c, cfg)
}

type dispatchCfgReq struct {
	WGeo, WRating, WTimeWindow, WAcceptRate float64
	SearchRadiusKm  float64
	DayStart, DayEnd, DayTimeoutSec, NightTimeoutSec int
	DriverPool      string
}

// SaveDispatchConfig 保存派单配置（版本化+审计）
func (h *DriverAPIHandler) SaveDispatchConfig(c *gin.Context) {
	var req dispatchCfgReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	cfg := model.DispatchConfig{
		City: c.Param("city"), WGeo: req.WGeo, WRating: req.WRating,
		WTimeWindow: req.WTimeWindow, WAcceptRate: req.WAcceptRate,
		SearchRadiusKm: req.SearchRadiusKm, DayStart: req.DayStart, DayEnd: req.DayEnd,
		DayTimeoutSec: req.DayTimeoutSec, NightTimeoutSec: req.NightTimeoutSec,
		DriverPool: req.DriverPool, Enabled: true,
	}
	if err := service.SaveDispatchConfig(&cfg, middleware.GetUsername(c)); err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, cfg)
}

// RollbackDispatchConfig 一键回滚上一版本（工单11）
func (h *DriverAPIHandler) RollbackDispatchConfig(c *gin.Context) {
	if err := service.RollbackDispatchConfig(c.Param("city"), middleware.GetUsername(c)); err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	var cfg model.DispatchConfig
	model.DB.Where("city = ?", c.Param("city")).First(&cfg)
	resp.OK(c, cfg)
}

// WsStats WebSocket 在线连接数（工单11 监控）
func (h *DriverAPIHandler) WsStats(c *gin.Context) {
	resp.OK(c, gin.H{"online": ws.Global().OnlineCount()})
}

// 网约车订单核心服务（工单11 订单中心 / 工单15 接单接乘客 / 工单16 行程账单取消 / 工单17 接单大厅）
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"

	"ride-hailing/internal/model"
	"ride-hailing/internal/pkg/bus"
	"ride-hailing/internal/pkg/geo"
	"ride-hailing/internal/pkg/redisx"
	"ride-hailing/internal/pkg/ws"
)

// ---------------- Redis Key 设计（工单11/15/17） ----------------
// driver:status:{id}        司机在线状态 online/offline/busy/ongoing（工单17 命名）
// ride:geo:drivers:{city}   司机实时位置 GEO
// ride:geo:orders:{city}    待接单订单 GEO（工单17 地图标记数据源）
// trace:hot:{orderNo}       轨迹热数据 List（工单11 冷热分层）
// cache:order:{orderNo}     订单详情缓存（工单11 二级缓存，变更精准失效）

func driverStatusKey(id uint) string { return "driver:status:" + strconv.Itoa(int(id)) }
func orderGeoBucket(city string) string { return geo.Bucket("orders", city) }
func driverGeoBucket(city string) string { return geo.Bucket("drivers", city) }
func traceHotKey(orderNo string) string { return "trace:hot:" + orderNo }
func orderCacheKey(orderNo string) string { return "cache:order:" + orderNo }

// ErrOrderTaken 并发抢单冲突（工单15/17：提示“订单已被他人接走”）
var ErrOrderTaken = errors.New("订单已被他人接走")

// ---------------- 司机状态（工单15/17：Redis 存储在线状态） ----------------

// DriverGoOnline 司机上线：Redis 写状态 + GEO 登记位置 + 广播（工单15：只有在线才接收新订单）
func DriverGoOnline(driverID uint, city string, lng, lat float64) error {
	ctx := context.Background()
	if err := redisx.S().Set(ctx, driverStatusKey(driverID), "online", 0); err != nil {
		return err
	}
	if err := geo.I().Add(ctx, driverGeoBucket(city), geo.Point{DriverID: driverID, Lng: lng, Lat: lat}); err != nil {
		return err
	}
	model.DB.Model(&model.DriverProfile{}).Where("user_id = ?", driverID).Update("online_status", "online")
	publishJSON(bus.EvDriverStatus, map[string]interface{}{"driverId": driverID, "status": "online", "city": city})
	return nil
}

// DriverGoOffline 司机下线：移除派单池（工单15：不接收任何推送）
func DriverGoOffline(driverID uint, city string) error {
	ctx := context.Background()
	if err := redisx.S().Set(ctx, driverStatusKey(driverID), "offline", 0); err != nil {
		return err
	}
	_ = geo.I().Remove(ctx, driverGeoBucket(city), driverID)
	model.DB.Model(&model.DriverProfile{}).Where("user_id = ?", driverID).Update("online_status", "offline")
	publishJSON(bus.EvDriverStatus, map[string]interface{}{"driverId": driverID, "status": "offline"})
	return nil
}

// DriverStatus 查询司机状态
func DriverStatus(driverID uint) string {
	v, err := redisx.S().Get(context.Background(), driverStatusKey(driverID))
	if err != nil {
		return "offline"
	}
	return v
}

// DriverLocationReport 司机位置上报（工单11 订单跟踪：GEO 更新 + 增量广播 + 节流）
func DriverLocationReport(driverID uint, city string, lng, lat, speed float64) error {
	ctx := context.Background()
	if err := geo.I().Add(ctx, driverGeoBucket(city), geo.Point{DriverID: driverID, Lng: lng, Lat: lat}); err != nil {
		return err
	}
	publishJSON(bus.EvDriverLocation, map[string]interface{}{
		"driverId": driverID, "lng": lng, "lat": lat, "speed": speed, "ts": time.Now().Unix(),
	})
	return nil
}

// ---------------- 订单创建与派单推送（工单11/15/17） ----------------

// CreateRideOrder 创建订单：幂等键 → 估价 → 派单计算 → 入 GEO 待接单池 → 广播推送
func CreateRideOrder(o *model.RideOrder, idemKey string) (*model.RideOrder, error) {
	// 工单11：幂等键——重复提交返回首次结果
	if idemKey != "" {
		var k model.IdemKey
		if err := model.DB.Where("key = ?", idemKey).First(&k).Error; err == nil {
			var prev model.RideOrder
			if json.Unmarshal([]byte(k.Result), &prev) == nil && prev.ID > 0 {
				return &prev, nil
			}
		}
	}

	if o.OrderNo == "" {
		o.OrderNo = "R" + time.Now().Format("20060102150405") + strconv.Itoa(int(o.ID)) + rand4()
	}
	o.Status = model.RidePending
	if o.City == "" {
		o.City = "上海"
	}
	if o.DistanceKm == 0 {
		o.DistanceKm = geo.HaversineKm(o.StartLat, o.StartLng, o.DestLat, o.DestLng)
	}
	// 预估费用 = 计费规则按预估里程（工单15/17：推送含预估金额）
	est := CalcBill(&model.RideOrder{City: o.City, MileageKm: o.DistanceKm}, o.DistanceKm*2, 0, 0)
	o.EstimateFare = est.Total

	if err := model.DB.Create(o).Error; err != nil {
		return nil, err
	}
	audit(o.OrderNo, "create", "system", "创建网约车订单, 预估¥"+fmt.Sprintf("%.2f", o.EstimateFare))

	// 加入待接单 GEO 池（工单17 地图订单标记）
	ctx := context.Background()
	if o.ID > 0 {
		_ = geo.I().Add(ctx, orderGeoBucket(o.City), geo.Point{DriverID: o.ID, Lng: o.StartLng, Lat: o.StartLat})
	}

	// 派单推送：GEO 候选 → 定向 WS 推送 + 事件总线广播（工单15：消息队列广播给附近司机）
	pushToCandidates(o)

	// 写幂等键
	if idemKey != "" {
		snap, _ := json.Marshal(o)
		model.DB.Create(&model.IdemKey{Key: idemKey, Result: string(snap)})
	}
	return o, nil
}

// pushToCandidates 计算候选并推送（工单11 派单引擎 + 工单17 新订单提醒）
func pushToCandidates(o *model.RideOrder) {
	cands, err := Dispatch(o)
	if err != nil {
		return
	}
	payload := map[string]interface{}{
		"orderNo": o.OrderNo, "startName": o.StartName, "destName": o.DestName,
		"estimateFare": o.EstimateFare, "distanceKm": o.DistanceKm,
		"passengerScore": o.PassengerScore, "departAt": o.DepartAt,
		"startLng": o.StartLng, "startLat": o.StartLat,
	}
	hub := ws.Global()
	for i, cd := range cands { // 规则a：按得分/用车时间排序依次推送（前5名）
		if i >= 5 {
			break
		}
		msg, _ := json.Marshal(map[string]interface{}{"type": "new_order", "data": payload, "score": cd.Score})
		hub.SendTo("driver-"+strconv.Itoa(int(cd.DriverID)), msg)
	}
	publishJSON(bus.EvOrderCreated, payload)
}

// ---------------- 接单大厅（工单17） ----------------

// HallOrder 大厅订单条目（含经纬度，地图绘制 Marker）
type HallOrder struct {
	model.RideOrder
	DistanceKmFromDriver float64 `json:"distanceKmFromDriver"` // 距司机距离（排序/筛选维度）
}

// ListHallOrders 接单大厅：附近可接订单 + 筛选排序 + 分页（工单17：GET /orders/list）
// filter: minDistance/maxDistance(km)、minFare、minScore；sortBy: distance/fare/score
func ListHallOrders(driverID uint, city string, lng, lat float64, minFare, maxDistanceKm, minScore float64, sortBy string, page, pageSize int) ([]HallOrder, int64, error) {
	ctx := context.Background()
	near, err := geo.I().Nearby(ctx, orderGeoBucket(city), lng, lat, math.Max(maxDistanceKm, 0.1), 200)
	if err != nil {
		return nil, 0, err
	}
	if len(near) == 0 {
		return []HallOrder{}, 0, nil
	}
	ids := make([]uint, 0, len(near))
	distMap := map[uint]float64{}
	for _, n := range near {
		ids = append(ids, n.ID)
		distMap[n.ID] = n.Distance
	}
	var orders []model.RideOrder
	if err := model.DB.Where("id IN ? AND status = ?", ids, model.RidePending).Find(&orders).Error; err != nil {
		return nil, 0, err
	}
	list := make([]HallOrder, 0, len(orders))
	for _, o := range orders {
		if o.EstimateFare < minFare || o.PassengerScore < minScore { // 工单17 筛选（如仅显示预估金额>50）
			continue
		}
		list = append(list, HallOrder{RideOrder: o, DistanceKmFromDriver: round2(distMap[o.ID])})
	}
	// 排序（工单17：按距离、预估收入、乘客评分）
	switch sortBy {
	case "fare":
		sort.Slice(list, func(i, j int) bool { return list[i].EstimateFare > list[j].EstimateFare })
	case "score":
		sort.Slice(list, func(i, j int) bool { return list[i].PassengerScore > list[j].PassengerScore })
	default: // distance
		sort.Slice(list, func(i, j int) bool { return list[i].DistanceKmFromDriver < list[j].DistanceKmFromDriver })
	}
	total := int64(len(list))
	// 分页（下拉刷新/上拉加载）
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 10
	}
	start := (page - 1) * pageSize
	if start >= len(list) {
		return []HallOrder{}, total, nil
	}
	end := start + pageSize
	if end > len(list) {
		end = len(list)
	}
	return list[start:end], total, nil
}

// ---------------- 抢单/拒单（工单15/17：并发确定性） ----------------

// AcceptRideOrder 司机抢单（工单17：第一次成功，再次尝试提示“订单已被他人接走”）
// 并发确定性：DB 条件更新（WHERE status='pending'）原子扣减，RowsAffected=0 即被他人抢先；
// 事务内二次校验，保证订单只绑定一个司机 ID（工单15 测试用例3）。
func AcceptRideOrder(orderNo string, driverID uint, driverName, plate string) (*model.RideOrder, error) {
	var o model.RideOrder
	if err := model.DB.Where("order_no = ?", orderNo).First(&o).Error; err != nil {
		return nil, errors.New("订单不存在")
	}
	// 工单15：下线司机不可接单
	if DriverStatus(driverID) != "online" && DriverStatus(driverID) != "busy" {
		return nil, errors.New("司机不在线，请先上线")
	}

	res := model.DB.Model(&model.RideOrder{}).
		Where("id = ? AND status = ? AND driver_id = 0", o.ID, model.RidePending).
		Updates(map[string]interface{}{
			"status": model.RideAccepted, "driver_id": driverID,
			"driver_name": driverName, "vehicle_plate": plate,
		})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrOrderTaken // 并发抢单失败（工单17 验收）
	}

	// 移出待接单池 + 派单记录 + 审计 + 广播
	ctx := context.Background()
	_ = geo.I().Remove(ctx, orderGeoBucket(o.City), o.ID)
	model.DB.Model(&model.DispatchLog{}).Where("order_no = ? AND driver_id = ?", orderNo, driverID).
		Update("result", "accepted")
	model.DB.Model(&model.DriverProfile{}).Where("user_id = ?", driverID).Update("online_status", "busy")
	_ = redisx.S().Set(ctx, driverStatusKey(driverID), "busy", 0)
	audit(orderNo, "accept", "driver", fmt.Sprintf("司机%d抢单成功", driverID))
	publishJSON(bus.EvOrderAccepted, map[string]interface{}{"orderNo": orderNo, "driverId": driverID})

	model.DB.Where("order_no = ?", orderNo).First(&o)
	// 通知乘客端（工单17：接单成功后通知乘客端）
	notifyUser("user-"+strconv.Itoa(int(o.PassengerID)), "order_accepted", map[string]interface{}{
		"orderNo": orderNo, "driverId": driverID, "driverName": driverName, "plate": plate,
	})
	return &o, nil
}

// RejectRideOrder 拒单（工单15：释放订单重新派发，日志记录拒单原因及时间）
func RejectRideOrder(orderNo string, driverID uint, reason string) error {
	var o model.RideOrder
	if err := model.DB.Where("order_no = ?", orderNo).First(&o).Error; err != nil {
		return errors.New("订单不存在")
	}
	if o.Status != model.RidePending {
		return errors.New("订单当前不可拒绝")
	}
	audit(orderNo, "reject", "driver", fmt.Sprintf("司机%d拒单 原因:%s 时间:%s", driverID, reason, time.Now().Format(time.RFC3339)))
	// 工单15 风控：频繁拒单记录（敏感操作日志）
	model.DB.Create(&model.OrderAudit{OrderNo: orderNo, Action: "reject_stat", Operator: strconv.Itoa(int(driverID)), Detail: reason})
	// 重新派发：广播给其他附近司机（工单17：状态→未接单，重新触发派单）
	pushToCandidates(&o)
	publishJSON(bus.EvOrderRejected, map[string]interface{}{"orderNo": orderNo, "driverId": driverID, "reason": reason})
	return nil
}

// ---------------- 行程流程（工单15 接乘客 + 工单16 行程开始/到达/账单/取消） ----------------

// arriveAtPickup 到达上车点（工单15：去接乘客→到达上车地点）
func ArriveAtPickup(orderNo string) (*model.RideOrder, error) {
	return transit(orderNo, model.RideAccepted, model.RideArrived, "arrive", "司机到达上车点")
}

// PickupPassenger 接到乘客（工单15：到达上车地点后接到乘客）
func PickupPassenger(orderNo string) (*model.RideOrder, error) {
	return transit(orderNo, model.RideArrived, model.RidePickedUp, "pickup", "接到乘客")
}

// StartTrip 行程开始（工单16：记录开始时间地点、状态→行程中、启动计费、司机状态同步）
func StartTrip(orderNo, startAddr string) (*model.RideOrder, *model.TripBill, error) {
	o, err := transit(orderNo, model.RidePickedUp, model.RideOngoing, "start_trip", "行程开始, 开始计费")
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	o.StartAt = &now
	o.StartAddr = startAddr
	model.DB.Model(o).Updates(map[string]interface{}{"start_at": now, "start_addr": startAddr})
	// 司机状态 → ongoing（工单16：司机状态更新为“行程中”并同步给乘客端）
	if o.DriverID > 0 {
		_ = redisx.S().Set(context.Background(), driverStatusKey(o.DriverID), "ongoing", 0)
		notifyUser("user-"+strconv.Itoa(int(o.PassengerID)), "trip_started", map[string]interface{}{
			"orderNo": orderNo, "startAt": now, "startAddr": startAddr,
		})
	}
	return o, nil, nil
}

// FinishTrip 到达目的地（工单16：停止计费、生成账单、通知乘客支付）
func FinishTrip(orderNo, endAddr string, tollFare float64) (*model.RideOrder, *model.TripBill, error) {
	o, err := transit(orderNo, model.RideOngoing, model.RideArrivedDest, "finish_trip", "到达目的地, 停止计费")
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	o.EndAt = &now
	o.EndAddr = endAddr
	tripMin := 0.0
	if o.StartAt != nil {
		tripMin = now.Sub(*o.StartAt).Minutes()
	}
	if o.MileageKm == 0 { // 未上报里程按直线距离估算
		o.MileageKm = geo.HaversineKm(o.StartLat, o.StartLng, o.DestLat, o.DestLng)
	}
	model.DB.Model(o).Updates(map[string]interface{}{"end_at": now, "end_addr": endAddr, "mileage_km": o.MileageKm})

	// 生成账单（工单16 确认账单模块：基础费用、里程费、时长费等）
	bill := CalcBill(o, tripMin, 0, tollFare)
	if err := model.DB.Create(bill).Error; err != nil {
		return o, nil, err
	}
	model.DB.Model(o).Updates(map[string]interface{}{"bill_status": "waiting", "final_fare": bill.Total})
	// 恢复司机可接单状态（工单16：司机回到可接单状态）
	if o.DriverID > 0 {
		_ = redisx.S().Set(context.Background(), driverStatusKey(o.DriverID), "online", 0)
	}
	invalidateOrderCache(orderNo)
	notifyUser("user-"+strconv.Itoa(int(o.PassengerID)), "trip_finished", map[string]interface{}{
		"orderNo": orderNo, "total": bill.Total, "bill": bill,
	})
	return o, bill, nil
}

// ConfirmBill 确认账单（工单16：司机确认后交易提交平台结算）
func ConfirmBill(orderNo string) (*model.TripBill, error) {
	var bill model.TripBill
	if err := model.DB.Where("order_no = ?", orderNo).Order("id desc").First(&bill).Error; err != nil {
		return nil, errors.New("账单不存在")
	}
	now := time.Now()
	bill.ConfirmedAt = &now
	if err := model.DB.Save(&bill).Error; err != nil {
		return nil, err
	}
	if _, err := transit(orderNo, model.RideArrivedDest, model.RideBilled, "confirm_bill", "司机确认账单, 提交结算"); err != nil {
		return nil, err
	}
	invalidateOrderCache(orderNo)
	return &bill, nil
}

// DisputeBill 账单异议上报（工单16：账单有异议可向平台报告）
func DisputeBill(orderNo, reason string) error {
	var bill model.TripBill
	if err := model.DB.Where("order_no = ?", orderNo).Order("id desc").First(&bill).Error; err != nil {
		return errors.New("账单不存在")
	}
	bill.Disputed = true
	bill.DisputeReason = reason
	if err := model.DB.Save(&bill).Error; err != nil {
		return err
	}
	audit(orderNo, "bill_dispute", "driver", reason)
	return nil
}

// CancelRideOrder 取消订单（工单16：实时通知司机、补偿计算、司机状态恢复、日志记录）
func CancelRideOrder(orderNo, by, reason, idemKey string) (*model.RideOrder, float64, error) {
	// 工单11：取消操作幂等
	if idemKey != "" {
		var k model.IdemKey
		if err := model.DB.Where("key = ?", idemKey).First(&k).Error; err == nil {
			var prev model.RideOrder
			_ = json.Unmarshal([]byte(k.Result), &prev)
			if prev.ID > 0 && prev.Status == model.RideCancelled {
				return &prev, prev.Compensation, nil
			}
		}
	}

	var o model.RideOrder
	if err := model.DB.Where("order_no = ?", orderNo).First(&o).Error; err != nil {
		return nil, 0, errors.New("订单不存在")
	}
	switch o.Status {
	case model.RideCompleted, model.RideCancelled:
		return nil, 0, errors.New("订单已完结，无法取消")
	case model.RideBilled:
		return nil, 0, errors.New("账单已确认，请联系客服处理退款")
	}

	comp := 0.0
	if by == "passenger" && o.Status != model.RidePending {
		comp = CalcCancelCompensation(&o) // 工单16：按取消时间点计算空驶补偿
	}
	now := time.Now()
	o.Status = model.RideCancelled
	o.CancelReason = reason
	o.CancelBy = by
	o.CancelAt = &now
	o.Compensation = comp
	if err := model.DB.Save(&o).Error; err != nil {
		return nil, 0, err
	}

	// 清理与恢复
	ctx := context.Background()
	_ = geo.I().Remove(ctx, orderGeoBucket(o.City), o.ID)
	if o.DriverID > 0 {
		_ = redisx.S().Set(ctx, driverStatusKey(o.DriverID), "online", 0) // 司机迅速恢复可接单状态
		// 实时通知司机（工单16：司机端立即收到通知，含取消原因与补偿方案）
		notifyUser("driver-"+strconv.Itoa(int(o.DriverID)), "order_cancelled", map[string]interface{}{
			"orderNo": orderNo, "reason": reason, "by": by, "compensation": comp,
		})
	}
	audit(orderNo, "cancel", by, fmt.Sprintf("取消原因:%s 补偿:¥%.2f", reason, comp))
	if idemKey != "" {
		snap, _ := json.Marshal(o)
		model.DB.Create(&model.IdemKey{Key: idemKey, Result: string(snap)})
	}
	invalidateOrderCache(orderNo)
	publishJSON(bus.EvOrderStatus, map[string]interface{}{"orderNo": orderNo, "status": "cancelled"})
	return &o, comp, nil
}

// ---------------- 修改目的地/乘车点（工单16 原型涉及） ----------------

// UpdateRideDestination 乘客修改目的地（重算预估价；行程中则同步司机）
func UpdateRideDestination(orderNo, destName string, lng, lat float64) (*model.RideOrder, error) {
	var o model.RideOrder
	if err := model.DB.Where("order_no = ?", orderNo).First(&o).Error; err != nil {
		return nil, errors.New("订单不存在")
	}
	if o.Status == model.RideBilled || o.Status == model.RideCompleted || o.Status == model.RideCancelled {
		return nil, errors.New("订单已完结，无法修改")
	}
	o.DestName, o.DestLng, o.DestLat = destName, lng, lat
	o.DistanceKm = geo.HaversineKm(o.StartLat, o.StartLng, lat, lng)
	est := CalcBill(&model.RideOrder{City: o.City, MileageKm: o.DistanceKm}, o.DistanceKm*2, 0, 0)
	o.EstimateFare = est.Total
	if err := model.DB.Save(&o).Error; err != nil {
		return nil, err
	}
	audit(orderNo, "update_dest", "passenger", "修改目的地为 "+destName)
	if o.DriverID > 0 {
		notifyUser("driver-"+strconv.Itoa(int(o.DriverID)), "dest_changed", map[string]interface{}{
			"orderNo": orderNo, "destName": destName, "lng": lng, "lat": lat,
		})
	}
	invalidateOrderCache(orderNo)
	return &o, nil
}

// UpdateRidePickup 乘客修改乘车点
func UpdateRidePickup(orderNo, startName string, lng, lat float64) (*model.RideOrder, error) {
	var o model.RideOrder
	if err := model.DB.Where("order_no = ?", orderNo).First(&o).Error; err != nil {
		return nil, errors.New("订单不存在")
	}
	if o.Status != model.RidePending && o.Status != model.RideAccepted {
		return nil, errors.New("当前行程状态不可修改乘车点")
	}
	o.StartName, o.StartLng, o.StartLat = startName, lng, lat
	model.DB.Save(&o)
	audit(orderNo, "update_pickup", "passenger", "修改乘车点为 "+startName)
	ctx := context.Background()
	_ = geo.I().Add(ctx, orderGeoBucket(o.City), geo.Point{DriverID: o.ID, Lng: lng, Lat: lat}) // 待接单池位置同步
	if o.DriverID > 0 {
		notifyUser("driver-"+strconv.Itoa(int(o.DriverID)), "pickup_changed", map[string]interface{}{
			"orderNo": orderNo, "startName": startName, "lng": lng, "lat": lat,
		})
	}
	invalidateOrderCache(orderNo)
	return &o, nil
}

// ---------------- 订单中心：列表/详情/改派（工单11） ----------------

// RideOrderQuery 列表查询条件（工单11：来源+状态+创建时间+车牌号组合搜索与多字段排序）
type RideOrderQuery struct {
	Status   string
	Source   string
	Plate    string
	Begin    *time.Time
	End      *time.Time
	SortBy   string // created_at/status/final_fare
	Order    string // asc/desc
	Page     int
	PageSize int
}

// ListRideOrders 订单管理列表（工单11：分页+聚合统计）
func ListRideOrders(q RideOrderQuery) ([]model.RideOrder, int64, map[string]int64, error) {
	db := model.DB.Model(&model.RideOrder{})
	if q.Status != "" {
		db = db.Where("status = ?", q.Status)
	}
	if q.Source != "" {
		db = db.Where("source = ?", q.Source)
	}
	if q.Plate != "" {
		db = db.Where("vehicle_plate = ?", q.Plate)
	}
	if q.Begin != nil {
		db = db.Where("created_at >= ?", *q.Begin)
	}
	if q.End != nil {
		db = db.Where("created_at <= ?", *q.End)
	}
	var total int64
	db.Count(&total)

	// 聚合统计（工单11：列表返回聚合统计）
	stats := map[string]int64{}
	rows, err := model.DB.Model(&model.RideOrder{}).Select("status, count(*) as c").Group("status").Rows()
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var st string
			var c int64
			_ = rows.Scan(&st, &c)
			stats[st] = c
		}
	}

	sortCol := "created_at"
	switch q.SortBy {
	case "status":
		sortCol = "status"
	case "final_fare":
		sortCol = "final_fare"
	}
	dir := "DESC"
	if strings.EqualFold(q.Order, "asc") {
		dir = "ASC"
	}
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize < 1 || q.PageSize > 200 {
		q.PageSize = 10
	}
	var list []model.RideOrder
	err = db.Order(sortCol + " " + dir).Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&list).Error
	return list, total, stats, err
}

// GetRideOrderDetail 订单详情（工单11：基础信息+费用明细+状态变更记录(审计)+缓存优先回源）
func GetRideOrderDetail(orderNo string) (map[string]interface{}, error) {
	// 缓存优先（工单11：缓存命中率 ≥70% 目标）
	ctx := context.Background()
	if raw, err := redisx.S().Get(ctx, orderCacheKey(orderNo)); err == nil && raw != "" {
		var cached map[string]interface{}
		if json.Unmarshal([]byte(raw), &cached) == nil {
			return cached, nil
		}
	}

	var o model.RideOrder
	if err := model.DB.Where("order_no = ?", orderNo).First(&o).Error; err != nil {
		return nil, errors.New("订单不存在")
	}
	var audits []model.OrderAudit
	model.DB.Where("order_no = ? AND action != 'reject_stat'", orderNo).Order("id asc").Find(&audits)
	var bill model.TripBill
	hasBill := model.DB.Where("order_no = ?", orderNo).Order("id desc").First(&bill).Error == nil
	var dispatches []model.DispatchLog
	model.DB.Where("order_no = ?", orderNo).Order("score desc").Limit(5).Find(&dispatches)

	detail := map[string]interface{}{
		"order": o, "audits": audits, "bill": bill, "hasBill": hasBill,
		"dispatchTop": dispatches, // 派单决策理由（工单11 可解释性）
	}
	if raw, err := json.Marshal(detail); err == nil {
		_ = redisx.S().Set(ctx, orderCacheKey(orderNo), string(raw), 60*time.Second)
	}
	return detail, nil
}

// ReassignOrder 订单改派（工单11：触发派单引擎重算并记录改派链路）
func ReassignOrder(orderNo, operator string) (*model.RideOrder, error) {
	var o model.RideOrder
	if err := model.DB.Where("order_no = ?", orderNo).First(&o).Error; err != nil {
		return nil, errors.New("订单不存在")
	}
	if o.Status != model.RideAccepted && o.Status != model.RideArrived {
		return nil, errors.New("仅已接单/已到达状态可改派")
	}
	oldDriver := o.DriverID
	// 释放原司机
	res := model.DB.Model(&o).Where("driver_id = ?", oldDriver).
		Updates(map[string]interface{}{"driver_id": 0, "driver_name": "", "vehicle_plate": "", "status": model.RidePending})
	if res.RowsAffected == 0 {
		return nil, errors.New("改派失败，订单已被处理")
	}
	if oldDriver > 0 {
		_ = redisx.S().Set(context.Background(), driverStatusKey(oldDriver), "online", 0)
	}
	audit(orderNo, "reassign", operator, fmt.Sprintf("从司机%d改派，触发派单引擎重算", oldDriver))
	ctx := context.Background()
	_ = geo.I().Add(ctx, orderGeoBucket(o.City), geo.Point{DriverID: o.ID, Lng: o.StartLng, Lat: o.StartLat})
	pushToCandidates(&o) // 派单引擎重算
	invalidateOrderCache(orderNo)
	model.DB.Where("order_no = ?", orderNo).First(&o)
	return &o, nil
}

// ---------------- 轨迹（工单11：热冷分层 + 回放） ----------------

// TraceReport 轨迹上报：热数据写 Redis List（窗口内增量），超阈值刷盘落库（冷数据）
func TraceReport(orderNo string, lng, lat, speed float64) error {
	ctx := context.Background()
	pt, _ := json.Marshal(map[string]interface{}{"lng": lng, "lat": lat, "speed": speed, "at": time.Now().Unix()})
	if rdb := redisx.RDB; rdb != nil {
		rdb.LPush(ctx, traceHotKey(orderNo), string(pt))
		rdb.LTrim(ctx, traceHotKey(orderNo), 0, 199) // 热窗口 200 点
	} else {
		// 降级：直接落库（冷数据）
		model.DB.Create(&model.TripTrace{OrderNo: orderNo, Lng: lng, Lat: lat, Speed: speed, At: time.Now()})
	}
	return nil
}

// TraceReplay 轨迹回放（工单11：按订单号与时间范围返回稀释轨迹点与关键事件）
func TraceReplay(orderNo string, begin, end *time.Time, stride int) ([]map[string]interface{}, []model.OrderAudit, error) {
	if stride < 1 {
		stride = 1
	}
	points := []map[string]interface{}{}
	ctx := context.Background()
	// 热数据（Redis List）
	if rdb := redisx.RDB; rdb != nil {
		if raws, err := rdb.LRange(ctx, traceHotKey(orderNo), 0, -1).Result(); err == nil {
			for i := len(raws) - 1; i >= 0; i -= stride { // 稀释
				var p map[string]interface{}
				if json.Unmarshal([]byte(raws[i]), &p) == nil {
					points = append(points, p)
				}
			}
		}
	}
	// 冷数据（MySQL）
	if len(points) == 0 {
		db := model.DB.Where("order_no = ?", orderNo)
		if begin != nil {
			db = db.Where("at >= ?", *begin)
		}
		if end != nil {
			db = db.Where("at <= ?", *end)
		}
		var traces []model.TripTrace
		db.Order("at asc").Find(&traces)
		for i := 0; i < len(traces); i += stride {
			points = append(points, map[string]interface{}{
				"lng": traces[i].Lng, "lat": traces[i].Lat, "speed": traces[i].Speed, "at": traces[i].At.Unix(),
			})
		}
	}
	// 关键事件（审计即事件源）
	var events []model.OrderAudit
	model.DB.Where("order_no = ? AND action != 'reject_stat'", orderNo).Order("id asc").Find(&events)
	return points, events, nil
}

// FlushHotTrace 行程结束时热轨迹刷盘（工单11 冷热分层：窗口内增量缓存 + 周期性落库）
func FlushHotTrace(orderNo string) {
	ctx := context.Background()
	if redisx.RDB == nil {
		return
	}
	raws, err := redisx.RDB.LRange(ctx, traceHotKey(orderNo), 0, -1).Result()
	if err != nil || len(raws) == 0 {
		return
	}
	for i := len(raws) - 1; i >= 0; i-- {
		var p struct {
			Lng   float64 `json:"lng"`
			Lat   float64 `json:"lat"`
			Speed float64 `json:"speed"`
			At    int64   `json:"at"`
		}
		_ = json.Unmarshal([]byte(raws[i]), &p)
		model.DB.Create(&model.TripTrace{OrderNo: orderNo, Lng: p.Lng, Lat: p.Lat, Speed: p.Speed, At: time.Unix(p.At, 0)})
	}
	redisx.RDB.Del(ctx, traceHotKey(orderNo))
}

// ---------------- 公共辅助 ----------------

// transit 订单状态原子推进（条件更新保证只允许合法流转）
func transit(orderNo, from, to, action, detail string) (*model.RideOrder, error) {
	res := model.DB.Model(&model.RideOrder{}).
		Where("order_no = ? AND status = ?", orderNo, from).
		Update("status", to)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, fmt.Errorf("订单状态不是 %s，无法流转到 %s", from, to)
	}
	audit(orderNo, action, "driver", detail)
	publishJSON(bus.EvOrderStatus, map[string]interface{}{"orderNo": orderNo, "status": to})
	var o model.RideOrder
	model.DB.Where("order_no = ?", orderNo).First(&o)
	invalidateOrderCache(orderNo)
	return &o, nil
}

func audit(orderNo, action, operator, detail string) {
	model.DB.Create(&model.OrderAudit{OrderNo: orderNo, Action: action, Operator: operator, Detail: detail})
}

func publishJSON(typ string, payload map[string]interface{}) {
	raw, _ := json.Marshal(payload)
	_ = bus.I().Publish(context.Background(), bus.Event{Type: typ, Payload: raw, Timestamp: time.Now().Unix()})
}

// notifyUser 经 WebSocket 定向推送（driver-{id} / user-{id}）
func notifyUser(key, typ string, data map[string]interface{}) {
	msg, _ := json.Marshal(map[string]interface{}{"type": typ, "data": data, "timestamp": time.Now().Unix()})
	ws.Global().SendTo(key, msg)
}

func invalidateOrderCache(orderNo string) {
	_ = redisx.S().Del(context.Background(), orderCacheKey(orderNo)) // 工单11：变更事件精准失效
}

var rng = rand.New(rand.NewSource(time.Now().UnixNano()))

func rand4() string {
	return fmt.Sprintf("%04d", rng.Intn(10000))
}

// 工单：全栈开发-网约车-02-班车管理
// 线路管理、站点管理（上下车站点排重 + 按时间自动排序）、排班管理。
package handler

import (
	"errors"

	"ride-hailing/internal/model"
	"ride-hailing/internal/pkg/resp"
	"ride-hailing/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type RouteHandler struct{}

type routeReq struct {
	Name      string `json:"name" binding:"required"`
	StartName string `json:"startName" binding:"required"`
	EndName   string `json:"endName" binding:"required"`
	Desc      string `json:"desc"`
}

// CreateRoute 创建线路（工单02：线路名称、起点和终点）
func (h *RouteHandler) CreateRoute(c *gin.Context) {
	var req routeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	r := model.Route{Name: req.Name, StartName: req.StartName, EndName: req.EndName, Desc: req.Desc}
	if err := model.DB.Create(&r).Error; err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, r)
}

// ListRoutes 线路列表（分页）
func (h *RouteHandler) ListRoutes(c *gin.Context) {
	var q resp.PageQuery
	_ = c.ShouldBindQuery(&q)
	q.Normalize()
	var total int64
	model.DB.Model(&model.Route{}).Count(&total)
	var routes []model.Route
	model.DB.Limit(q.PageSize).Offset((q.Page - 1) * q.PageSize).Order("id DESC").Find(&routes)
	resp.OK(c, resp.PageResult{List: routes, Total: total, Page: q.Page, PageSize: q.PageSize})
}

// GetRoute 线路详情（含途经站点，按序展示：站点位置与预计到达时间）
func (h *RouteHandler) GetRoute(c *gin.Context) {
	id, err := parseUint(c.Param("id"))
	if err != nil {
		resp.BadRequest(c, "id 非法")
		return
	}
	var route model.Route
	if err := model.DB.First(&route, id).Error; err != nil {
		resp.NotFound(c, "线路不存在")
		return
	}
	var stations []model.RouteStation
	model.DB.Where("route_id = ?", id).Order("seq").Find(&stations)
	resp.OK(c, gin.H{"route": route, "stations": stations}) // 途经站点：列出所有站点、位置及预计到达时间
}

type routeStationsReq struct {
	Stations []service.StationInput `json:"stations" binding:"required,min=1,dive"`
}

// SaveRouteStations 保存线路站点（工单02核心规则：排重 + 按时间自动排序 + 首上站时间为0）
func (h *RouteHandler) SaveRouteStations(c *gin.Context) {
	id, err := parseUint(c.Param("id"))
	if err != nil {
		resp.BadRequest(c, "id 非法")
		return
	}
	var req routeStationsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	var route model.Route
	if err := model.DB.First(&route, id).Error; err != nil {
		resp.NotFound(c, "线路不存在")
		return
	}
	if err := service.SaveRouteStations(id, req.Stations); err != nil {
		resp.BadRequest(c, err.Error()) // 排重失败等信息直接反馈
		return
	}
	var stations []model.RouteStation
	model.DB.Where("route_id = ?", id).Order("seq").Find(&stations)
	resp.OK(c, stations)
}

// ---------- 排班 ----------

type shiftReq struct {
	RouteID    uint   `json:"routeId" binding:"required"`
	VehicleID  uint   `json:"vehicleId" binding:"required"`
	DriverID   uint   `json:"driverId" binding:"required"`
	DepartTime string `json:"departTime" binding:"required"` // RFC3339，前端以日历形式展示（工单04）
	Price      float64 `json:"price" binding:"gt=0"`
	SeatCount  int    `json:"seatCount" binding:"gt=0"`
}

// CreateShift 排班（班线班次设置）
func (h *RouteHandler) CreateShift(c *gin.Context) {
	var req shiftReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	var route model.Route
	if err := model.DB.First(&route, req.RouteID).Error; err != nil {
		resp.BadRequest(c, "线路不存在")
		return
	}
	shift, err := newShift(&req)
	if err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	if err := model.DB.Create(shift).Error; err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, shift)
}

func newShift(req *shiftReq) (*model.Shift, error) {
	t, err := parseTimeStr(req.DepartTime)
	if err != nil {
		return nil, errors.New("departTime 应为 RFC3339 格式，如 2026-10-26T08:00:00+08:00")
	}
	return &model.Shift{
		RouteID: req.RouteID, VehicleID: req.VehicleID, DriverID: req.DriverID,
		DepartTime: t, Price: req.Price, SeatCount: req.SeatCount, Status: "scheduled",
	}, nil
}

// ListShifts 排班列表（按线路筛选，支持日历展示）
func (h *RouteHandler) ListShifts(c *gin.Context) {
	var q resp.PageQuery
	_ = c.ShouldBindQuery(&q)
	q.Normalize()
	qx := model.DB.Model(&model.Shift{})
	if rid := c.Query("routeId"); rid != "" {
		qx = qx.Where("route_id = ?", rid)
	}
	if st := c.Query("status"); st != "" {
		qx = qx.Where("status = ?", st)
	}
	var total int64
	qx.Count(&total)
	var shifts []model.Shift
	qx.Limit(q.PageSize).Offset((q.Page - 1) * q.PageSize).Order("depart_time").Find(&shifts)
	resp.OK(c, resp.PageResult{List: shifts, Total: total, Page: q.Page, PageSize: q.PageSize})
}

var _ = gorm.ErrRecordNotFound

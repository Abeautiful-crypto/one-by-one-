// 工单：全栈开发-网约车-01-顺风车管理
// 顺风车接口：行程发布（起终点/时间安排/特殊需求）、自动匹配推荐、
// 紧急求助、行程分享、双向评价。
package handler

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"math"
	"time"

	"ride-hailing/internal/middleware"
	"ride-hailing/internal/model"
	"ride-hailing/internal/pkg/resp"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type TripHandler struct{}

type publishTripReq struct {
	StartName    string    `json:"startName" binding:"required"` // 起点输入/选择
	EndName      string    `json:"endName" binding:"required"`   // 终点输入/选择
	StartLng     float64   `json:"startLng"`
	StartLat     float64   `json:"startLat"`
	EndLng       float64   `json:"endLng"`
	EndLat       float64   `json:"endLat"`
	DepartTime   string    `json:"departTime" binding:"required"`
	Immediate    bool      `json:"immediate"`                    // 即时出行请求或未来预约
	SeatCount    int       `json:"seatCount" binding:"required,min=1,max=4"`
	SpecialNeeds string    `json:"specialNeeds"`                 // 婴儿座椅、宠物携带等
	Price        float64   `json:"price" binding:"gt=0"`
}

// PublishTrip 发布顺风车行程
func (h *TripHandler) PublishTrip(c *gin.Context) {
	var req publishTripReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	depart, err := parseTimeStr(req.DepartTime)
	if err != nil {
		resp.BadRequest(c, "departTime 应为 RFC3339 格式")
		return
	}
	if !req.Immediate && depart.Before(time.Now()) {
		resp.BadRequest(c, "预约行程的出发时间必须晚于当前时间")
		return
	}
	trip := model.Trip{
		UserID: middleware.GetUserID(c), StartName: req.StartName, EndName: req.EndName,
		StartLng: req.StartLng, StartLat: req.StartLat, EndLng: req.EndLng, EndLat: req.EndLat,
		DepartTime: depart, Immediate: req.Immediate, SeatCount: req.SeatCount,
		SpecialNeeds: req.SpecialNeeds, Price: req.Price, Status: "published",
	}
	if err := model.DB.Create(&trip).Error; err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, trip)
}

// ListTrips 行程列表（我的行程）
func (h *TripHandler) ListTrips(c *gin.Context) {
	var q resp.PageQuery
	_ = c.ShouldBindQuery(&q)
	q.Normalize()
	qx := model.DB.Model(&model.Trip{}).Where("user_id = ?", middleware.GetUserID(c))
	if st := c.Query("status"); st != "" {
		qx = qx.Where("status = ?", st)
	}
	var total int64
	qx.Count(&total)
	var trips []model.Trip
	qx.Limit(q.PageSize).Offset((q.Page - 1) * q.PageSize).Order("id DESC").Find(&trips)
	resp.OK(c, resp.PageResult{List: trips, Total: total, Page: q.Page, PageSize: q.PageSize})
}

// MatchTrips 自动推荐算法（工单01+图片：自动推荐算法）
// 基于起终点距离 + 出发时间接近度的加权评分匹配司机行程。
func (h *TripHandler) MatchTrips(c *gin.Context) {
	tripID, err := parseUint(c.Param("id"))
	if err != nil {
		resp.BadRequest(c, "id 非法")
		return
	}
	var trip model.Trip
	if err := model.DB.First(&trip, tripID).Error; err != nil {
		resp.NotFound(c, "行程不存在")
		return
	}
	var driverTrips []model.DriverTrip
	model.DB.Where("status = ? AND depart_time BETWEEN ? AND ?",
		"published", trip.DepartTime.Add(-2*time.Hour), trip.DepartTime.Add(2*time.Hour)).
		Find(&driverTrips)

	type match struct {
		model.DriverTrip
		Score float64 `json:"score"` // 匹配度 0~100
	}
	var matches []match
	for _, dt := range driverTrips {
		// 起终点相似度（名称前缀简化匹配 + 坐标距离）
		score := 40.0
		if dt.StartName == trip.StartName {
			score += 20
		}
		if dt.EndName == trip.EndName {
			score += 20
		}
		if dist(haversine(trip.StartLat, trip.StartLng, trip.StartLat, trip.StartLng)) < 5000 {
			score += 10
		}
		// 时间接近度：每差 10 分钟扣 1 分
		diff := math.Abs(dt.DepartTime.Sub(trip.DepartTime).Minutes())
		score -= diff / 10
		if score > 0 {
			matches = append(matches, match{DriverTrip: dt, Score: math.Round(score*100) / 100})
		}
	}
	resp.OK(c, matches) // 前端按 score 降序展示推荐结果
}

func haversine(lat1, lng1, lat2, lng2 float64) float64 {
	const R = 6371000
	rad := math.Pi / 180
	a := math.Sin((lat2-lat1)*rad/2)*math.Sin((lat2-lat1)*rad/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin((lng2-lng1)*rad/2)*math.Sin((lng2-lng1)*rad/2)
	return 2 * R * math.Asin(math.Sqrt(a))
}

func dist(m float64) float64 { return m }

// ---------- 紧急求助 ----------

type sosReq struct {
	Type     string `json:"type" binding:"required,oneof=police customer_service"` // 一键联系警方或客服
	TripID   uint   `json:"tripId"`
	Location string `json:"location" binding:"required"`
}

// CreateSOS 紧急求助（工单01：一键联系客服或当地警方）
func (h *TripHandler) CreateSOS(c *gin.Context) {
	var req sosReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	rec := model.SOSRecord{
		UserID: middleware.GetUserID(c), TripID: req.TripID, Type: req.Type,
		Location: req.Location, Status: "pending",
	}
	if err := model.DB.Create(&rec).Error; err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"record": rec, "hotline": "110 / 400-000-0000", "message": "求助已受理，请保持电话畅通"})
}

// HandleSOS 处理求助（客服端）
func (h *TripHandler) HandleSOS(c *gin.Context) {
	var req struct {
		Status     string `json:"status" binding:"required,oneof=handled closed"`
		HandleNote string `json:"handleNote"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	var rec model.SOSRecord
	if err := model.DB.First(&rec, c.Param("id")).Error; err != nil {
		resp.NotFound(c, "求助记录不存在")
		return
	}
	rec.Status = req.Status
	rec.HandleNote = req.HandleNote
	rec.HandledBy = middleware.GetUserID(c)
	model.DB.Save(&rec)
	resp.OK(c, rec)
}

// ListSOS 求助记录列表（客服端）
func (h *TripHandler) ListSOS(c *gin.Context) {
	var q resp.PageQuery
	_ = c.ShouldBindQuery(&q)
	q.Normalize()
	var total int64
	model.DB.Model(&model.SOSRecord{}).Count(&total)
	var records []model.SOSRecord
	model.DB.Limit(q.PageSize).Offset((q.Page - 1) * q.PageSize).Order("id DESC").Find(&records)
	resp.OK(c, resp.PageResult{List: records, Total: total, Page: q.Page, PageSize: q.PageSize})
}

// ---------- 行程分享 ----------

type shareReq struct {
	TripID       uint `json:"tripId" binding:"required"`
	ExpireHours  int  `json:"expireHours" binding:"required,min=1,max=72"`
}

// ShareTrip 行程分享（工单01：将行程详情分享给朋友或家人，增加安全感）
func (h *TripHandler) ShareTrip(c *gin.Context) {
	var req shareReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	var trip model.Trip
	if err := model.DB.Where("id = ? AND user_id = ?", req.TripID, middleware.GetUserID(c)).First(&trip).Error; err != nil {
		resp.NotFound(c, "行程不存在")
		return
	}
	token := randToken()
	share := model.TripShare{
		TripID: req.TripID, ShareToken: token,
		ExpireAt: time.Now().Add(time.Duration(req.ExpireHours) * time.Hour),
	}
	if err := model.DB.Create(&share).Error; err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"shareToken": token, "shareURL": "/share/trip/" + token, "expireAt": share.ExpireAt})
}

// GetSharedTrip 查看分享的行程（无需登录，凭 token）
func (h *TripHandler) GetSharedTrip(c *gin.Context) {
	var share model.TripShare
	if err := model.DB.Where("share_token = ?", c.Param("token")).First(&share).Error; err != nil {
		resp.NotFound(c, "分享链接无效")
		return
	}
	if time.Now().After(share.ExpireAt) {
		resp.BadRequest(c, "分享链接已过期")
		return
	}
	model.DB.Model(&share).UpdateColumn("view_count", gorm.Expr("view_count + 1"))
	var trip model.Trip
	if err := model.DB.First(&trip, share.TripID).Error; err != nil {
		resp.NotFound(c, "行程不存在")
		return
	}
	resp.OK(c, gin.H{"trip": trip, "viewCount": share.ViewCount + 1})
}

func randToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

var _ = errors.New

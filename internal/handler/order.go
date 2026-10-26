// 工单：全栈开发-网约车-04-订单管理 & 07-营销管理（优惠券在下单时使用）
// 订单接口：创建订单（班车/顺风车）、筛选列表、详情、状态流转、状态历史、双向评价。
package handler

import (
	"errors"
	"fmt"
	"time"

	"ride-hailing/internal/middleware"
	"ride-hailing/internal/model"
	"ride-hailing/internal/pkg/resp"
	"ride-hailing/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type OrderHandler struct{}

type createOrderReq struct {
	OrderType    string `json:"orderType" binding:"required,oneof=bus carpool"`
	ShiftID      uint   `json:"shiftId"` // 班车订单必填
	TripID       uint   `json:"tripId"`  // 顺风车订单必填
	StartName    string `json:"startName" binding:"required"`
	EndName      string `json:"endName" binding:"required"`
	ContactName  string `json:"contactName" binding:"required"`
	ContactPhone string `json:"contactPhone" binding:"required,len=11"`
	UserCouponID uint   `json:"userCouponId"` // 使用的用户优惠券
	PayMethod    string `json:"payMethod" binding:"required,oneof=wechat alipay bank_card credit_card e_wallet"`
}

// CreateOrder 创建订单（购票：工单04 购票记录管理）
// 加密传输：支付相关信息通过 SSL/TLS 安全通道传输（服务端启用 HTTPS）。
func (h *OrderHandler) CreateOrder(c *gin.Context) {
	var req createOrderReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	userID := middleware.GetUserID(c)

	var order *model.Order
	var userCoupon *model.UserCoupon
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var amount float64
		switch req.OrderType {
		case model.OrderTypeBus:
			var shift model.Shift
			if err := tx.First(&shift, req.ShiftID).Error; err != nil {
				return errors.New("班次不存在")
			}
			if shift.SoldCount >= shift.SeatCount {
				return errors.New("班次座位已售罄") // 并发场景：事务 + 行数校验
			}
			amount = shift.Price
			if err := tx.Model(&shift).UpdateColumn("sold_count", gorm.Expr("sold_count + 1")).Error; err != nil {
				return err
			}
		default: // carpool
			var trip model.Trip
			if err := tx.First(&trip, req.TripID).Error; err != nil {
				return errors.New("顺风车行程不存在")
			}
			if trip.Status != "published" {
				return errors.New("行程不可下单")
			}
			amount = trip.Price
		}

		// 优惠券核销（工单07：不可叠加 —— 一单仅可用一张）
		discount := 0.0
		if req.UserCouponID > 0 {
			var uc model.UserCoupon
			if err := tx.Where("id = ? AND user_id = ?", req.UserCouponID, userID).First(&uc).Error; err != nil {
				return errors.New("优惠券不存在")
			}
			if uc.Status != model.CouponUnused {
				return errors.New("优惠券不可用（已使用或已过期）")
			}
			var coupon model.Coupon
			if err := tx.First(&coupon, uc.CouponID).Error; err != nil {
				return errors.New("优惠券模板不存在")
			}
			d, err := service.ValidateAndCalc(&coupon, amount, "", "")
			if err != nil {
				return err
			}
			discount = d
			now := time.Now()
			if err := tx.Model(&uc).Updates(map[string]interface{}{"status": model.CouponUsed, "used_at": &now}).Error; err != nil {
				return err
			}
			userCoupon = &uc
		}

		order = &model.Order{
			OrderNo: genOrderNo(), OrderType: req.OrderType, UserID: userID,
			ShiftID: req.ShiftID, TripID: req.TripID,
			StartName: req.StartName, EndName: req.EndName,
			ContactName: req.ContactName, ContactPhone: req.ContactPhone,
			Amount: amount, Discount: discount, CouponID: req.UserCouponID,
			PayMethod: req.PayMethod, Status: model.OrderPending,
		}
		if err := tx.Create(order).Error; err != nil {
			return err
		}
		if userCoupon != nil {
			tx.Model(userCoupon).Update("order_id", order.ID)
		}
		return changeOrderStatusTx(tx, order, model.OrderPending, "", "用户下单")
	})
	if err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	resp.OK(c, order)
}

func changeOrderStatusTx(tx *gorm.DB, order *model.Order, to, reason, operator string) error {
	from := order.Status
	order.Status = to
	if err := tx.Save(order).Error; err != nil {
		return err
	}
	return tx.Create(&model.OrderStatusLog{OrderID: order.ID, FromStatus: from, ToStatus: to, Reason: reason, Operator: operator}).Error
}

func genOrderNo() string {
	return fmt.Sprintf("ORD%s%06d", time.Now().Format("20060102150405"), time.Now().UnixNano()%1000000)
}

// PayOrder 支付订单（模拟支付回调，写交易流水 —— 工单03：实时同步交易信息）
func (h *OrderHandler) PayOrder(c *gin.Context) {
	id, err := parseUint(c.Param("id"))
	if err != nil {
		resp.BadRequest(c, "id 非法")
		return
	}
	var order model.Order
	if err := model.DB.First(&order, id).Error; err != nil {
		resp.NotFound(c, "订单不存在")
		return
	}
	if order.Status != model.OrderPending {
		resp.BadRequest(c, "订单状态不允许支付")
		return
	}
	paid := order.Amount - order.Discount
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		order.PaidAmount = paid
		if err := tx.Save(&order).Error; err != nil {
			return err
		}
		if err := changeOrderStatusTx(tx, &order, model.OrderPaid, "支付成功", "system"); err != nil {
			return err
		}
		txn := &model.Transaction{
			OrderNo: order.OrderNo, OrderID: order.ID, UserID: order.UserID,
			Direction: model.DirectionIncome, Amount: paid, PayMethod: order.PayMethod,
			TxnType: "pay", TxnTime: time.Now(),
		}
		return service.RecordTransaction(tx, txn) // 自动触发异常检测
	})
	if err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, order)
}

type orderListReq struct {
	resp.PageQuery
	OrderType string `form:"orderType"` // bus/carpool 筛选区分（工单04）
	Status    string `form:"status"`
	Keyword   string `form:"keyword"` // 订单号/联系人/手机号
}

// ListOrders 订单列表（筛选区分订单以及详情信息；动态刷新由前端轮询实现）
func (h *OrderHandler) ListOrders(c *gin.Context) {
	var req orderListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	req.Normalize()
	q := model.DB.Model(&model.Order{})
	// 普通用户仅可见自己的订单；管理角色可见全部
	if !hasStaffRole(c) {
		q = q.Where("user_id = ?", middleware.GetUserID(c))
	}
	if req.OrderType != "" {
		q = q.Where("order_type = ?", req.OrderType)
	}
	if req.Status != "" {
		q = q.Where("status = ?", req.Status)
	}
	if req.Keyword != "" {
		kw := "%" + req.Keyword + "%"
		q = q.Where("order_no LIKE ? OR contact_name LIKE ? OR contact_phone LIKE ?", kw, kw, kw)
	}
	var total int64
	q.Count(&total)
	var orders []model.Order
	q.Limit(req.PageSize).Offset((req.Page - 1) * req.PageSize).Order("id DESC").Find(&orders)
	resp.OK(c, resp.PageResult{List: orders, Total: total, Page: req.Page, PageSize: req.PageSize})
}

func hasStaffRole(c *gin.Context) bool {
	for _, r := range middleware.GetRoles(c) {
		if r == "admin" || r == "dispatcher" || r == "ticket_checker" || r == "contact" {
			return true
		}
	}
	return false
}

// GetOrder 订单详情：乘客信息、司机信息、车辆信息、状态、取消原因
func (h *OrderHandler) GetOrder(c *gin.Context) {
	id, err := parseUint(c.Param("id"))
	if err != nil {
		resp.BadRequest(c, "id 非法")
		return
	}
	var order model.Order
	if err := model.DB.First(&order, id).Error; err != nil {
		resp.NotFound(c, "订单不存在")
		return
	}
	if !hasStaffRole(c) && order.UserID != middleware.GetUserID(c) {
		resp.Forbidden(c, "无权查看该订单")
		return
	}
	// 司机信息 + 评分
	var driver struct {
		RealName string
		Phone    string
		Score    float64
		PlateNo  string
		Model    string
	}
	model.DB.Table("users u").
		Select("u.real_name, u.phone, COALESCE(dp.service_score,5) AS score").
		Joins("LEFT JOIN driver_profiles dp ON dp.user_id = u.id").
		Where("u.id = ?", order.DriverID).Scan(&driver)
	if order.ShiftID > 0 {
		model.DB.Table("vehicles v").
			Select("v.plate_no, v.model").
			Joins("JOIN shifts s ON s.vehicle_id = v.id AND s.id = ?", order.ShiftID).
			Scan(&driver)
	}
	// 历史变更记录（工单04）
	var logs []model.OrderStatusLog
	model.DB.Where("order_id = ?", order.ID).Order("id").Find(&logs)
	resp.OK(c, gin.H{"order": order, "driver": driver, "statusLogs": logs})
}

// CancelOrder 取消订单（标注取消原因）
func (h *OrderHandler) CancelOrder(c *gin.Context) {
	id, err := parseUint(c.Param("id"))
	if err != nil {
		resp.BadRequest(c, "id 非法")
		return
	}
	var req struct {
		Reason string `json:"reason" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	var order model.Order
	if err := model.DB.First(&order, id).Error; err != nil {
		resp.NotFound(c, "订单不存在")
		return
	}
	if order.Status == model.OrderFinished || order.Status == model.OrderCancelled {
		resp.BadRequest(c, "订单已完成或已取消")
		return
	}
	order.CancelReason = req.Reason
	if err := model.DB.Save(&order).Error; err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	if err := changeOrderStatus(&order, model.OrderCancelled, req.Reason, middleware.GetUsername(c)); err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	// 已支付订单引导走退票流程
	resp.OK(c, gin.H{"order": order, "hint": "已支付订单请申请退票退款"})
}

// ---------- 评价管理（工单01/04：双向评价） ----------

type reviewReq struct {
	OrderID uint   `json:"orderId" binding:"required"`
	Score   int    `json:"score" binding:"required,min=1,max=5"`
	Content string `json:"content" binding:"max=500"`
}

// CreateReview 乘客评价司机
func (h *OrderHandler) CreateReview(c *gin.Context) {
	var req reviewReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	var order model.Order
	if err := model.DB.First(&order, req.OrderID).Error; err != nil {
		resp.NotFound(c, "订单不存在")
		return
	}
	if order.Status != model.OrderFinished {
		resp.BadRequest(c, "订单完成后才能评价")
		return
	}
	var cnt int64
	model.DB.Model(&model.Review{}).Where("order_id = ? AND from_uid = ?", req.OrderID, middleware.GetUserID(c)).Count(&cnt)
	if cnt > 0 {
		resp.BadRequest(c, "您已评价过该订单")
		return
	}
	review := model.Review{
		OrderID: req.OrderID, FromUID: middleware.GetUserID(c), ToUID: order.DriverID,
		Role: "passenger_to_driver", Score: req.Score, Content: req.Content,
	}
	if err := model.DB.Create(&review).Error; err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	// 更新司机服务评分（平均分）
	var avg struct{ Score float64 }
	model.DB.Model(&model.Review{}).Select("COALESCE(AVG(score),5) AS score").Where("to_uid = ?", order.DriverID).Scan(&avg)
	model.DB.Model(&model.DriverProfile{}).Where("user_id = ?", order.DriverID).Update("service_score", avg.Score)
	resp.OK(c, review)
}

// ListReviews 评价管理列表（管理端审核视角）
func (h *OrderHandler) ListReviews(c *gin.Context) {
	var q resp.PageQuery
	_ = c.ShouldBindQuery(&q)
	q.Normalize()
	var total int64
	model.DB.Model(&model.Review{}).Count(&total)
	var reviews []model.Review
	model.DB.Limit(q.PageSize).Offset((q.Page - 1) * q.PageSize).Order("id DESC").Find(&reviews)
	resp.OK(c, resp.PageResult{List: reviews, Total: total, Page: q.Page, PageSize: q.PageSize})
}

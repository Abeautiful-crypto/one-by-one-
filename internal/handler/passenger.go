// 工单12/13/14：乘客端接口（登录注册账户、实时打车辅助、支付评价打赏取消）
package handler

import (
	"strconv"
	"time"

	"ride-hailing/internal/middleware"
	"ride-hailing/internal/model"
	"ride-hailing/internal/pkg/resp"
	"ride-hailing/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// PassengerHandler 乘客端接口
type PassengerHandler struct{}

// ---------------- 工单12：验证码登录 / 找回密码 ----------------

type smsCodeReq struct {
	Phone   string `json:"phone" binding:"required,len=11"`
	Purpose string `json:"purpose" binding:"required,oneof=login reset"` // login/reset
}

// SendSmsCode 发送验证码（演示：返回 code 便于联调；生产接 SMS 网关）
func (h *PassengerHandler) SendSmsCode(c *gin.Context) {
	var req smsCodeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	code, err := service.SendSmsCode(req.Phone, req.Purpose)
	if err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"message": "验证码已发送", "devCode": code}) // devCode 仅演示环境暴露
}

type smsLoginReq struct {
	Phone string `json:"phone" binding:"required,len=11"`
	Code  string `json:"code" binding:"required,len=6"`
}

// SmsLogin 手机验证码登录（工单12：即时手机验证）
func (h *PassengerHandler) SmsLogin(c *gin.Context) {
	var req smsLoginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	token, err := service.LoginBySms(req.Phone, req.Code)
	if err != nil {
		resp.Unauthorized(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"token": token})
}

type resetPwdReq struct {
	Phone   string `json:"phone" binding:"required,len=11"`
	Code    string `json:"code" binding:"required,len=6"`
	NewPwd  string `json:"newPassword" binding:"required,min=6"`
}

// ResetPassword 验证码找回/重置密码（工单12）
func (h *PassengerHandler) ResetPassword(c *gin.Context) {
	var req resetPwdReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	if err := service.ResetPasswordByCode(req.Phone, req.Code, req.NewPwd); err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"message": "密码重置成功，请重新登录"})
}

// ---------------- 工单12：账户信息 ----------------

type profileReq struct {
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
	Email    string `json:"email"`
}

// GetProfile 获取账户信息（工单12：个人信息/积分/余额/状态）
func (h *PassengerHandler) GetProfile(c *gin.Context) {
	uid := middleware.GetUserID(c)
	var u model.User
	if err := model.DB.First(&u, uid).Error; err != nil {
		resp.NotFound(c, "用户不存在")
		return
	}
	resp.OK(c, gin.H{
		"username": u.Username, "realName": u.RealName, "nickname": u.Nickname,
		"avatar": u.Avatar, "phone": u.Phone, "email": u.Email,
		"balance": u.Balance, "points": u.Points, "status": u.Status,
	})
}

// UpdateProfile 个人信息编辑（昵称/头像/邮箱，工单12）
func (h *PassengerHandler) UpdateProfile(c *gin.Context) {
	uid := middleware.GetUserID(c)
	var req profileReq
	_ = c.ShouldBindJSON(&req)
	updates := map[string]interface{}{}
	if req.Nickname != "" {
		updates["nickname"] = req.Nickname
	}
	if req.Avatar != "" {
		updates["avatar"] = req.Avatar
	}
	if req.Email != "" {
		updates["email"] = req.Email
	}
	if err := model.DB.Model(&model.User{}).Where("id = ?", uid).Updates(updates).Error; err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	var u model.User
	model.DB.First(&u, uid)
	resp.OK(c, gin.H{"nickname": u.Nickname, "avatar": u.Avatar, "email": u.Email, "points": u.Points, "balance": u.Balance})
}

type changePwdReq struct {
	OldPwd  string `json:"oldPassword" binding:"required"`
	NewPwd  string `json:"newPassword" binding:"required,min=6"`
}

// ChangePassword 安全设置：修改密码（工单12）
func (h *PassengerHandler) ChangePassword(c *gin.Context) {
	uid := middleware.GetUserID(c)
	var req changePwdReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	var u model.User
	model.DB.First(&u, uid)
	if !u.CheckPassword(req.OldPwd) {
		resp.BadRequest(c, "原密码不正确")
		return
	}
	if err := u.SetPassword(req.NewPwd); err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	model.DB.Model(&u).Update("password", u.Password)
	resp.OK(c, gin.H{"message": "密码修改成功"})
}

// ---------------- 工单12/13：常用地址 ----------------

type addrReq struct {
	Label     string  `json:"label" binding:"required,oneof=home work other"`
	Name      string  `json:"name" binding:"required"`
	Phone     string  `json:"phone"`
	Address   string  `json:"address" binding:"required"`
	Lng       float64 `json:"lng"`
	Lat       float64 `json:"lat"`
	IsDefault bool    `json:"isDefault"`
}

// ListAddresses 常用地址列表
func (h *PassengerHandler) ListAddresses(c *gin.Context) {
	uid := middleware.GetUserID(c)
	var list []model.UserAddress
	model.DB.Where("user_id = ?", uid).Find(&list)
	resp.OK(c, list)
}

// CreateAddress 新增常用地址
func (h *PassengerHandler) CreateAddress(c *gin.Context) {
	uid := middleware.GetUserID(c)
	var req addrReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	a := model.UserAddress{UserID: uid, Label: req.Label, Name: req.Name, Phone: req.Phone,
		Address: req.Address, Lng: req.Lng, Lat: req.Lat, IsDefault: req.IsDefault}
	if req.IsDefault {
		model.DB.Model(&model.UserAddress{}).Where("user_id = ?", uid).Update("is_default", false)
	}
	model.DB.Create(&a)
	resp.OK(c, a)
}

// UpdateAddress 更新常用地址
func (h *PassengerHandler) UpdateAddress(c *gin.Context) {
	uid := middleware.GetUserID(c)
	var req addrReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	if req.IsDefault {
		model.DB.Model(&model.UserAddress{}).Where("user_id = ?", uid).Update("is_default", false)
	}
	model.DB.Model(&model.UserAddress{}).Where("user_id = ? AND id = ?", uid, parseID(c.Param("id"))).
		Updates(map[string]interface{}{"label": req.Label, "name": req.Name, "phone": req.Phone,
			"address": req.Address, "lng": req.Lng, "lat": req.Lat, "is_default": req.IsDefault})
	resp.OK(c, gin.H{"message": "updated"})
}

// DeleteAddress 删除常用地址
func (h *PassengerHandler) DeleteAddress(c *gin.Context) {
	uid := middleware.GetUserID(c)
	model.DB.Where("user_id = ? AND id = ?", uid, parseID(c.Param("id"))).Delete(&model.UserAddress{})
	resp.OK(c, gin.H{"message": "deleted"})
}

// ---------------- 工单12：支付方式管理 ----------------

type payReq struct {
	Type     string `json:"type" binding:"required,oneof=bank alipay wechat"`
	Account  string `json:"account" binding:"required"`
	Holder   string `json:"holder"`
	Expiry   string `json:"expiry"`
	IsDefault bool  `json:"isDefault"`
}

// ListPayments 支付方式列表
func (h *PassengerHandler) ListPayments(c *gin.Context) {
	uid := middleware.GetUserID(c)
	var list []model.PaymentMethod
	model.DB.Where("user_id = ?", uid).Find(&list)
	resp.OK(c, list)
}

// CreatePayment 新增支付方式
func (h *PassengerHandler) CreatePayment(c *gin.Context) {
	uid := middleware.GetUserID(c)
	var req payReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	if req.IsDefault {
		model.DB.Model(&model.PaymentMethod{}).Where("user_id = ?", uid).Update("is_default", false)
	}
	p := model.PaymentMethod{UserID: uid, Type: req.Type, Account: maskAccount(req.Account),
		Holder: req.Holder, Expiry: req.Expiry, IsDefault: req.IsDefault}
	model.DB.Create(&p)
	resp.OK(c, p)
}

// DeletePayment 删除支付方式
func (h *PassengerHandler) DeletePayment(c *gin.Context) {
	uid := middleware.GetUserID(c)
	model.DB.Where("user_id = ? AND id = ?", uid, parseID(c.Param("id"))).Delete(&model.PaymentMethod{})
	resp.OK(c, gin.H{"message": "deleted"})
}

// ---------------- 工单12：帮助与反馈 ----------------

type feedbackReq struct {
	Type    string `json:"type" binding:"required,oneof=bug suggest complaint"`
	Content string `json:"content" binding:"required"`
	Contact string `json:"contact"`
}

// SubmitFeedback 提交反馈
func (h *PassengerHandler) SubmitFeedback(c *gin.Context) {
	uid := middleware.GetUserID(c)
	var req feedbackReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	model.DB.Create(&model.UserFeedback{UserID: uid, Type: req.Type, Content: req.Content, Contact: req.Contact})
	resp.OK(c, gin.H{"message": "反馈已提交，感谢您的建议"})
}

// ---------------- 工单13：附近司机 / 司机详情 ----------------

// NearbyDrivers 附近可用车辆（地图展示：评分/车牌/距离）
func (h *PassengerHandler) NearbyDrivers(c *gin.Context) {
	city := c.DefaultQuery("city", "上海")
	lng, _ := strconv.ParseFloat(c.DefaultQuery("lng", "121.4737"), 64)
	lat, _ := strconv.ParseFloat(c.DefaultQuery("lat", "31.2304"), 64)
	radius, _ := strconv.ParseFloat(c.DefaultQuery("radiusKm", "3"), 64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	drivers, err := service.ListNearbyDrivers(city, lng, lat, radius, limit)
	if err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"count": len(drivers), "drivers": drivers})
}

// DriverDetail 司机详情（评分/完成单数/车牌/车辆）
func (h *PassengerHandler) DriverDetail(c *gin.Context) {
	id := parseID(c.Param("id"))
	var dp model.DriverProfile
	model.DB.Where("user_id = ?", id).First(&dp)
	var v model.Vehicle
	model.DB.Where("driver_id = ?", id).First(&v)
	var u model.User
	model.DB.Where("id = ?", id).First(&u)
	resp.OK(c, gin.H{
		"driverId": id, "name": u.RealName, "rating": dp.ServiceScore,
		"reviewCount": dp.ReviewCount, "plateNo": v.PlateNo, "vehicleType": v.VehicleType,
		"online": service.DriverStatus(id),
	})
}

// ---------------- 工单13：紧急求助 SOS ----------------

type rideSosReq struct {
	OrderNo string  `json:"orderNo"`
	Lng      float64 `json:"lng"`
	Lat      float64 `json:"lat"`
	Address  string  `json:"address"`
}

// CreateSOS 一键求助（立即通知管理员/司机）
func (h *PassengerHandler) CreateSOS(c *gin.Context) {
	uid := middleware.GetUserID(c)
	var req rideSosReq
	_ = c.ShouldBindJSON(&req)
	s, err := service.CreateSOS(uid, req.OrderNo, req.Lng, req.Lat, req.Address)
	if err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, s)
}

// ---------------- 工单14：支付评价打赏取消 ----------------

type tipReq struct {
	Amount float64 `json:"amount" binding:"required,gt=0"`
	Thanks string  `json:"thanks"`
}

// TipDriver 打赏司机（即时到账，工单14）
func (h *PassengerHandler) TipDriver(c *gin.Context) {
	uid := middleware.GetUserID(c)
	orderNo := c.Param("orderNo")
	var o model.RideOrder
	if err := model.DB.Where("order_no = ?", orderNo).First(&o).Error; err != nil {
		resp.NotFound(c, "订单不存在")
		return
	}
	var req tipReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	if err := service.CreateTip(orderNo, uid, o.DriverID, req.Amount, req.Thanks); err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"message": "打赏成功，金额已即时到账司机账户", "amount": req.Amount})
}

type rideReviewReq struct {
	Rating    int    `json:"rating" binding:"required,min=1,max=5"`
	Comment   string `json:"comment"`
	Anonymous bool   `json:"anonymous"`
}

// CreateReview 行程评价（5星+评论+匿名，工单14）
func (h *PassengerHandler) CreateReview(c *gin.Context) {
	uid := middleware.GetUserID(c)
	orderNo := c.Param("orderNo")
	var o model.RideOrder
	if err := model.DB.Where("order_no = ?", orderNo).First(&o).Error; err != nil {
		resp.NotFound(c, "订单不存在")
		return
	}
	var req rideReviewReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	r := &model.RideReview{OrderNo: orderNo, PassengerID: uid, DriverID: o.DriverID,
		Rating: req.Rating, Comment: req.Comment, Anonymous: req.Anonymous}
	if err := service.CreateReview(r); err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"message": "评价已提交，审核通过后公开", "reviewId": r.ID, "status": r.Status})
}

// ListReviews 评价列表（含审核状态，管理端）
func (h *PassengerHandler) ListReviews(c *gin.Context) {
	var list []model.RideReview
	model.DB.Order("created_at desc").Limit(100).Find(&list)
	resp.OK(c, list)
}

// ApproveReview 审核通过评价（工单14）
func (h *PassengerHandler) ApproveReview(c *gin.Context) {
	id := parseID(c.Param("id"))
	if err := service.ApproveReview(id); err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"message": "已通过并公开"})
}

// RejectReview 审核拒绝评价
func (h *PassengerHandler) RejectReview(c *gin.Context) {
	id := parseID(c.Param("id"))
	if err := service.RejectReview(id); err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"message": "已拒绝"})
}

type invoiceReq struct {
	Type  string `json:"type" binding:"required,oneof=electronic paper"`
	Title string `json:"title" binding:"required"`
	TaxNo string `json:"taxNo"`
	Email string `json:"email"`
}

// CreateInvoice 申请发票（工单14）
func (h *PassengerHandler) CreateInvoice(c *gin.Context) {
	uid := middleware.GetUserID(c)
	var req invoiceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	inv, err := service.CreateInvoice(uid, c.Param("orderNo"), req.Type, req.Title, req.TaxNo, req.Email)
	if err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, inv)
}

// ---------------- 工单12：行程记录（乘客历史订单） ----------------

// PassengerOrders 乘客行程记录（按时间倒序）
func (h *PassengerHandler) PassengerOrders(c *gin.Context) {
	uid := middleware.GetUserID(c)
	var list []model.RideOrder
	model.DB.Where("passenger_id = ?", uid).Order("created_at desc").
		Limit(50).Find(&list)
	resp.OK(c, gin.H{"count": len(list), "orders": list})
}

func parseID(s string) uint {
	v, _ := strconv.ParseUint(s, 10, 64)
	return uint(v)
}

// maskAccount 银行卡/账号掩码存储（工单12：安全设置）
func maskAccount(acc string) string {
	if len(acc) <= 4 {
		return "****"
	}
	return "****" + acc[len(acc)-4:]
}

var _ = time.Now
var _ = gorm.ErrRecordNotFound

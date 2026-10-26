// 工单：全栈开发-网约车-07-营销管理
// 优惠券接口：创建与配置（折扣券/现金券/免费乘车券）、发布、发放（自动规则/手动/推广发码/推荐奖励）、
// 关键词搜索、收藏、分享、删除引用校验。
package handler

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"time"

	"ride-hailing/internal/middleware"
	"ride-hailing/internal/model"
	"ride-hailing/internal/pkg/resp"
	"ride-hailing/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type CouponHandler struct{}

type couponReq struct {
	Name        string    `json:"name" binding:"required"`
	Type        string    `json:"type" binding:"required,oneof=discount cash free"`
	Value       float64   `json:"value" binding:"gt=0"`
	MinConsume  float64   `json:"minConsume"`  // 最低消费要求
	MaxDiscount float64   `json:"maxDiscount"` // 折扣券优惠上限
	City        string    `json:"city"`        // 适用城市/区域
	VehicleType string    `json:"vehicleType" binding:"omitempty,oneof=economy luxury"`
	Stackable   bool      `json:"stackable"`
	ValidFrom   time.Time `json:"validFrom" binding:"required"`
	ValidTo     time.Time `json:"validTo" binding:"required,gtfield=ValidFrom"` // 使用范围及有效期
	TotalCount  int       `json:"totalCount" binding:"required,min=1"`
	Desc        string    `json:"desc"`
}

// CreateCoupon 创建优惠券
func (h *CouponHandler) CreateCoupon(c *gin.Context) {
	var req couponReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	// 策略合法性预检（策略模式）
	if _, err := service.GetCouponStrategy(req.Type); err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	cp := model.Coupon{
		Name: req.Name, Type: req.Type, Value: req.Value, MinConsume: req.MinConsume,
		MaxDiscount: req.MaxDiscount, City: req.City, VehicleType: req.VehicleType,
		Stackable: req.Stackable, ValidFrom: req.ValidFrom, ValidTo: req.ValidTo,
		TotalCount: req.TotalCount, Status: "offline", Desc: req.Desc, // 默认未发布
	}
	if err := model.DB.Create(&cp).Error; err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, cp)
}

// ListCoupons 优惠券列表：关键词搜索 + 类型筛选，即时反馈（缓存加速，工单07）
func (h *CouponHandler) ListCoupons(c *gin.Context) {
	var q resp.PageQuery
	_ = c.ShouldBindQuery(&q)
	q.Normalize()

	// 缓存：关键词命中场景频繁（工单07：索引优化 + 缓存机制）
	ck := fmt.Sprintf("coupon_list:%s:%s", c.Query("keyword"), c.Query("type"))
	if v, ok := AppCache.Get(ck); ok {
		resp.OK(c, v)
		return
	}

	qx := model.DB.Model(&model.Coupon{})
	if kw := c.Query("keyword"); kw != "" { // 关键词搜索：折扣、现金、免费乘车等
		k := "%" + kw + "%"
		qx = qx.Where("name LIKE ? OR `desc` LIKE ? OR type LIKE ?", k, k, k)
	}
	if t := c.Query("type"); t != "" {
		qx = qx.Where("type = ?", t)
	}
	if st := c.Query("status"); st != "" {
		qx = qx.Where("status = ?", st)
	}
	var total int64
	qx.Count(&total)
	var coupons []model.Coupon
	qx.Limit(q.PageSize).Offset((q.Page - 1) * q.PageSize).Order("id DESC").Find(&coupons)
	result := resp.PageResult{List: coupons, Total: total, Page: q.Page, PageSize: q.PageSize}
	AppCache.Set(ck, result, 30*time.Second)
	resp.OK(c, result)
}

// UpdateCoupon 更新（发布/下线）
func (h *CouponHandler) UpdateCoupon(c *gin.Context) {
	id, err := parseUint(c.Param("id"))
	if err != nil {
		resp.BadRequest(c, "id 非法")
		return
	}
	var req couponReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	var cp model.Coupon
	if err := model.DB.First(&cp, id).Error; err != nil {
		resp.NotFound(c, "优惠券不存在")
		return
	}
	cp.Name, cp.Type, cp.Value, cp.MinConsume = req.Name, req.Type, req.Value, req.MinConsume
	cp.MaxDiscount, cp.City, cp.VehicleType, cp.Stackable = req.MaxDiscount, req.City, req.VehicleType, req.Stackable
	cp.ValidFrom, cp.ValidTo, cp.TotalCount, cp.Desc = req.ValidFrom, req.ValidTo, req.TotalCount, req.Desc
	if st := c.Query("publish"); st == "online" || st == "offline" {
		cp.Status = st // 发布优惠券：设置使用范围及有效期
	}
	if err := model.DB.Save(&cp).Error; err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	AppCache.Delete("coupon_list:") // 失效缓存
	resp.OK(c, cp)
}

// DeleteCoupon 删除优惠券（工单07：删除时注意是否被引用，要有校验）
func (h *CouponHandler) DeleteCoupon(c *gin.Context) {
	id, err := parseUint(c.Param("id"))
	if err != nil {
		resp.BadRequest(c, "id 非法")
		return
	}
	var refCnt int64
	model.DB.Model(&model.UserCoupon{}).Where("coupon_id = ? AND status IN ?", id, []string{model.CouponUnused, model.CouponUsed}).Count(&refCnt)
	if refCnt > 0 {
		resp.BadRequest(c, fmt.Sprintf("该优惠券已被 %d 位用户领取/使用，禁止删除，请先下线", refCnt))
		return
	}
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&model.Coupon{}, id).Error; err != nil {
			return err
		}
		tx.Where("coupon_id = ?", id).Delete(&model.CouponFavorite{})
		tx.Where("coupon_id = ?", id).Delete(&model.CouponCode{})
		return nil
	})
	if err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"message": "删除成功"})
}

// ---------- 发放 ----------

type grantReq struct {
	UserID  uint `json:"userId" binding:"required"`
	CouponID uint `json:"couponId" binding:"required"`
}

// GrantCoupon 手动发放（工单07：管理员通过后台手动为特定用户发放）
func (h *CouponHandler) GrantCoupon(c *gin.Context) {
	var req grantReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	uc, err := issueCoupon(req.UserID, req.CouponID, "manual")
	if err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	resp.OK(c, uc)
}

// AutoGrant 自动发放（工单07：根据注册时间、首次使用、累计消费额等条件自动发放）
func (h *CouponHandler) AutoGrant(c *gin.Context) {
	var req struct {
		CouponID uint `json:"couponId" binding:"required"`
		Rule     string `json:"rule" binding:"required,oneof=new_user high_consumer"` // 新用户 / 高消费用户
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	var userIDs []uint
	switch req.Rule {
	case "new_user": // 近 7 天注册用户
		model.DB.Model(&model.User{}).Where("created_at >= ?", time.Now().AddDate(0, 0, -7)).Pluck("id", &userIDs)
	default: // 累计消费 top 100
		model.DB.Model(&model.Order{}).Select("user_id").
			Where("status IN ?", []string{model.OrderPaid, model.OrderFinished}).
			Group("user_id").Order("SUM(paid_amount) DESC").Limit(100).Pluck("user_id", &userIDs)
	}
	granted := 0
	for _, uid := range userIDs {
		if _, err := issueCoupon(uid, req.CouponID, "auto"); err == nil {
			granted++
		}
	}
	resp.OK(c, gin.H{"matched": len(userIDs), "granted": granted})
}

// issueCoupon 发放一张券（库存扣减 + 生成券码）
func issueCoupon(userID, couponID uint, source string) (*model.UserCoupon, error) {
	var cp model.Coupon
	if err := model.DB.First(&cp, couponID).Error; err != nil {
		return nil, errors.New("优惠券不存在")
	}
	if cp.Status != "online" {
		return nil, errors.New("优惠券未发布")
	}
	if cp.IssuedCount >= cp.TotalCount {
		return nil, errors.New("优惠券已发放完毕")
	}
	var cnt int64
	model.DB.Model(&model.UserCoupon{}).Where("user_id = ? AND coupon_id = ?", userID, couponID).Count(&cnt)
	if cnt > 0 {
		return nil, errors.New("该用户已领取过此优惠券")
	}
	uc := &model.UserCoupon{
		UserID: userID, CouponID: couponID, Code: genCouponCode(),
		Status: model.CouponUnused, Source: source, ExpireAt: cp.ValidTo,
	}
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(uc).Error; err != nil {
			return err
		}
		return tx.Model(&cp).UpdateColumn("issued_count", gorm.Expr("issued_count + 1")).Error
	})
	if err != nil {
		return nil, err
	}
	return uc, nil
}

func genCouponCode() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	return "CPN" + hex.EncodeToString([]byte(fmt.Sprintf("%016x", n)))[:16]
}

// ---------- 推广发码 & 推荐奖励 ----------

// CreatePromoCode 生成推广优惠码（工单07：社交媒体、合作伙伴渠道发布优惠码供领取）
func (h *CouponHandler) CreatePromoCode(c *gin.Context) {
	var req struct {
		CouponID uint `json:"couponId" binding:"required"`
		Count    int  `json:"count" binding:"required,min=1,max=1000"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	codes := make([]model.CouponCode, 0, req.Count)
	for i := 0; i < req.Count; i++ {
		codes = append(codes, model.CouponCode{Code: "PROMO" + genCouponCode()[3:], CouponID: req.CouponID})
	}
	if err := model.DB.Create(&codes).Error; err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, codes)
}

// RedeemPromoCode 领取优惠码（推荐奖励：邀请人与被邀请人双方均可获得优惠券）
func (h *CouponHandler) RedeemPromoCode(c *gin.Context) {
	userID := middleware.GetUserID(c)
	var req struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	var pc model.CouponCode
	if err := model.DB.Where("code = ?", req.Code).First(&pc).Error; err != nil {
		resp.NotFound(c, "优惠码不存在")
		return
	}
	if pc.Used {
		resp.BadRequest(c, "优惠码已被使用")
		return
	}
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&pc).Updates(map[string]interface{}{"used": true, "bind_uid": userID, "used_at": time.Now()}).Error; err != nil {
			return err
		}
		if _, err := issueCoupon(userID, pc.CouponID, "activity"); err != nil {
			return err
		}
		// 推荐奖励：若该码绑定了邀请人，双方得券
		if pc.InviterUID > 0 && pc.InviterUID != userID {
			_, _ = issueCoupon(pc.InviterUID, pc.CouponID, "referral")
		}
		return nil
	})
	if err != nil {
		resp.BadRequest(c, err.Error())
		return
	}
	resp.OK(c, gin.H{"message": "领取成功，优惠券已发放"})
}

// ---------- 我的优惠券 / 收藏 / 分享 ----------

// MyCoupons 我的优惠券
func (h *CouponHandler) MyCoupons(c *gin.Context) {
	var list []model.UserCoupon
	model.DB.Preload("Coupon", func(db *gorm.DB) *gorm.DB { return db.Select("id,name,type,value,min_consume,valid_to,status") }).
		Where("user_id = ?", middleware.GetUserID(c)).Order("id DESC").Find(&list)
	resp.OK(c, list)
}

// FavoriteCoupon 收藏优惠券（工单07：收藏功能）
func (h *CouponHandler) FavoriteCoupon(c *gin.Context) {
	id, err := parseUint(c.Param("id"))
	if err != nil {
		resp.BadRequest(c, "id 非法")
		return
	}
	var cnt int64
	model.DB.Model(&model.CouponFavorite{}).Where("user_id = ? AND coupon_id = ?", middleware.GetUserID(c), id).Count(&cnt)
	if cnt > 0 {
		resp.OK(c, gin.H{"message": "已收藏"})
		return
	}
	model.DB.Create(&model.CouponFavorite{UserID: middleware.GetUserID(c), CouponID: id})
	resp.OK(c, gin.H{"message": "收藏成功"})
}

// ListMyFavorites 我的收藏
func (h *CouponHandler) ListMyFavorites(c *gin.Context) {
	var list []model.CouponFavorite
	model.DB.Preload("Coupon").
		Where("user_id = ?", middleware.GetUserID(c)).Find(&list)
	resp.OK(c, list)
}

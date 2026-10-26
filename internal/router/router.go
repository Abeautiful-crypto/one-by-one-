// 工单：全栈开发-网约车（01~07 全模块）
// 路由注册：RESTful API 设计（工单07），RBAC 权限分级（工单05）。
package router

import (
	"ride-hailing/internal/config"
	"ride-hailing/internal/handler"
	"ride-hailing/internal/middleware"

	"github.com/gin-gonic/gin"
)

func Setup(cfg *config.Config) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), middleware.CORS(), middleware.OperationLog())
	r.Use(gin.CustomRecovery(func(c *gin.Context, err interface{}) {
		c.AbortWithStatus(500)
	}))

	authH := &handler.AuthHandler{Cfg: cfg}
	personH := &handler.PersonnelHandler{}
	driverH := &handler.DriverHandler{}
	routeH := &handler.RouteHandler{}
	tripH := &handler.TripHandler{}
	orderH := &handler.OrderHandler{}
	refundH := &handler.RefundHandler{}
	couponH := &handler.CouponHandler{}
	finH := &handler.FinanceHandler{}
	anaH := &handler.AnalyticsHandler{}
	expH := &handler.ExportHandler{}

	api := r.Group("/api/v1")

	// ---------- 公开接口 ----------
	api.POST("/auth/login", authH.Login)
	api.POST("/auth/register", authH.Register) // 实名制注册（工单01）
	api.GET("/share/trip/:token", tripH.GetSharedTrip)
	api.POST("/analytics/track", anaH.TrackVisit) // 行为埋点

	// ---------- 认证接口 ----------
	auth := api.Group("", middleware.JWTAuth(cfg))
	{
		auth.GET("/auth/me", authH.Me)

		// 工单05：人员管理（管理端：admin/调度员/验票员/联系人员）
		staff := auth.Group("", middleware.RequireStaff())
		{
			staff.GET("/users", personH.ListUsers)
			staff.POST("/users", personH.CreateUser)          // 设置员工
			staff.PUT("/users/:id", personH.UpdateUser)
			staff.POST("/users/batch", personH.BatchUsers)    // 批量启用/禁用/删除
			staff.POST("/users/import", personH.ImportUsers)  // CSV/JSON 导入校验
			staff.GET("/roles", personH.ListRoles)
			staff.GET("/drivers", personH.ListDrivers)        // 司机列表
			staff.GET("/sos", tripH.ListSOS)
			staff.POST("/sos/:id/handle", tripH.HandleSOS)

			// 工单04：订单管理（管理端视角）
			staff.GET("/orders", orderH.ListOrders)
			staff.GET("/orders/:id", orderH.GetOrder)
			staff.GET("/reviews", orderH.ListReviews) // 评价管理

			// 工单02：班车管理（调度员独立管理界面使用这组接口）
			staff.POST("/routes", routeH.CreateRoute)
			staff.GET("/routes", routeH.ListRoutes)
			staff.GET("/routes/:id", routeH.GetRoute)
			staff.PUT("/routes/:id/stations", routeH.SaveRouteStations) // 站点排重+排序
			staff.POST("/shifts", routeH.CreateShift)                   // 排班
			staff.GET("/shifts", routeH.ListShifts)

			// 工单03：财务管理
			staff.GET("/finance/transactions", finH.ListTransactions)
			staff.GET("/finance/reconcile", finH.Reconcile)
			staff.POST("/finance/bills", finH.CreateBill)
			staff.GET("/finance/bills", finH.ListBills)
			staff.GET("/finance/anomalies", finH.ListAnomalies)
			staff.POST("/finance/anomalies/:id/resolve", finH.ResolveAnomaly)
			staff.GET("/finance/disputes", nilHandle)
			staff.POST("/finance/disputes/:id/handle", finH.HandleDispute)

			// 工单06：数据分析
			staff.GET("/analytics/orders", anaH.OrderAnalysis)
			staff.GET("/analytics/conversion", anaH.ConversionAnalysis)
			staff.GET("/analytics/repurchase", anaH.RepurchaseAnalysis)
		}

		// 仅 admin
		admin := auth.Group("", middleware.RequireAdmin())
		{
			admin.POST("/coupons", couponH.CreateCoupon) // 创建优惠券
			admin.PUT("/coupons/:id", couponH.UpdateCoupon)
			admin.DELETE("/coupons/:id", couponH.DeleteCoupon)
			admin.POST("/coupons/grant", couponH.GrantCoupon)         // 手动发放
			admin.POST("/coupons/auto-grant", couponH.AutoGrant)      // 自动发放
			admin.POST("/coupons/promo-codes", couponH.CreatePromoCode) // 推广发码
			admin.POST("/driver-docs/:id/audit", driverH.AuditDoc)    // 证件审核
		}

		// 工单07：优惠券（登录用户可浏览/领取/收藏）
		auth.GET("/coupons", couponH.ListCoupons)
		auth.POST("/coupons/promo-codes/redeem", couponH.RedeemPromoCode) // 领优惠码+推荐奖励
		auth.GET("/coupons/mine", couponH.MyCoupons)
		auth.POST("/coupons/:id/favorite", couponH.FavoriteCoupon)
		auth.GET("/coupons/favorites", couponH.ListMyFavorites)

		// 工单01：顺风车
		auth.POST("/trips", tripH.PublishTrip)          // 行程发布
		auth.GET("/trips", tripH.ListTrips)
		auth.GET("/trips/:id/match", tripH.MatchTrips)  // 自动推荐
		auth.POST("/trips/:id/share", tripH.ShareTrip)  // 行程分享
		auth.POST("/sos", tripH.CreateSOS)              // 紧急求助

		// 工单01：司机配置
		auth.POST("/driver/profile", driverH.SaveDriverProfile)
		auth.POST("/driver/vehicles", driverH.CreateVehicle)
		auth.GET("/driver/vehicles", driverH.ListMyVehicles)
		auth.GET("/driver/reminders", driverH.DueReminders) // 年检/保险提醒
		auth.POST("/driver/docs", driverH.UploadDoc)        // 证件上传

		// 工单04：订单（用户视角）
		auth.POST("/orders", orderH.CreateOrder)
		auth.POST("/orders/:id/pay", orderH.PayOrder)
		auth.POST("/orders/:id/cancel", orderH.CancelOrder)
		auth.POST("/reviews", orderH.CreateReview) // 双向评价

		// 工单04：退票
		auth.POST("/refunds", refundH.ApplyRefund)
		auth.GET("/refunds", refundH.ListRefunds)
		auth.POST("/refunds/:id/audit", refundH.AuditRefund) // 人工复核
		auth.POST("/refunds/batch", refundH.BatchRefund)     // 批量退票

		// 工单03：争议（用户提交）
		auth.POST("/finance/disputes", finH.CreateDispute)

		// 异步导出（工单01 性能优化）
		auth.POST("/export/tasks", expH.CreateTask)
		auth.GET("/export/tasks", expH.ListTasks)
		auth.GET("/export/tasks/:taskNo", expH.GetTask)
		auth.GET("/export/download/:taskNo", expH.Download)
	}

	return r
}

// nilHandle 占位（争议列表走用户侧/管理侧合并查询，暂不单列）
func nilHandle(c *gin.Context) {
	c.JSON(200, gin.H{"code": 0, "msg": "success", "data": []interface{}{}})
}

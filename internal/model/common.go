// 工单：全栈开发-网约车-公共
// 数据库初始化、基础模型、种子数据（RBAC 角色 + 管理员账号）。
package model

import (
	"log"
	"os"
	"time"

	"ride-hailing/internal/config"

	"gorm.io/driver/mysql"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

// BaseModel 公共字段
type BaseModel struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// InitDB 根据配置初始化数据库连接并自动迁移
func InitDB(cfg *config.Config) {
	var err error
	gormCfg := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	}
	switch cfg.DBDriver {
	case "mysql":
		DB, err = gorm.Open(mysql.Open(cfg.DBDSN), gormCfg)
	default:
		DB, err = gorm.Open(sqlite.Open("ride_hailing.db"), gormCfg)
	}
	if err != nil {
		log.Fatalf("数据库连接失败: %v", err)
	}
	if err := DB.AutoMigrate(allModels()...); err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}
	seed()
}

func allModels() []interface{} {
	return []interface{}{
		&User{}, &Role{}, &UserRole{},
		&DriverProfile{}, &Vehicle{}, &DriverDoc{},
		&Route{}, &Station{}, &RouteStation{}, &Shift{},
		&Trip{}, &SOSRecord{}, &TripShare{},
		&Order{}, &OrderStatusLog{}, &Review{},
		&Refund{},
		&Coupon{}, &UserCoupon{}, &CouponCode{}, &CouponFavorite{},
		&Transaction{}, &Bill{}, &Anomaly{}, &Dispute{},
		&UserVisit{},
		&ExportTask{}, &OperationLog{},
	}
}

// seed 首次启动写入 RBAC 角色与默认管理员
func seed() {
	var cnt int64
	DB.Model(&User{}).Count(&cnt)
	if cnt > 0 {
		return
	}
	roles := []Role{
		{Code: "admin", Name: "超级管理员"},
		{Code: "dispatcher", Name: "调度员"}, // 工单05：调度员有独立管理界面、独立账号登录
		{Code: "ticket_checker", Name: "验票员"},
		{Code: "contact", Name: "联系人员"},
		{Code: "bus_driver", Name: "班车司机"},
		{Code: "shuttle_driver", Name: "接送司机"},
		{Code: "carpool_driver", Name: "顺风车司机"},
		{Code: "passenger", Name: "乘客"},
	}
	if err := DB.Create(&roles).Error; err != nil {
		log.Printf("初始化角色失败: %v", err)
		os.Exit(1)
	}
	admin := User{
		Username: "admin", Password: mustHash("admin123"),
		RealName: "系统管理员", Phone: "13800000000", Status: "enabled",
	}
	DB.Create(&admin)
	DB.Create(&UserRole{UserID: admin.ID, RoleID: roles[0].ID})
	log.Println("已初始化默认管理员：admin / admin123")
}

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
		// SQLite：通过 DSN 的 _pragma 让每个连接自动应用 PRAGMA，避免 SQLITE_BUSY 写锁冲突（并发可靠性）
		dsn := "file:ride_hailing.db?_pragma=busy_timeout(8000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
		DB, err = gorm.Open(sqlite.Open(dsn), gormCfg)
	}
	if err != nil {
		log.Fatalf("数据库连接失败: %v", err)
	}
	// 连接池：SQLite 为单机文件库，写操作全局互斥；设为单连接可彻底避免 SQLITE_BUSY。
	// 生产环境建议 DB_DRIVER=mysql 以获得更好并发能力。
	sqlDB, _ := DB.DB()
	if cfg.DBDriver == "mysql" {
		sqlDB.SetMaxOpenConns(50)
		sqlDB.SetMaxIdleConns(10)
	} else {
		sqlDB.SetMaxOpenConns(1)
		sqlDB.SetMaxIdleConns(1)
	}
	sqlDB.SetConnMaxLifetime(time.Hour)
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
		// 网约车工单（10/11/15/16/17）
		&RideOrder{}, &TripBill{}, &OrderAudit{}, &DispatchConfig{}, &DispatchLog{}, &DispatchConfigHistory{},
		&FloodReport{}, &ShelterPoint{}, &WeatherAlert{}, &IdemKey{}, &TripTrace{},
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

// 工单：全栈开发-网约车-05-人员管理
// 用户与 RBAC 模型：员工、司机、乘客、调度员等统一为 User，通过 UserRole 关联多个角色。
package model

import (
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// User 平台统一用户（员工/司机/乘客），工单05：每个用户可拥有多个角色
type User struct {
	BaseModel
	Username  string     `gorm:"size:64;uniqueIndex;not null" json:"username"` // 登录名
	Password  string     `gorm:"size:128;not null" json:"-"`                   // bcrypt 加密存储
	RealName  string     `gorm:"size:64" json:"realName"`                      // 姓名
	Nickname  string     `gorm:"size:64" json:"nickname"`                      // 昵称（工单12 账户信息）
	Avatar    string     `gorm:"size:255" json:"avatar"`                       // 头像 URL（工单12）
	Phone     string     `gorm:"size:20;index" json:"phone"`                   // 手机号（唯一性校验在服务层）
	Email     string     `gorm:"size:128" json:"email"`                        // 电子邮件
	IdCard    string     `gorm:"size:32;index" json:"idCard"`                  // 身份证号（实名制，工单01）
	Status    string     `gorm:"size:20;index;default:enabled" json:"status"`  // enabled/disabled
	RegSource string     `gorm:"size:32;default:web" json:"regSource"`         // 注册来源（数据分析用）
	Balance   float64    `gorm:"default:0" json:"balance"`                     // 账户余额（工单14 打赏即时到账）
	Points    int        `gorm:"default:0" json:"points"`                      // 积分（工单12 优惠与积分）
	LastLogin *time.Time `json:"lastLogin"`
	Roles     []Role     `gorm:"many2many:user_roles;" json:"roles,omitempty"`
}

// Role 角色（工单05：班车司机、接送司机、顺风车司机、调度员、验票员、联系人员等）
type Role struct {
	BaseModel
	Code string `gorm:"size:32;uniqueIndex;not null" json:"code"` // 角色编码
	Name string `gorm:"size:64" json:"name"`
	Desc string `gorm:"size:255" json:"desc"`
}

// UserRole 用户-角色关联
type UserRole struct {
	UserID uint `gorm:"primaryKey"`
	RoleID uint `gorm:"primaryKey"`
}

func (User) TableName() string  { return "users" }
func (Role) TableName() string  { return "roles" }
func (UserRole) TableName() string { return "user_roles" }

// SetPassword bcrypt 加密
func (u *User) SetPassword(plain string) error {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.Password = string(b)
	return nil
}

func mustHash(plain string) string {
	b, _ := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	return string(b)
}

// CheckPassword 校验密码
func (u *User) CheckPassword(plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(plain)) == nil
}

// HasRole 判断用户是否拥有某角色编码
func (u *User) HasRole(db *gorm.DB, code string) bool {
	var cnt int64
	db.Table("user_roles ur").
		Joins("JOIN roles r ON r.id = ur.role_id").
		Where("ur.user_id = ? AND r.code = ?", u.ID, code).
		Count(&cnt)
	return cnt > 0
}

// IsDispatcher 工单05：调度员独立账号登录、独立管理界面
func (u *User) IsDispatcher(db *gorm.DB) bool {
	return u.HasRole(db, "dispatcher")
}

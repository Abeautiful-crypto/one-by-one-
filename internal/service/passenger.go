// 工单12/13/14：乘客端业务逻辑（验证码登录、找回密码、附近司机、打赏、评价审核、取消限制等）
package service

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"ride-hailing/internal/config"
	"ride-hailing/internal/model"
	"ride-hailing/internal/pkg/bus"
	"ride-hailing/internal/pkg/geo"
	"ride-hailing/internal/pkg/jwtutil"

	"gorm.io/gorm"
)

// ---------------- 验证码（工单12：手机验证码登录/找回密码，模拟短信发送） ----------------
var (
	codeMu   sync.Mutex
	codeStor = map[string]string{} // key: phone:purpose -> code
)

// SendSmsCode 生成并"发送"验证码（演示：不接真实 SMS，直接返回/存储）。返回验证码便于联调。
func SendSmsCode(phone, purpose string) (string, error) {
	if len(phone) != 11 {
		return "", errors.New("手机号格式不正确")
	}
	code := fmt.Sprintf("%06d", rand.Intn(1000000))
	codeMu.Lock()
	codeStor[phone+":"+purpose] = code
	codeMu.Unlock()
	// TODO: 生产接入短信网关；此处仅模拟
	return code, nil
}

// VerifySmsCode 校验验证码
func VerifySmsCode(phone, purpose, code string) bool {
	codeMu.Lock()
	defer codeMu.Unlock()
	return codeStor[phone+":"+purpose] == code
}

// LoginBySms 手机验证码登录（工单12：即时手机验证）。手机号须已注册。
func LoginBySms(phone, code string) (string, error) {
	if !VerifySmsCode(phone, "login", code) {
		return "", errors.New("验证码错误或已过期")
	}
	var u model.User
	if err := model.DB.Where("phone = ?", phone).First(&u).Error; err != nil {
		return "", errors.New("该手机号未注册")
	}
	return issueToken(&u)
}

// ResetPasswordByCode 验证码找回/重置密码（工单12）
func ResetPasswordByCode(phone, code, newPwd string) error {
	if len(newPwd) < 6 {
		return errors.New("新密码至少6位")
	}
	if !VerifySmsCode(phone, "reset", code) {
		return errors.New("验证码错误或已过期")
	}
	var u model.User
	if err := model.DB.Where("phone = ?", phone).First(&u).Error; err != nil {
		return errors.New("该手机号未注册")
	}
	if err := u.SetPassword(newPwd); err != nil {
		return err
	}
	return model.DB.Model(&u).Update("password", u.Password).Error
}

func issueToken(u *model.User) (string, error) {
	var roles []model.Role
	model.DB.Model(u).Association("Roles").Find(&roles)
	codes := make([]string, 0, len(roles))
	for _, r := range roles {
		codes = append(codes, r.Code)
	}
	return jwtutil.GenerateToken(u.ID, u.Username, codes, config.Load().JWTSecret)
}

// ---------------- 附近司机（工单13：实时查看附近可用车辆，含评分/车牌） ----------------
type NearbyDriver struct {
	DriverID uint    `json:"driverId"`
	DistanceKm float64 `json:"distanceKm"`
	Rating    float64 `json:"rating"`
	PlateNo   string  `json:"plateNo"`
	Name      string  `json:"name"`
	Status    string  `json:"status"`
}

// ListNearbyDrivers 检索附近在线司机（GEO），用于乘客端地图展示
func ListNearbyDrivers(city string, lng, lat, radiusKm float64, limit int) ([]NearbyDriver, error) {
	if limit <= 0 {
		limit = 20
	}
	ns, err := geo.I().Nearby(context.Background(), geo.Bucket("drivers", city), lng, lat, radiusKm, limit)
	if err != nil {
		return nil, err
	}
	out := make([]NearbyDriver, 0, len(ns))
	for _, n := range ns {
		d := NearbyDriver{DriverID: n.ID, DistanceKm: n.Distance}
		var dp model.DriverProfile
		if e := model.DB.Where("user_id = ?", n.ID).First(&dp).Error; e == nil {
			d.Rating = dp.ServiceScore
		}
		var v model.Vehicle
		if e := model.DB.Where("driver_id = ?", n.ID).First(&v).Error; e == nil {
			d.PlateNo = v.PlateNo
		}
		var u model.User
		if e := model.DB.Where("id = ?", n.ID).First(&u).Error; e == nil {
			d.Name = u.RealName
		}
		d.Status = DriverStatus(n.ID)
		out = append(out, d)
	}
	return out, nil
}

// ---------------- 打赏司机（工单14：自定义金额、即时到账、感谢语、记录） ----------------
func CreateTip(orderNo string, passengerID, driverID uint, amount float64, thanks string) error {
	if amount <= 0 {
		return errors.New("打赏金额必须大于0")
	}
	tx := model.DB.Begin()
	// 打赏记录
	if err := tx.Create(&model.RideTip{
		OrderNo: orderNo, PassengerID: passengerID, DriverID: driverID,
		Amount: amount, Thanks: thanks,
	}).Error; err != nil {
		tx.Rollback()
		return err
	}
	// 即时到账司机账户余额
	if err := tx.Model(&model.User{}).Where("id = ?", driverID).
		UpdateColumn("balance", gorm.Expr("balance + ?", amount)).Error; err != nil {
		tx.Rollback()
		return err
	}
	// 乘客扣减余额（无余额则记负，演示允许）
	tx.Model(&model.User{}).Where("id = ?", passengerID).
		UpdateColumn("balance", gorm.Expr("balance - ?", amount))
	tx.Commit()
	notifyUser("driver-"+fmt.Sprint(driverID), "tip", map[string]interface{}{
		"orderNo": orderNo, "amount": amount, "thanks": thanks,
	})
	return nil
}

// ---------------- 行程评价（工单14：5星+评论+匿名+审核） ----------------
func CreateReview(r *model.RideReview) error {
	if r.Rating < 1 || r.Rating > 5 {
		return errors.New("评分必须为1~5星")
	}
	r.Status = "pending" // 默认待审核
	if err := model.DB.Create(r).Error; err != nil {
		return err
	}
	// 同步评分到司机档案（审核通过后生效，此处仅记录）
	return nil
}

// ApproveReview 审核通过（工单14：敏感/负面评价审核后公开）
func ApproveReview(id uint) error {
	var r model.RideReview
	if err := model.DB.First(&r, id).Error; err != nil {
		return err
	}
	if r.Status != "pending" {
		return errors.New("该评价已处理")
	}
	tx := model.DB.Begin()
	if err := tx.Model(&r).Update("status", "approved").Error; err != nil {
		tx.Rollback()
		return err
	}
	// 更新司机平均评分
	var dp model.DriverProfile
	if err := tx.Where("user_id = ?", r.DriverID).First(&dp).Error; err == nil {
		newCnt := dp.ReviewCount + 1
		newRating := (dp.ServiceScore*float64(dp.ReviewCount) + float64(r.Rating)) / float64(newCnt)
		tx.Model(&dp).Updates(map[string]interface{}{"service_score": newRating, "review_count": newCnt})
	}
	tx.Commit()
	return nil
}

// RejectReview 审核拒绝（不公开）
func RejectReview(id uint) error {
	return model.DB.Model(&model.RideReview{}).Where("id = ?", id).Update("status", "rejected").Error
}

// ---------------- 取消限制（工单14：限制滥用，24小时内频繁取消限制下单） ----------------
// CheckCancelAllowed 检查用户是否因频繁取消被限制（演示阈值：24h 内 >=3 次）
func CheckCancelAllowed(userID uint) error {
	var cnt int64
	since := time.Now().Add(-24 * time.Hour)
	model.DB.Model(&model.CancelRecord{}).Where("user_id = ? AND created_at >= ?", userID, since).Count(&cnt)
	if cnt >= 3 {
		return fmt.Errorf("您24小时内已取消%d次，暂不能发起新订单（限制滥用）", cnt)
	}
	return nil
}

// RecordCancel 记录取消（用于限制滥用统计）+ 计算补偿
func RecordCancel(userID uint, orderNo, reason string, compensation float64) {
	model.DB.Create(&model.CancelRecord{
		UserID: userID, OrderNo: orderNo, Reason: reason, Compensation: compensation,
	})
}

// ---------------- 紧急求助（工单13：一键 SOS，立即通知） ----------------
func CreateSOS(userID uint, orderNo string, lng, lat float64, address string) (*model.SOSAlert, error) {
	s := &model.SOSAlert{UserID: userID, OrderNo: orderNo, Lng: lng, Lat: lat, Address: address, Status: "pending"}
	if err := model.DB.Create(s).Error; err != nil {
		return nil, err
	}
	publishJSON(bus.EvSOS, map[string]interface{}{
		"alertId": s.ID, "userId": userID, "orderNo": orderNo, "lng": lng, "lat": lat, "address": address,
	})
	// 通知在线司机/管理员（演示：广播到 admin 与行程司机）
	notifyUser("admin", "sos", map[string]interface{}{"alertId": s.ID, "address": address})
	return s, nil
}

// ---------------- 发票申请（工单14） ----------------
func CreateInvoice(userID uint, orderNo, invType, title, taxNo, email string) (*model.Invoice, error) {
	if invType == "" {
		invType = "electronic"
	}
	inv := &model.Invoice{
		UserID: userID, OrderNo: orderNo, Type: invType,
		Title: title, TaxNo: taxNo, Email: email, Status: "pending",
	}
	if err := model.DB.Create(inv).Error; err != nil {
		return nil, err
	}
	notifyUser("admin", "invoice", map[string]interface{}{"invoiceId": inv.ID, "orderNo": orderNo})
	return inv, nil
}

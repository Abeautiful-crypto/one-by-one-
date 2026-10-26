// 工单：全栈开发-网约车-01-顺风车管理（顺风车司机配置）& 05-人员管理
// 司机配置接口：司机资料、车辆信息（品牌/型号/颜色/车牌/外观照片备案）、
// 证件上传验证（身份证/驾驶证/行驶证）、年检与保险到期提醒。
package handler

import (
	"errors"
	"time"

	"ride-hailing/internal/middleware"
	"ride-hailing/internal/model"
	"ride-hailing/internal/pkg/resp"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type DriverHandler struct{}

// ---------- 司机资料 ----------

type upsertDriverReq struct {
	LicenseNo     string     `json:"licenseNo" binding:"required"` // 驾驶证号
	LicenseExpiry *time.Time `json:"licenseExpiry"`
	OnlineStatus  string     `json:"onlineStatus" binding:"omitempty,oneof=online offline"`
}

// SaveDriverProfile 保存/更新司机资料（实名制司机注册）
func (h *DriverHandler) SaveDriverProfile(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if uid := c.Param("userId"); uid != "" && uid != "me" {
		if _, err := parseUint(uid); err != nil {
			resp.BadRequest(c, "userId 非法")
			return
		}
	}
	var req upsertDriverReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	var profile model.DriverProfile
	err := model.DB.Where("user_id = ?", userID).First(&profile).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		profile = model.DriverProfile{UserID: userID, LicenseNo: req.LicenseNo, LicenseExpiry: req.LicenseExpiry}
		if err := model.DB.Create(&profile).Error; err != nil {
			resp.ServerError(c, err.Error())
			return
		}
	} else if err != nil {
		resp.ServerError(c, err.Error())
		return
	} else {
		profile.LicenseNo = req.LicenseNo
		profile.LicenseExpiry = req.LicenseExpiry
		if req.OnlineStatus != "" {
			profile.OnlineStatus = req.OnlineStatus // 工单05：在线/离线状态
		}
		if err := model.DB.Save(&profile).Error; err != nil {
			resp.ServerError(c, err.Error())
			return
		}
	}
	resp.OK(c, profile)
}

// ---------- 车辆信息 ----------

type upsertVehicleReq struct {
	Brand         string     `json:"brand" binding:"required"`
	Model         string     `json:"model" binding:"required"`
	Color         string     `json:"color" binding:"required"`
	PlateNo       string     `json:"plateNo" binding:"required,min=7,max=8"` // 车牌号
	PhotoURL      string     `json:"photoURL"`                               // 车辆外观照片备案
	VehicleType   string     `json:"vehicleType" binding:"omitempty,oneof=economy luxury"`
	InspectionDue *time.Time `json:"inspectionDue"` // 年检到期提醒
	InsuranceDue  *time.Time `json:"insuranceDue"`  // 保险到期提醒
}

// CreateVehicle 录入车辆（工单01：填写车辆详细信息并上传外观照片备案）
func (h *DriverHandler) CreateVehicle(c *gin.Context) {
	var req upsertVehicleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	v := model.Vehicle{
		DriverID: middleware.GetUserID(c), Brand: req.Brand, Model: req.Model,
		Color: req.Color, PlateNo: req.PlateNo, PhotoURL: req.PhotoURL,
		VehicleType: req.VehicleType, InspectionDue: req.InspectionDue, InsuranceDue: req.InsuranceDue,
	}
	if v.VehicleType == "" {
		v.VehicleType = "economy"
	}
	var cnt int64
	model.DB.Model(&model.Vehicle{}).Where("plate_no = ?", v.PlateNo).Count(&cnt)
	if cnt > 0 {
		resp.BadRequest(c, "车牌号已存在")
		return
	}
	if err := model.DB.Create(&v).Error; err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, v)
}

// ListMyVehicles 我的车辆列表
func (h *DriverHandler) ListMyVehicles(c *gin.Context) {
	var vehicles []model.Vehicle
	model.DB.Where("driver_id = ?", middleware.GetUserID(c)).Find(&vehicles)
	resp.OK(c, vehicles)
}

// DueReminders 车辆年检/保险到期提醒（工单01：设置重要日期提醒，确保合法合规）
func (h *DriverHandler) DueReminders(c *gin.Context) {
	var vehicles []model.Vehicle
	model.DB.Where("driver_id = ?", middleware.GetUserID(c)).Find(&vehicles)
	now := time.Now()
	reminders := []model.DueReminder{}
	for _, v := range vehicles {
		if v.InspectionDue != nil && v.InspectionDue.Before(now.AddDate(0, 0, 30)) {
			reminders = append(reminders, model.DueReminder{VehicleID: v.ID, PlateNo: v.PlateNo, Type: "inspection", DueDate: *v.InspectionDue, DaysLeft: int(v.InspectionDue.Sub(now).Hours() / 24)})
		}
		if v.InsuranceDue != nil && v.InsuranceDue.Before(now.AddDate(0, 0, 30)) {
			reminders = append(reminders, model.DueReminder{VehicleID: v.ID, PlateNo: v.PlateNo, Type: "insurance", DueDate: *v.InsuranceDue, DaysLeft: int(v.InsuranceDue.Sub(now).Hours() / 24)})
		}
	}
	resp.OK(c, reminders)
}

// ---------- 证件上传验证 ----------

type uploadDocReq struct {
	DocType  string `json:"docType" binding:"required,oneof=id_card license vehicle_license"` // 身份证/驾驶证/行驶证
	FileURL  string `json:"fileURL" binding:"required,url"`                                    // HTTPS 文件地址
}

// UploadDoc 上传证件（工单01：上传并验证驾驶证、行驶证等必要证件）
func (h *DriverHandler) UploadDoc(c *gin.Context) {
	var req uploadDocReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	doc := model.DriverDoc{UserID: middleware.GetUserID(c), DocType: req.DocType, FileURL: req.FileURL}
	if err := model.DB.Create(&doc).Error; err != nil {
		resp.ServerError(c, err.Error())
		return
	}
	resp.OK(c, doc)
}

// AuditDoc 证件审核（管理端：身份验证）
func (h *DriverHandler) AuditDoc(c *gin.Context) {
	var req struct {
		Result string `json:"result" binding:"required,oneof=approved rejected"`
		Remark string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.BadRequest(c, "参数校验失败(Validator): "+err.Error())
		return
	}
	var doc model.DriverDoc
	if err := model.DB.First(&doc, c.Param("id")).Error; err != nil {
		resp.NotFound(c, "证件记录不存在")
		return
	}
	now := time.Now()
	doc.VerifyStatus = req.Result
	doc.Remark = req.Remark
	doc.VerifiedAt = &now
	model.DB.Save(&doc)
	// 证件通过 => 司机实名审核通过
	if req.Result == "approved" {
		model.DB.Model(&model.DriverProfile{}).Where("user_id = ?", doc.UserID).Update("verify_status", "approved")
	}
	resp.OK(c, doc)
}

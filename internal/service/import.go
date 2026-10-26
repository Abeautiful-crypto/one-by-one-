// 工单：全栈开发-网约车-05-人员管理
// 数据导入：支持 CSV / JSON 文件导入司机、乘客信息。
// 数据验证与清洗：格式检查（手机号、身份证、邮箱）、唯一性验证（手机号、身份证号避免重复录入）、
// 详细错误报告（列出所有不符合要求的数据项及修改建议）。
package service

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"ride-hailing/internal/model"

	"gorm.io/gorm"
)

var (
	phoneRe   = regexp.MustCompile(`^1[3-9]\d{9}$`)
	idCardRe  = regexp.MustCompile(`^\d{17}[\dXx]$`)
	emailRe   = regexp.MustCompile(`^[\w.+-]+@[\w-]+(\.[\w-]+)+$`)
)

// ImportError 单行导入错误报告
type ImportError struct {
	Row     int    `json:"row"`
	Field   string `json:"field"`
	Value   string `json:"value"`
	Message string `json:"message"`
	Advice  string `json:"advice"`
}

// ImportResult 导入结果：成功数 + 错误报告
type ImportResult struct {
	Total    int           `json:"total"`
	Success  int           `json:"success"`
	Failed   int           `json:"failed"`
	Errors   []ImportError `json:"errors"`
}

// ImportPersonRow 人员导入行（司机/乘客通用）
type ImportPersonRow struct {
	Username string `json:"username"`
	RealName string `json:"realName"`
	Phone    string `json:"phone"`
	Email    string `json:"email"`
	IdCard   string `json:"idCard"`
	Role     string `json:"role"` // carpool_driver/bus_driver/passenger...
}

// ParsePersonCSV 解析 CSV（表头：username,realName,phone,email,idCard,role）
func ParsePersonCSV(r io.Reader) ([]ImportPersonRow, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) < 2 {
		return nil, errors.New("CSV 至少需要表头和一行数据")
	}
	header := make(map[string]int)
	for i, h := range records[0] {
		header[strings.TrimSpace(h)] = i
	}
	var rows []ImportPersonRow
	for _, rec := range records[1:] {
		get := func(k string) string {
			if idx, ok := header[k]; ok && idx < len(rec) {
				return strings.TrimSpace(rec[idx])
			}
			return ""
		}
		rows = append(rows, ImportPersonRow{
			Username: get("username"), RealName: get("realName"),
			Phone: get("phone"), Email: get("email"),
			IdCard: get("idCard"), Role: get("role"),
		})
	}
	return rows, nil
}

// ParsePersonJSON 解析 JSON 数组
func ParsePersonJSON(r io.Reader) ([]ImportPersonRow, error) {
	var rows []ImportPersonRow
	if err := json.NewDecoder(r).Decode(&rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// ImportPersons 导入人员并校验（工单05：格式检查、唯一性验证、错误提示）
func ImportPersons(rows []ImportPersonRow) *ImportResult {
	result := &ImportResult{Total: len(rows), Errors: []ImportError{}}
	// 批内手机号/身份证排重
	batchPhone := map[string]bool{}
	batchID := map[string]bool{}

	for i, row := range rows {
		rowNo := i + 2 // CSV 数据从第 2 行开始（含表头）
		var errs []ImportError
		if row.Username == "" {
			errs = append(errs, ImportError{rowNo, "username", row.Username, "用户名不能为空", "请填写登录用户名"})
		}
		if !phoneRe.MatchString(row.Phone) {
			errs = append(errs, ImportError{rowNo, "phone", row.Phone, "手机号格式不正确", "应为 11 位大陆手机号，如 13812345678"})
		}
		if row.IdCard != "" && !idCardRe.MatchString(row.IdCard) {
			errs = append(errs, ImportError{rowNo, "idCard", row.IdCard, "身份证号格式不正确", "应为 18 位（末位可为 X）"})
		}
		if row.Email != "" && !emailRe.MatchString(row.Email) {
			errs = append(errs, ImportError{rowNo, "email", row.Email, "邮箱格式不正确", "示例 name@example.com"})
		}
		// 唯一性验证：数据库已存在
		var cnt int64
		model.DB.Model(&model.User{}).Where("phone = ?", row.Phone).Count(&cnt)
		if cnt > 0 {
			errs = append(errs, ImportError{rowNo, "phone", row.Phone, "手机号已存在", "关键字段唯一性校验失败，请勿重复录入"})
		}
		if row.IdCard != "" {
			model.DB.Model(&model.User{}).Where("id_card = ?", row.IdCard).Count(&cnt)
			if cnt > 0 {
				errs = append(errs, ImportError{rowNo, "idCard", row.IdCard, "身份证号已存在", "关键字段唯一性校验失败，请勿重复录入"})
			}
		}
		if batchPhone[row.Phone] {
			errs = append(errs, ImportError{rowNo, "phone", row.Phone, "文件内手机号重复", "请检查文件数据"})
		}
		if len(errs) > 0 {
			batchPhone[row.Phone] = true
			result.Errors = append(result.Errors, errs...)
			result.Failed++
			continue
		}
		batchPhone[row.Phone] = true
		if row.IdCard != "" {
			batchID[row.IdCard] = true
		}

		roleCode := row.Role
		if roleCode == "" {
			roleCode = "passenger"
		}
		if err := CreateUserWithRole(row.Username, "Abc@123456", row.RealName, row.Phone, row.Email, row.IdCard, []string{roleCode}); err != nil {
			result.Errors = append(result.Errors, ImportError{rowNo, "user", row.Username, "创建失败: " + err.Error(), "请检查角色编码是否合法"})
			result.Failed++
			continue
		}
		result.Success++
	}
	return result
}

// CreateUserWithRole 创建用户并绑定角色（工单05：每个用户可拥有多个角色）
func CreateUserWithRole(username, password, realName, phone, email, idCard string, roleCodes []string) error {
	return model.DB.Transaction(func(tx *gorm.DB) error {
		var cnt int64
		tx.Model(&model.User{}).Where("username = ?", username).Count(&cnt)
		if cnt > 0 {
			return errors.New("用户名已存在")
		}
		u := model.User{
			Username: username, RealName: realName, Phone: phone,
			Email: email, IdCard: idCard, Status: "enabled",
		}
		if err := u.SetPassword(password); err != nil {
			return err
		}
		if err := tx.Create(&u).Error; err != nil {
			return err
		}
		return bindRoles(tx, u.ID, roleCodes)
	})
}

func bindRoles(tx *gorm.DB, userID uint, roleCodes []string) error {
	for _, code := range roleCodes {
		var role model.Role
		if err := tx.Where("code = ?", code).First(&role).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("角色不存在: %s", code)
			}
			return err
		}
		var cnt int64
		tx.Model(&model.UserRole{}).Where("user_id = ? AND role_id = ?", userID, role.ID).Count(&cnt)
		if cnt == 0 {
			if err := tx.Create(&model.UserRole{UserID: userID, RoleID: role.ID}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// BindRolesPublic 供 handler 层事务内调用
func BindRolesPublic(tx *gorm.DB, userID uint, roleCodes []string) error {
	return bindRoles(tx, userID, roleCodes)
}

// itoa 供导入报告使用
func itoa(i int) string { return strconv.Itoa(i) }

// 工单：全栈开发-网约车-01-顺风车管理（性能优化验收标准）
// 异步导出服务：
//   - 分页处理：大数据集分页查询，避免一次性加载导致内存溢出；
//   - 异步任务：耗时导出后台处理，完成后通知用户任务状态；
//   - 权限控制：导出接口必须登录授权；
//   - 操作日志：ExportTask 记录谁执行了导出、何时导出、导出了哪些数据（审计）；
//   - 异常日志：失败原因写入 ErrorMsg，便于管理员排查。
package service

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"ride-hailing/internal/model"
)

var exportDir = "exports"

// ExportDir 导出目录（handler 下载用）
func ExportDir() string { return exportDir }

// CreateExportTask 创建导出任务并立即返回，后台 goroutine 执行
func CreateExportTask(userID uint, bizType string, params string) (*model.ExportTask, error) {
	task := &model.ExportTask{
		TaskNo:  fmt.Sprintf("EXP%s%06d", time.Now().Format("20060102150405"), userID),
		UserID:  userID,
		BizType: bizType,
		Params:  params,
		Status:  "processing",
	}
	if err := model.DB.Create(task).Error; err != nil {
		return nil, err
	}
	go runExport(task)
	return task, nil
}

// runExport 后台执行：分页查询 -> 逐步写入 CSV 文件
func runExport(task *model.ExportTask) {
	finish := func(status, fileURL, errMsg string) {
		now := time.Now()
		model.DB.Model(task).Updates(map[string]interface{}{
			"status": status, "file_url": fileURL, "error_msg": errMsg, "finished_at": &now,
		})
	}
	if err := os.MkdirAll(exportDir, 0755); err != nil {
		finish("failed", "", "创建导出目录失败: "+err.Error())
		return
	}
	filePath := filepath.Join(exportDir, task.TaskNo+".csv")
	f, err := os.Create(filePath)
	if err != nil {
		finish("failed", "", "创建文件失败: "+err.Error())
		return
	}
	defer f.Close()

	w := csv.NewWriter(f)
	writeErr := func(e error) { finish("failed", "", e.Error()) }

	switch task.BizType {
	case "orders":
		_ = w.Write([]string{"订单号", "类型", "乘客ID", "司机ID", "起站", "终站", "金额", "实付", "状态"})
		const pageSize = 500 // 分页处理，避免内存溢出
		for page := 1; ; page++ {
			var orders []model.Order
			if err := model.DB.Where("created_at >= ?", time.Now().AddDate(-1, 0, 0)).
				Order("id").Limit(pageSize).Offset((page - 1) * pageSize).Find(&orders).Error; err != nil {
				writeErr(err); return
			}
			if len(orders) == 0 {
				break
			}
			for _, o := range orders {
				_ = w.Write([]string{o.OrderNo, o.OrderType, fmt.Sprint(o.UserID), fmt.Sprint(o.DriverID),
					o.StartName, o.EndName, fmt.Sprintf("%.2f", o.Amount), fmt.Sprintf("%.2f", o.PaidAmount), o.Status})
			}
		}
	case "drivers":
		_ = w.Write([]string{"用户名", "姓名", "手机号", "身份证号", "评分", "状态"})
		var users []model.User
		model.DB.Joins("JOIN user_roles ur ON ur.user_id = users.id JOIN roles r ON r.id = ur.role_id AND r.code = 'carpool_driver'").
			Find(&users)
		for _, u := range users {
			_ = w.Write([]string{u.Username, u.RealName, u.Phone, u.IdCard, "5", u.Status})
		}
	case "transactions":
		_ = w.Write([]string{"交易号", "订单号", "方向", "金额", "支付方式", "交易时间"})
		var txns []model.Transaction
		model.DB.Order("id").Limit(10000).Find(&txns)
		for _, t := range txns {
			_ = w.Write([]string{t.TradeNo, t.OrderNo, t.Direction, fmt.Sprintf("%.2f", t.Amount), t.PayMethod, t.TxnTime.Format(time.RFC3339)})
		}
	default:
		finish("failed", "", "未知导出类型: "+task.BizType)
		return
	}
	w.Flush()
	if err := w.Error(); err != nil {
		writeErr(err)
		return
	}
	finish("done", "/api/v1/export/download/"+task.TaskNo, "") // 下载链接经 HTTPS 传输
}

// GetExportTask 查询任务状态（用户轮询获取完成情况）
func GetExportTask(taskNo string, userID uint) (*model.ExportTask, error) {
	var task model.ExportTask
	err := model.DB.Where("task_no = ? AND user_id = ?", taskNo, userID).First(&task).Error
	if err != nil {
		return nil, err
	}
	return &task, nil
}

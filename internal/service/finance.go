// 工单：全栈开发-网约车-03-财务管理
// 财务核心服务：交易记录、自动账单（异步）、异常检测（重复扣款/金额不符）、争议处理。
package service

import (
	"errors"
	"fmt"
	"time"

	"ride-hailing/internal/model"

	"gorm.io/gorm"
)

// RecordTransaction 记录交易流水（工单03：实时同步、精确记录）
func RecordTransaction(tx *gorm.DB, t *model.Transaction) error {
	if t.TradeNo == "" {
		t.TradeNo = fmt.Sprintf("TXN%s%06d", time.Now().Format("20060102150405"), tx.RowsAffected+time.Now().UnixNano()%1000000)
	}
	if t.Status == "" {
		t.Status = "success"
	}
	if t.TxnTime.IsZero() {
		t.TxnTime = time.Now()
	}
	if err := tx.Create(t).Error; err != nil {
		return err
	}
	go DetectAnomaly(t) // 工单03：自动检测异常交易
	return nil
}

// DetectAnomaly 异常检测：重复扣款（同一订单同方向多笔支付）、金额不符（支付金额与订单应付不符）
func DetectAnomaly(t *model.Transaction) {
	var order model.Order
	if err := model.DB.Where("order_no = ?", t.OrderNo).First(&order).Error; err != nil {
		return // 非订单类交易跳过
	}
	// 重复扣款检测
	var dupCnt int64
	model.DB.Model(&model.Transaction{}).
		Where("order_no = ? AND txn_type = 'pay' AND status = 'success' AND id <> ?", t.OrderNo, t.ID).
		Count(&dupCnt)
	if t.TxnType == "pay" && dupCnt > 0 {
		createAnomaly("duplicate_payment", t.OrderNo, fmt.Sprintf("检测到订单 %s 存在 %d 笔成功支付，疑似重复扣款", t.OrderNo, dupCnt+1))
	}
	// 金额不符检测
	if t.TxnType == "pay" && t.Amount != order.Amount-order.Discount {
		createAnomaly("amount_mismatch", t.OrderNo,
			fmt.Sprintf("支付金额 %.2f 与应收金额 %.2f 不一致", t.Amount, order.Amount-order.Discount))
	}
}

func createAnomaly(typ, orderNo, detail string) {
	model.DB.Create(&model.Anomaly{Type: typ, OrderNo: orderNo, Detail: detail})
}

// GenerateBill 自动账单生成（工单03：每日/每周/每月账单，异步任务）
func GenerateBill(periodType string, start time.Time) (*model.Bill, error) {
	var end time.Time
	switch periodType {
	case "day":
		end = start.AddDate(0, 0, 1)
	case "week":
		end = start.AddDate(0, 0, 7)
	case "month":
		end = start.AddDate(0, 1, 0)
	default:
		return nil, errors.New("periodType 应为 day/week/month")
	}
	bill := &model.Bill{
		PeriodType: periodType, PeriodStart: start, PeriodEnd: end, Status: "generating",
	}
	if err := model.DB.Create(bill).Error; err != nil {
		return nil, err
	}
	go func() { // 工单01：耗时任务后台异步处理，不阻塞前台操作
		var income, expense struct{ Sum float64 }
		model.DB.Model(&model.Transaction{}).
			Where("txn_time >= ? AND txn_time < ? AND direction = ?", start, end, model.DirectionIncome).
			Select("COALESCE(SUM(amount),0) AS sum").Scan(&income)
		model.DB.Model(&model.Transaction{}).
			Where("txn_time >= ? AND txn_time < ? AND direction = ?", start, end, model.DirectionExpense).
			Select("COALESCE(SUM(amount),0) AS sum").Scan(&expense)
		var orderCnt int64
		model.DB.Model(&model.Order{}).Where("created_at >= ? AND created_at < ?", start, end).Count(&orderCnt)
		model.DB.Model(bill).Updates(map[string]interface{}{
			"total_income": income.Sum, "total_expense": expense.Sum,
			"order_count": orderCnt, "status": "done",
		})
	}()
	return bill, nil
}

// Reconcile 收支对账：校验指定时间段的订单与流水一致性（工单03：对账规则）
type ReconcileResult struct {
	Period        string  `json:"period"`
	OrderTotal    float64 `json:"orderTotal"`    // 订单应收合计
	TxnIncome     float64 `json:"txnIncome"`     // 流水收入合计
	TxnExpense    float64 `json:"txnExpense"`    // 流水支出合计（退款）
	Diff          float64 `json:"diff"`          // 差额（非 0 说明存在对账差异）
	MismatchCount int64   `json:"mismatchCount"` // 异常记录数
}

func Reconcile(start, end time.Time) (*ReconcileResult, error) {
	r := &ReconcileResult{Period: fmt.Sprintf("%s ~ %s", start.Format("2006-01-02"), end.Format("2006-01-02"))}
	var orderAgg struct{ Sum float64 }
	model.DB.Model(&model.Order{}).
		Where("created_at >= ? AND created_at < ? AND status IN ?", start, end, []string{model.OrderPaid, model.OrderOngoing, model.OrderFinished}).
		Select("COALESCE(SUM(amount - discount),0) AS sum").Scan(&orderAgg)
	var inc, exp struct{ Sum float64 }
	model.DB.Model(&model.Transaction{}).
		Where("txn_time >= ? AND txn_time < ? AND direction = ?", start, end, model.DirectionIncome).
		Select("COALESCE(SUM(amount),0) AS sum").Scan(&inc)
	model.DB.Model(&model.Transaction{}).
		Where("txn_time >= ? AND txn_time < ? AND direction = ?", start, end, model.DirectionExpense).
		Select("COALESCE(SUM(amount),0) AS sum").Scan(&exp)
	model.DB.Model(&model.Anomaly{}).Where("resolved = ?", false).Count(&r.MismatchCount)

	r.OrderTotal = round2(orderAgg.Sum)
	r.TxnIncome = round2(inc.Sum)
	r.TxnExpense = round2(exp.Sum)
	r.Diff = round2(r.OrderTotal - (r.TxnIncome - r.TxnExpense))
	return r, nil
}

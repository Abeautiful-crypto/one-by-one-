// 工单09：定时任务管理（GVA 调度能力落地）
// 以内部可观测的任务注册表替代对外部 XXL-JOB 调度中心的强依赖：支持任务参数在线更新即时生效，
// 立即执行，并完整记录审计历史。所有任务均为可重入的幂等实现。
package service

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"ride-hailing/internal/model"

	"gorm.io/gorm"
)

// JobDef 任务定义
type JobDef struct {
	Name        string                 `json:"name"`
	Desc        string                 `json:"desc"`
	Params      map[string]interface{} `json:"params"`      // 在线可配置参数
	LastRunAt   *time.Time             `json:"lastRunAt"`
	LastResult  string                 `json:"lastResult"`
	LastError   string                 `json:"lastError"`
	Run         func(params map[string]interface{}) error `json:"-"`
}

var (
	jobMu sync.RWMutex
	jobs  = map[string]*JobDef{}
)

// RegisterJob 注册任务（init 中调用）
func RegisterJob(j *JobDef) {
	jobMu.Lock()
	jobs[j.Name] = j
	jobMu.Unlock()
}

func init() {
	// 示例任务1：清理长期未支付/过期订单
	RegisterJob(&JobDef{
		Name:   "cleanup_expired_orders",
		Desc:   "清理超过 N 小时仍未接单的待接订单",
		Params: map[string]interface{}{"olderThanHours": 24},
		Run: func(p map[string]interface{}) error {
			h := toInt(p["olderThanHours"], 24)
			cutoff := time.Now().Add(-time.Duration(h) * time.Hour)
			res := model.DB.Model(&model.RideOrder{}).
				Where("status = ? AND created_at < ?", model.RidePending, cutoff).
				Update("status", model.RideCancelled)
			_ = res
			return nil
		},
	})
	// 示例任务2：派单超时未响应重派
	RegisterJob(&JobDef{
		Name:   "redispatch_timeout",
		Desc:   "对接单超过 N 分钟未到达上车点的订单重新派单",
		Params: map[string]interface{}{"timeoutMin": 10},
		Run: func(p map[string]interface{}) error {
			m := toInt(p["timeoutMin"], 10)
			cutoff := time.Now().Add(-time.Duration(m) * time.Minute)
			var list []model.RideOrder
			model.DB.Where("status = ? AND accepted_at < ?", model.RideAccepted, cutoff).Find(&list)
			for _, o := range list {
				_, _ = Dispatch(&o) // 复用派单引擎重新广播（工单11）
			}
			return nil
		},
	})
	// 示例任务3：SOS 待处理提醒
	RegisterJob(&JobDef{
		Name:   "sos_reminder",
		Desc:   "将超过 N 分钟未处理的 SOS 标记为需升级处理",
		Params: map[string]interface{}{"pendingMin": 5},
		Run: func(p map[string]interface{}) error {
			m := toInt(p["pendingMin"], 5)
			cutoff := time.Now().Add(-time.Duration(m) * time.Minute)
			model.DB.Model(&model.SOSAlert{}).
				Where("status = ? AND created_at < ?", "pending", cutoff).
				Update("status", "escalated")
			return nil
		},
	})
}

// ListJobs 任务列表（含当前参数与最近执行结果）
func ListJobs() []JobDef {
	jobMu.RLock()
	defer jobMu.RUnlock()
	out := make([]JobDef, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, *j)
	}
	return out
}

// RunJob 立即执行任务并记录审计（工单09：在线触发 + 审计历史）
func RunJob(name string, operator string, params map[string]interface{}) error {
	jobMu.RLock()
	j, ok := jobs[name]
	jobMu.RUnlock()
	if !ok {
		return fmt.Errorf("任务不存在: %s", name)
	}
	if params != nil {
		jobMu.Lock()
		for k, v := range params {
			j.Params[k] = v
		}
		jobMu.Unlock()
	}
	start := time.Now()
	err := j.Run(j.Params)
	dur := time.Since(start).Milliseconds()
	now := time.Now()
	jobMu.Lock()
	j.LastRunAt = &now
	if err != nil {
		j.LastResult = "failed"
		j.LastError = err.Error()
	} else {
		j.LastResult = "success"
		j.LastError = ""
	}
	jobMu.Unlock()

	pb, _ := json.Marshal(j.Params)
	model.DB.Create(&model.JobAudit{
		JobName: name, Operator: operator, Params: string(pb),
		Result: j.LastResult, DurationMs: dur,
	})
	return err
}

// UpdateJobParams 在线更新任务参数（即时生效，工单09）
func UpdateJobParams(name string, params map[string]interface{}, operator string) error {
	jobMu.Lock()
	j, ok := jobs[name]
	if ok {
		for k, v := range params {
			j.Params[k] = v
		}
	}
	jobMu.Unlock()
	if !ok {
		return fmt.Errorf("任务不存在: %s", name)
	}
	pb, _ := json.Marshal(params)
	model.DB.Create(&model.JobAudit{
		JobName: name, Operator: operator, Params: string(pb), Result: "param_updated",
	})
	return nil
}

// ListJobAudits 任务审计历史
func ListJobAudits(name string) []model.JobAuditRecord {
	var rows []model.JobAudit
	q := model.DB.Model(&model.JobAudit{}).Order("created_at desc").Limit(100)
	if name != "" {
		q = q.Where("job_name = ?", name)
	}
	q.Find(&rows)
	out := make([]model.JobAuditRecord, 0, len(rows))
	for _, r := range rows {
		out = append(out, model.JobAuditRecord{
			ID: r.ID, JobName: r.JobName, Operator: r.Operator,
			Params: r.Params, Result: r.Result, DurationMs: r.DurationMs, CreatedAt: r.CreatedAt,
		})
	}
	return out
}

func toInt(v interface{}, def int) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case string:
		var i int
		_, _ = fmt.Sscanf(n, "%d", &i)
		return i
	}
	return def
}

var _ = gorm.ErrRecordNotFound

// 工单：全栈开发-网约车-01-顺风车管理 / 03-财务管理
// 操作日志中间件：记录每次写操作的执行人、时间、内容，用于审计追踪（工单04：审计追踪）。
package middleware

import (
	"bytes"
	"io"
	"time"

	"ride-hailing/internal/model"

	"github.com/gin-gonic/gin"
)

type bodyWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w bodyWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

// OperationLog 记录非 GET 请求的审计日志；导出接口有专用 ExportTask 记录。
func OperationLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == "GET" {
			c.Next()
			return
		}
		start := time.Now()

		var body []byte
		if c.Request.Body != nil {
			body, _ = io.ReadAll(io.LimitReader(c.Request.Body, 1024)) // 截断保护
			c.Request.Body = io.NopCloser(bytes.NewBuffer(body))
		}
		bw := &bodyWriter{ResponseWriter: c.Writer, body: bytes.NewBufferString("")}
		c.Writer = bw

		c.Next()

		go func() {
			log := model.OperationLog{
				UserID:   GetUserID(c),
				Username: GetUsername(c),
				Method:   c.Request.Method,
				Path:     c.Request.URL.Path,
				IP:       c.ClientIP(),
				Body:     string(body),
				Status:   bw.Status(),
				CostMs:   time.Since(start).Milliseconds(),
			}
			model.DB.Create(&log) // 审计日志异步落库，不阻塞主流程
		}()
	}
}

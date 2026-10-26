// 工单：全栈开发-网约车-04-订单管理
// 统一 API 响应结构与分页工具。
package resp

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Response 统一响应体
type Response struct {
	Code int         `json:"code"` // 0 成功，非0 业务错误码
	Msg  string      `json:"msg"`
	Data interface{} `json:"data,omitempty"`
}

func OK(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Response{Code: 0, Msg: "success", Data: data})
}

func Fail(c *gin.Context, httpStatus, code int, msg string) {
	c.JSON(httpStatus, Response{Code: code, Msg: msg})
}

func BadRequest(c *gin.Context, msg string) {
	Fail(c, http.StatusBadRequest, 400, msg)
}

func Unauthorized(c *gin.Context, msg string) {
	Fail(c, http.StatusUnauthorized, 401, msg)
}

func Forbidden(c *gin.Context, msg string) {
	Fail(c, http.StatusForbidden, 403, msg)
}

func NotFound(c *gin.Context, msg string) {
	Fail(c, http.StatusNotFound, 404, msg)
}

func ServerError(c *gin.Context, msg string) {
	Fail(c, http.StatusInternalServerError, 500, msg)
}

// PageQuery 通用分页入参
type PageQuery struct {
	Page     int `form:"page" binding:"omitempty,min=1"`
	PageSize int `form:"pageSize" binding:"omitempty,min=1,max=200"`
}

func (p *PageQuery) Normalize() {
	if p.Page <= 0 {
		p.Page = 1
	}
	if p.PageSize <= 0 {
		p.PageSize = 10
	}
}

// PageResult 通用分页出参（工单01：大数据集分页查询，避免一次性加载导致内存溢出）
type PageResult struct {
	List     interface{} `json:"list"`
	Total    int64       `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"pageSize"`
}

// 网约车管理系统 - 程序入口
// 工单范围：全栈开发-网约车-01 顺风车管理 ~ 07 营销管理
package main

import (
	"log"

	"ride-hailing/internal/config"
	"ride-hailing/internal/model"
	"ride-hailing/internal/router"
)

func main() {
	cfg := config.Load()
	model.InitDB(cfg) // 数据库连接 + 自动迁移 + 种子数据

	// 工单01/04：敏感信息加密传输 —— 配置 TLS_ENABLED=true 时以 HTTPS 启动（SSL/TLS）
	if cfg.TLSEnabled {
		log.Printf("HTTPS 服务启动于 %s (SSL/TLS 加密传输)", cfg.ServerPort)
		if err := router.Setup(cfg).RunTLS(cfg.ServerPort, cfg.TLSCert, cfg.TLSKey); err != nil {
			log.Fatal(err)
		}
		return
	}
	log.Printf("HTTP 服务启动于 %s（生产环境请配置 TLS_ENABLED=true 启用 HTTPS）", cfg.ServerPort)
	if err := router.Setup(cfg).Run(cfg.ServerPort); err != nil {
		log.Fatal(err)
	}
}

// 网约车管理系统 - 程序入口
// 工单范围：全栈开发-网约车-01 顺风车管理 ~ 07 营销管理
package main

import (
	"log"
	"os"

	"github.com/google/gops/agent" // 工单08：gops 实时进程状态查询（GOPS_ENABLED=1 时启用）

	"ride-hailing/internal/config"
	"ride-hailing/internal/model"
	"ride-hailing/internal/pkg/datasource"
	"ride-hailing/internal/router"
)

func main() {
	cfg := config.Load()
	model.InitDB(cfg) // 数据库连接 + 自动迁移 + 种子数据
	datasource.Init(model.DB) // 工单09：多数据源工厂（主从/读写分离基础，当前仅主库）

	// 工单08：gops agent（实时进程状态查询）。仅 GOPS_ENABLED=1 时启用，避免常驻端口。
	if os.Getenv("GOPS_ENABLED") == "1" {
		if err := agent.Listen(agent.Options{}); err != nil {
			log.Printf("gops 启动失败: %v", err)
		} else {
			log.Println("gops agent 已启用（gops <pid> 可查看进程状态）")
		}
	}

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

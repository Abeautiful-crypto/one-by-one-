// 工单：全栈开发-网约车-05-人员管理
// 全局配置：支持环境变量覆盖，默认使用 SQLite 便于本地直接运行，生产环境可切换 MySQL。
package config

import "os"

// Config 全局配置项
type Config struct {
	DBDriver   string // sqlite / mysql
	DBDSN      string // mysql DSN
	JWTSecret  string
	ServerPort string
	TLSEnabled bool // 工单01/04：敏感信息 HTTPS 加密传输
	TLSCert    string
	TLSKey     string
}

func Load() *Config {
	return &Config{
		DBDriver:   getEnv("DB_DRIVER", "sqlite"),
		DBDSN:      getEnv("DB_DSN", "root:123456@tcp(127.0.0.1:3306)/ride_hailing?charset=utf8mb4&parseTime=True&loc=Local"),
		JWTSecret:  getEnv("JWT_SECRET", "ride-hailing-secret-key"),
		ServerPort: getEnv("SERVER_PORT", ":8080"),
		TLSEnabled: getEnv("TLS_ENABLED", "false") == "true",
		TLSCert:    getEnv("TLS_CERT", "cert.pem"),
		TLSKey:     getEnv("TLS_KEY", "key.pem"),
	}
}

func getEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

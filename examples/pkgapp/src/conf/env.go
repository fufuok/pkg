package conf

import "os"

// 环境变量名集中放在 env.go. 改名会同时影响 env 文件和配置解析.
const (
	// MessageEnvName 非空时覆盖 app_conf.message.
	MessageEnvName = "PKGAPP_MESSAGE"

	// NoteEnvName 保存程序内部配置密文, 使用 xcrypto.Encrypt 生成.
	NoteEnvName = "PKGAPP_NOTE"

	// PayloadKeyEnvName 包装对外 AES-GCM 的随机密钥, 原文为 32 字节的 Raw URL Base64.
	PayloadKeyEnvName = "PKGAPP_PAYLOAD_KEY"
)

// Message 是 PKGAPP_MESSAGE 的热加载值. 空值表示不覆盖 JSON.
var Message string

// loadEnvConfig 在 pkg 重载 env 文件后刷新可热加载变量.
func loadEnvConfig() {
	Message = os.Getenv(MessageEnvName)
}

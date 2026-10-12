package api

import (
	"net/http"

	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/json"
	"github.com/fufuok/pkg/logger"
	"github.com/fufuok/pkg/xcrypto"
	"github.com/fufuok/pkg/xsign"
	"github.com/gin-gonic/gin"

	"github.com/fufuok/pkgapp/conf"
	"github.com/fufuok/pkgapp/service"
)

type echoRequest struct {
	Message string `json:"message"`
	Sign    string `json:"sign"`
}

type openRequest struct {
	Secret string `json:"secret"`
	Sign   string `json:"sign"`
}

// statusHandler 返回当前应用配置和进程内观察值.
func statusHandler(c *gin.Context) {
	cfg := conf.App()
	writeJSON(c, gin.H{
		"message":         cfg.AppConf.Message,
		"note_configured": cfg.AppConf.Note != "",
		"ticks":           service.Ticks(),
		"generation":      service.Generation(),
		"config":          config.ConfigFile,
	})
}

// echoHandler 验证时间签名后返回通用密文. 空消息也是合法的 AES-GCM 输入.
func echoHandler(c *gin.Context) {
	var req echoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.String(http.StatusBadRequest, "invalid json")
		return
	}
	webConf := conf.Main().WebConf
	// 时间签名只验证密钥和有效期, 不绑定 message; 传输仍需 HTTPS.
	if !xsign.VerifySignTTL(webConf.SignKey, req.Sign, webConf.SignTTL) {
		c.String(http.StatusUnauthorized, "invalid sign")
		return
	}
	cfg := conf.App()
	env, err := service.SealMessage(req.Message, cfg.AppConf.PayloadKey, webConf.SignKey)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to seal message")
		c.String(http.StatusInternalServerError, "seal message failed")
		return
	}
	writeJSON(c, env)
}

// openHandler 验证时间签名并还原 SealString 的密文, 错误密文不返回部分明文.
func openHandler(c *gin.Context) {
	var req openRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.String(http.StatusBadRequest, "invalid json")
		return
	}
	webConf := conf.Main().WebConf
	if !xsign.VerifySignTTL(webConf.SignKey, req.Sign, webConf.SignTTL) {
		c.String(http.StatusUnauthorized, "invalid sign")
		return
	}
	message, err := xcrypto.OpenString(req.Secret, conf.App().AppConf.PayloadKey)
	if err != nil {
		c.String(http.StatusBadRequest, "invalid secret")
		return
	}
	writeJSON(c, gin.H{"message": message})
}

// writeJSON 使用 pkg/json 输出.
func writeJSON(c *gin.Context, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to encode response")
		c.String(http.StatusInternalServerError, "encode response failed")
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", body)
}

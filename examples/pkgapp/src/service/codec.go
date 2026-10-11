package service

import (
	"fmt"

	"github.com/fufuok/pkg/xcrypto"
	"github.com/fufuok/pkg/xsign"
)

// Envelope 是对外加密响应. 明文不随密文一起返回.
type Envelope struct {
	Sign   string `json:"sign"`
	Secret string `json:"secret"`
}

// SealMessage 用独立 AES 密钥加密消息, 并生成供客户端下一次请求使用的时间签名.
// 签名不绑定正文, 有效期内可重放; 示例不扩展 pkg 的签名协议.
func SealMessage(message string, payloadKey []byte, signKey string) (*Envelope, error) {
	secret, err := xcrypto.SealString(message, payloadKey)
	if err != nil {
		return nil, fmt.Errorf("seal message: %w", err)
	}
	_, sign := xsign.GenSignNow(signKey)
	return &Envelope{
		Sign:   sign,
		Secret: secret,
	}, nil
}

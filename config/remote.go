package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/fufuok/utils"
	"github.com/fufuok/utils/xhash"
	"github.com/imroc/req/v3"
)

// RespDataSource 数据源响应结构体
type RespDataSource struct {
	OK   int              `json:"ok"`
	Msg  string           `json:"msg"`
	Data []map[string]any `json:"data"`
}

type DataSourceArgs struct {
	Time time.Time
	Conf FilesConf
}

// GetDataSource 获取源数据配置并更新本地文件
// 可在应用级重定义该函数, 覆盖 common.Funcs["GetDataSource"]
func GetDataSource(args any) error {
	params, ok := args.(DataSourceArgs)
	if !ok {
		return fmt.Errorf("invalid data source configuration: %T", args)
	}

	// 新旧文件内容不同时重写文件
	shouldUpdateFile := func(contentBytes []byte) (bool, error) {
		md5Old := xhash.MustMD5Sum(params.Conf.Path)
		md5New := xhash.MD5BytesHex(contentBytes)
		return md5New != md5Old, nil
	}

	return GetDataSourceWithCheck(params, shouldUpdateFile)
}

// GetDataSourceWithCheck 获取数据源并根据检查结果更新本地文件
func GetDataSourceWithCheck(params DataSourceArgs, shouldUpdate func([]byte) (bool, error)) error {
	body, err := GetDataSourceBody(params)
	if err != nil {
		return fmt.Errorf("failed to fetch data source content: %w", err)
	}
	contentBytes := utils.S2B(body)

	if shouldUpdate != nil {
		update, err := shouldUpdate(contentBytes)
		if err != nil {
			return fmt.Errorf("failed to check if update needed: %w", err)
		}
		// 配置内容无变化时, 忽略更新
		if !update {
			return nil
		}
	}

	if err = os.WriteFile(params.Conf.Path, contentBytes, 0o600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	return nil
}

// GetDataSourceBody 获取数据源内容
func GetDataSourceBody(params DataSourceArgs) (string, error) {
	// Token: md5(timestamp + auth_key)
	timestamp := strconv.FormatInt(params.Time.Unix(), 10)
	token := xhash.MD5Hex(timestamp + params.Conf.SecretValue)

	// 请求数据源: IP 白名单 + Token
	var res RespDataSource
	resp, err := req.SetSuccessResult(&res).SetErrorResult(&res).Get(params.Conf.API + token + "&time=" + timestamp)
	if err != nil {
		return "", err
	}

	if res.OK != 1 || !resp.IsSuccessState() {
		return "", fmt.Errorf("data source request failed: [%d] %s", resp.StatusCode, res.Msg)
	}

	// 获取所有配置项数据
	body := ""
	for _, info := range res.Data {
		if txt, ok := info["ip_info"]; ok {
			body += utils.MustString(txt) + "\n"
		}
	}

	body = strings.TrimSpace(body)
	if body == "" {
		return "", errors.New("data source result is empty")
	}
	return body, nil
}

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

// GetDataSource 获取源数据配置
// 可在应用级重定义该函数, 覆盖 common.Funcs["GetDataSource"]
func GetDataSource(args any) error {
	params, ok := args.(DataSourceArgs)
	if !ok {
		return errors.New("invalid data source configuration")
	}

	body, err := GetDataSourceBody(params)
	if err != nil {
		return err
	}

	// 新旧文件内容不同时重写文件
	md5Old := xhash.MustMD5Sum(params.Conf.Path)
	md5New := xhash.MD5Hex(body)
	if md5New != md5Old {
		if err = os.WriteFile(params.Conf.Path, []byte(body), 0600); err != nil {
			return err
		}
	}
	return nil
}

func GetDataSourceBody(params DataSourceArgs) (string, error) {
	// Token: md5(timestamp + auth_key)
	timestamp := strconv.FormatInt(params.Time.Unix(), 10)
	token := xhash.MD5Hex(timestamp + params.Conf.SecretValue)

	// 请求数据源: IP 白名单 + Token
	var res RespDataSource
	resp, err := req.SetSuccessResult(&res).Get(params.Conf.API + token + "&time=" + timestamp)
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

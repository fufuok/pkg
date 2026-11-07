package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/imroc/req/v3"

	"github.com/fufuok/pkg/utils"
	"github.com/fufuok/pkg/xhash"
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
	return GetDataSourceContext(context.Background(), args)
}

// GetDataSourceContext 在 ctx 约束下获取源数据配置并原子更新本地文件.
// ctx 取消或超时时中断在途请求, 且不再覆盖本地文件.
func GetDataSourceContext(ctx context.Context, args any) error {
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

	return GetDataSourceWithCheckContext(ctx, params, shouldUpdateFile)
}

// GetDataSourceWithCheck 获取数据源并根据检查结果更新本地文件
func GetDataSourceWithCheck(params DataSourceArgs, shouldUpdate func([]byte) (bool, error)) error {
	return GetDataSourceWithCheckContext(context.Background(), params, shouldUpdate)
}

// GetDataSourceWithCheckContext 在 ctx 约束下获取数据源, 校验通过后原子替换目标配置文件.
func GetDataSourceWithCheckContext(
	ctx context.Context,
	params DataSourceArgs,
	shouldUpdate func([]byte) (bool, error),
) error {
	if ctx == nil {
		return errors.New("data source context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	body, err := GetDataSourceBodyContext(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to fetch data source content: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
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
	if err := ctx.Err(); err != nil {
		return err
	}

	if err = writeFileAtomic(params.Conf.Path, contentBytes, 0o600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	return nil
}

// GetDataSourceBody 获取数据源内容
func GetDataSourceBody(params DataSourceArgs) (string, error) {
	return GetDataSourceBodyContext(context.Background(), params)
}

// GetDataSourceBodyContext 在 ctx 约束下请求远端数据源并提取非空配置正文.
func GetDataSourceBodyContext(ctx context.Context, params DataSourceArgs) (string, error) {
	if ctx == nil {
		return "", errors.New("data source context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	// Token: md5(timestamp + auth_key)
	timestamp := strconv.FormatInt(params.Time.Unix(), 10)
	token := xhash.MD5Hex(timestamp + params.Conf.SecretValue)

	// 请求数据源: IP 白名单 + Token
	var res RespDataSource
	resp, err := req.R().
		SetContext(ctx).
		SetSuccessResult(&res).
		SetErrorResult(&res).
		Get(params.Conf.API + token + "&time=" + timestamp)
	if err != nil {
		return "", err
	}

	if res.OK != 1 || !resp.IsSuccessState() {
		return "", fmt.Errorf("data source request failed: [%d] %s", resp.StatusCode, res.Msg)
	}

	// 获取所有配置项数据
	var builder strings.Builder
	for _, info := range res.Data {
		if txt, ok := info["ip_info"]; ok {
			builder.WriteString(utils.MustString(txt))
			builder.WriteString("\n")
		}
	}

	body := strings.TrimSpace(builder.String())
	if body == "" {
		return "", errors.New("data source result is empty")
	}
	return body, nil
}

// writeFileAtomic 先在目标文件同目录写入临时文件再重命名替换, 防止半截写入被文件监控读取.
func writeFileAtomic(targetPath string, content []byte, perm os.FileMode) (err error) {
	dir := filepath.Dir(targetPath)
	tmpFile, err := os.CreateTemp(dir, ".tmp-conf-*")
	if err != nil {
		return err
	}
	tmpName := tmpFile.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err = tmpFile.Write(content); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err = tmpFile.Chmod(perm); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err = tmpFile.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, targetPath)
}

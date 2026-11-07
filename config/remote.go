package config

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
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
// ctx 只约束拉取阶段; shouldUpdate 一旦返回 true (回调可能已更新内存状态), 必定继续落盘,
// 避免取消发生在回调与写文件之间时出现内存已生效而磁盘仍为旧内容的分裂.
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
// 兼容边界:
//  1. 目标是符号链接时替换链接指向的真实文件, 不把链接本身换成普通文件; 悬空链接沿用 os.WriteFile 跟随创建.
//  2. 已存在的目标继承其权限位, 新建文件使用传入的 perm; 替换后的属主为当前进程用户.
//  3. 目录不可写但目标文件已存在且可写时, 回退为原地覆写 (与改造前 os.WriteFile 一致, 不具备原子性).
func writeFileAtomic(targetPath string, content []byte, perm os.FileMode) (err error) {
	if info, lstatErr := os.Lstat(targetPath); lstatErr == nil && info.Mode()&os.ModeSymlink != 0 {
		resolved, evalErr := filepath.EvalSymlinks(targetPath)
		if evalErr != nil {
			return os.WriteFile(targetPath, content, perm)
		}
		targetPath = resolved
	}
	existing := false
	if info, statErr := os.Stat(targetPath); statErr == nil && !info.IsDir() {
		perm = info.Mode().Perm()
		existing = true
	}
	dir := filepath.Dir(targetPath)
	tmpFile, err := os.CreateTemp(dir, ".tmp-conf-*")
	if err != nil {
		if existing && errors.Is(err, fs.ErrPermission) {
			return os.WriteFile(targetPath, content, perm)
		}
		return err
	}
	tmpName := tmpFile.Name()
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err = tmpFile.Write(content); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err = tmpFile.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmpName, perm); err != nil {
		return err
	}
	if err = os.Rename(tmpName, targetPath); err != nil {
		return err
	}
	tmpName = ""
	return nil
}

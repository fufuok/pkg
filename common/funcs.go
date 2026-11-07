package common

import (
	"context"
	"errors"
	"reflect"

	"github.com/fufuok/cache/xsync"

	"github.com/fufuok/pkg/config"
)

var (
	// Funcs 通用函数集合, 用于远程配置获取等场景
	Funcs = xsync.NewMap[string, Func]()

	ErrInvalidGetter = errors.New("invalid getter method")
)

type Func func(args any) error

// InvokeConfigMethod 调用配置中指定的方法, 执行远端配置获取
func InvokeConfigMethod(cfg config.FilesConf) error {
	return InvokeConfigMethodContext(context.Background(), cfg)
}

// InvokeConfigMethodContext 向默认数据源传递 ctx 取消信号, 同时保留应用自定义 Funcs 的既有签名与行为.
func InvokeConfigMethodContext(ctx context.Context, cfg config.FilesConf) error {
	if ctx == nil {
		return errors.New("config method context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	fn, ok := Funcs.Load(cfg.Method)
	if !ok || fn == nil {
		return ErrInvalidGetter
	}

	args := config.DataSourceArgs{
		Time: GTimeNow(),
		Conf: cfg,
	}
	if reflect.ValueOf(fn).Pointer() == reflect.ValueOf(config.GetDataSource).Pointer() {
		return config.GetDataSourceContext(ctx, args)
	}
	return fn(args)
}

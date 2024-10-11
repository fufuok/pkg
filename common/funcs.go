package common

import (
	"errors"

	"github.com/fufuok/utils/xsync"

	"github.com/fufuok/pkg/config"
)

var (
	// Funcs 通用函数集合, 用于远程配置获取等场景
	Funcs = xsync.NewMapOf[string, Func]()

	ErrInvalidGetter = errors.New("invalid getter method")
)

type Func func(args any) error

// InvokeConfigMethod 调用配置中指定的方法, 执行远端配置获取
func InvokeConfigMethod(cfg config.FilesConf) error {
	fn, ok := Funcs.Load(cfg.Method)
	if !ok {
		return ErrInvalidGetter
	}

	args := config.DataSourceArgs{
		Time: GTimeNow(),
		Conf: cfg,
	}
	return fn(args)
}

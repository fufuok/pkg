package common

import (
	"github.com/fufuok/utils/xsync"
)

// Funcs 通用函数集合, 用于远程配置获取等场景
var Funcs = xsync.NewMapOf[string, Func]()

type Func func(args any) error

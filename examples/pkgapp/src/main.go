// Package main 只设置项目身份并注册模块.
package main

import (
	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/master"

	"github.com/fufuok/pkgapp/conf"
	"github.com/fufuok/pkgapp/service"
	"github.com/fufuok/pkgapp/web"
)

// init 在 master.Main 解析参数前固定项目身份.
func init() {
	// BinName 决定默认配置、env 和日志名. 运行期 LOCAL_BIN_NAME 不重算已选定路径.
	config.AppName = "PKG.App"
	config.BinName = "pkgapp"
	config.DebName = "ff-pkgapp"

	// 不重复注册 pkg 自动加入的 config.M、common.M、crontab.M.
	master.Register(master.ConfigStage, &conf.M{})
	master.Register(master.MainStage, &service.M{}, &web.M{})
}

// main 把参数、daemon、信号和退出交给 pkg/master.
func main() {
	master.Main()
}

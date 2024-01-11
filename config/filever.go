package config

import (
	"time"
)

type FileVer struct {
	MD5        string
	LastUpdate time.Time
}

// GetFileVer 获取或初始化文件版本信息
func GetFileVer(k any) (ver *FileVer) {
	v, ok := FilesVer.Load(k)
	if ok {
		ver, ok = v.(*FileVer)
		if ok {
			return
		}
	}
	ver = new(FileVer)
	FilesVer.Store(k, ver)
	return
}

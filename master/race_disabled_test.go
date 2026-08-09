//go:build !race

package master

// raceDetectorEnabled 标记当前测试二进制未启用 race 插桩.
const raceDetectorEnabled = false

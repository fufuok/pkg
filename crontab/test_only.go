package crontab

var testModule M

func InitTester() {
	_ = testModule.Start()
}

func StopTester() {
	_ = testModule.Stop()
}

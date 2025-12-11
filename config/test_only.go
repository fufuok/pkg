package config

var testModule M

func InitTester() {
	AppBaseSecretValue = "Tester"
	AppConfigBody = testConfig
	_ = testModule.Start()
}

func StopTester() {
	_ = testModule.Stop()
}

var testConfig = []byte(`{
"log_conf": {
    "level": 0,
    "post_api_env": "POST_API",
    "post_alarm_api_env": "POST_ALARM_API",
    "alarm_code_env": "ALARM_CODE",
    "post_interval": 7
  }
}`)

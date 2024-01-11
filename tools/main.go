package main

import (
	"flag"
	"fmt"
	"log"
	"net/url"

	"github.com/fufuok/utils/base58"
	"github.com/fufuok/utils/xcrypto"

	"github.com/fufuok/pkg/config"
)

var (
	appName         = config.AppName
	baseSecretSalt  = config.BaseSecretSalt
	baseSecretValue = ""

	base58Value string

	// 环境变量名(可选), 待加解密内容
	key, data string

	// 编码用户名密码字符串
	user, password string
)

func main() {
	flag.StringVar(&baseSecretValue, "base", "", "自定义基础密钥")
	flag.StringVar(&baseSecretSalt, "salt", baseSecretSalt, "自定义基础密钥(可选)")
	flag.StringVar(&appName, "appname", appName, "应用名, 用于加密基础密钥")

	flag.StringVar(&base58Value, "b58", "", "base58")

	flag.StringVar(&user, "user", "", "用户名")
	flag.StringVar(&password, "password", "", "密码")

	flag.StringVar(&key, "key", "envname", "环境变量名")
	flag.StringVar(&data, "data", "", "待加密字符串")
	flag.Parse()

	// # go run main.go -base="FF~~666" -appname="FF.YourAPP"
	//
	// 基础密钥原始值:
	// FF~~666
	// 待写入环境变量:
	// BASE_SECRET_KEY=TQeKrAAFJ5godyTxtDw2o1
	// 程序解码测试:
	// FF~~666
	if appName != "" && baseSecretValue != "" {
		result, _ := xcrypto.SetenvEncrypt(config.BaseSecretKeyName, baseSecretValue, baseSecretSalt+appName)
		testResult := xcrypto.GetenvDecrypt(config.BaseSecretKeyName, baseSecretSalt+appName)
		fmt.Printf("\n基础密钥原始值: \n%s\n待写入环境变量: \n%s=%s\n程序解码测试: \n%s\n\n",
			baseSecretValue, config.BaseSecretKeyName, result, testResult)
		return
	}

	// # go run main.go -b58="SAlt~~666"
	//
	// 原始值:
	// SAlt~~666
	// 待写入环境变量:
	// BASE_SECRET_SALT=24TwueXvpmsUZ
	// 程序解码测试:
	// SAlt~~666
	if base58Value != "" {
		result := base58.Encode([]byte(base58Value))
		testResult := base58.Decode(result)
		fmt.Printf("\n原始值: \n%s\n待写入环境变量: \nBASE_SECRET_SALT=%s\n程序解码测试: \n%s\n\n",
			base58Value, result, testResult)
		return
	}

	// # go run main.go -user="user~~666" -password='~!@#$%^&*()_+{}|":?><,./;[]'
	// url.UserPassword:
	// user~~666
	// ~!@#$%^&*()_+{}|":?><,./;[]
	// user~~666:~%21%40%23$%25%5E&%2A%28%29_+%7B%7D%7C%22%3A%3F%3E%3C,.%2F;%5B%5D
	if user != "" || password != "" {
		fmt.Printf("url.UserPassword:\n%s\n%s\n%s\n", user, password, url.UserPassword(user, password))
		return
	}

	// # export BASE_SECRET_KEY=TQeKrAAFJ5godyTxtDw2o1
	// # go run main.go -key="REDIS_AUTH" -data="redis12345" -appname="FF.YourAPP"
	// APP_NAME: FF.YourAPP 基础密钥: FF~~666
	//
	// plaintext:
	//        redis12345
	// ciphertext:
	//        XYq6HwQGzuiQmk2rMEsoE2
	// Linux:
	//        export REDIS_AUTH=XYq6HwQGzuiQmk2rMEsoE2
	// Windows:
	//        set REDIS_AUTH=XYq6HwQGzuiQmk2rMEsoE2
	//
	//
	// testGetenv: REDIS_AUTH = redis12345
	if data != "" {
		// 获取基础密钥
		baseSecretValue = xcrypto.GetenvDecrypt(config.BaseSecretKeyName, baseSecretSalt+appName)
		if baseSecretValue == "" {
			fmt.Println("错误: 请设置 BASE_SECRET_KEY 加密的基础密钥环境变量")
			fmt.Println("示例: ")
			fmt.Println("export BASE_SECRET_KEY=TQeKrAAFJ5godyTxtDw2o1")
			fmt.Println(`go run main.go -key="REDIS_AUTH" -data="redis12345" -appname="FF.YourAPP"`)
			return
		}
		fmt.Println("APP_NAME:", appName, "基础密钥:", baseSecretValue)
		// 使用基础密钥加密
		result, err := xcrypto.SetenvEncrypt(key, data, baseSecretValue)
		if err != nil {
			log.Fatalln(err)
		}
		fmt.Printf("\nplaintext:\n\t%s\nciphertext:\n\t%s\nLinux:\n\texport %s=%s\nWindows:\n\tset %s=%s\n\n",
			data, result, key, result, key, result)
	}

	// 测试解密
	result := xcrypto.GetenvDecrypt(key, baseSecretValue)
	fmt.Printf("\ntestGetenv: %s = %s\n\n", key, result)

	// 程序中要使用上面示例中的 REDIS_AUTH 一般是:
	// redisAuth := xcrypto.GetenvDecrypt("REDIS_AUTH", config.Config().SYSConf.BaseSecretValue)
	// fmt.Println(redisAuth) // redis12345
}

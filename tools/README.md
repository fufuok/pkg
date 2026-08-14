# 环境变量加密工具

用于项目中敏感配置项加解密. 比如各类 API Secret:

1. 项目 git 中不会出现明文信息
2. 运行环境中也不会见到明文信息, 也不能通过环境变量值解密

## 基础密钥

### `BASE_SECRET_KEY`

这个环境变量存放经过加密的基础密钥.

程序运行时, 由程序固化的密钥解密环境变量得到您设定的原始密钥, 其他加密变量都使用解密后的原始密钥再加密. 如:

```go
# go run main.go -base="FF~~666" -appname="FF.YourAPP"

基础密钥原始值:
FF~~666
待写入环境变量:
BASE_SECRET_KEY=TQeKrAAFJ5godyTxtDw2o1
程序解码测试:
FF~~666
```

将得到的 `BASE_SECRET_KEY` 和值写到服务器环境变量, 让程序运行时解密出您设定的原始基础密钥.

------

另外, `BASE_SECRET_KEY` 这个变量的名称可以通过环境变量指定: `BASE_SECRET_KEY_NAME=REAL_BASE_KEY`, 此时程序运行时会读取 `REAL_BASE_KEY` 来解密原始基础密钥.

------

### `BASE_SECRET_SALT`

这个环境变量是加密的盐, 只用于加解密基础密钥. 程序里默认是固化的.

可以在应用程序引用包后直接修改这个全局变量的值, 如:

```go
package main

import (
	"github.com/fufuok/pkg/config"
)

func init() {
	config.BaseSecretSalt = "您的YLM"
	config.AppName = "FF.YourAPP"
}
```

然后您的程序就可以基于上面 2 个变量来加密基础密钥.

------

另外, 这个盐除了使用默认和通过程序修改外, 还可以放在 `.env` 文件里被加载. 需要先将盐的原始值做 `base58` 再放到变量: `BASE_SECRET_SALT`.

```shell
# go run main.go -b58="SAlt~~666"

原始值:
SAlt~~666
待写入环境变量:
BASE_SECRET_SALT=24TwueXvpmsUZ
程序解码测试:
SAlt~~666
```

此时, 程序会在启动时加载 `BASE_SECRET_SALT` 变量值, 解码出盐来替换程序里固化的值, 再参与解密基础密钥.

------

以上都是对原始的基础密钥做保密工作.

------

## 通用敏感信息加密

```
# export BASE_SECRET_KEY=TQeKrAAFJ5godyTxtDw2o1
# go run main.go -key="REDIS_AUTH" -data="redis12345" -appname="FF.YourAPP"
APP_NAME: FF.YourAPP 基础密钥: FF~~666

plaintext:
       redis12345
ciphertext:
       XYq6HwQGzuiQmk2rMEsoE2
Linux:
       export REDIS_AUTH=XYq6HwQGzuiQmk2rMEsoE2
Windows:
       set REDIS_AUTH=XYq6HwQGzuiQmk2rMEsoE2


testGetenv: REDIS_AUTH = redis12345
```

**注意: 先要把加密后的基础密钥设置到环境变量中, 然后观察结果第一行显示的基础密钥是否与您预想的一致.**

## 现有加解密边界

`xcrypto.Encrypt` / `Decrypt` / `GetenvDecrypt` / `SetenvEncrypt` 是 pkg 和应用初始化密钥的唯一底层实现. 现有输出已冻结, 不要改算法、IV、padding 或编码.

实现路径:

1. `secret` 做 `MD5Hex`, 得到 32 字节 AES-256 密钥
2. AES-CBC, Zeros padding
3. 未传 IV 时使用 `key[:16]`
4. 输出 base58
5. `secret` 为空时原样返回明文

已知边界:

- 这是确定性配置包装, 不是通用加密, 也不是 AEAD. 同一明文+同一密钥永远同一密文
- 密文被改或密钥错误时不会明确报错, 通常得到空串或不可用明文; `config` 只拒绝解出空的 `BASE_SECRET_KEY`
- Zeros padding 去不掉明文末尾的 `0x00`
- MD5 只是把任意 `secret` 映射成 32 字节, 不是口令 KDF
- DataRouter tunnel 也复用这组函数, 相同报文会暴露重复

通用加密请使用 `xcrypto.Seal` / `Open` / `SealString` / `OpenString`, 不要改旧函数, 也不要把配置密文交给 `Open`.

```go
// 程序中要使用上面示例中的 REDIS_AUTH 一般是:
redisAuth := xcrypto.GetenvDecrypt("REDIS_AUTH", config.Config().SYSConf.BaseSecretValue)
fmt.Println(redisAuth) // redis12345
```

## 通用 AEAD

`Seal` / `Open` 是新程序的推荐加密入口, 与上面的配置包装完全独立.

- 算法: AES-GCM
- 密钥: 调用方提供 16 或 32 字节原始密钥, 推荐 32 字节; 不要传口令, 也不要 `MD5Hex(secret)`
- 输出: `nonce(12) || ciphertext || tag(16)`; 文本层再套 `base64.RawURLEncoding`
- 同一明文 + 同一密钥每次密文不同, 同一密钥都能解回同一明文
- 认证失败、密钥长度非法、密文过短都返回 error, 不吞错

```go
key := make([]byte, xcrypto.AES256KeySize)
_, err := rand.Read(key)
sealed, err := xcrypto.Seal([]byte("payload"), key)
plain, err := xcrypto.Open(sealed, key)

text, err := xcrypto.SealString("redis-password", key)
plainText, err := xcrypto.OpenString(text, key)
```

不要用这组函数处理 `BASE_SECRET_KEY` 或现有 env 密文. 需要绑定租户、路由等上下文时再考虑后续 `WithAD`, 不要把上下文拼进 key.

## 用户名密码编码

数据库连接密码通常时含有特殊字符的, 一般需要先编码后再加密.

```shell
# go run main.go -user="user~~666" -password='~!@#$%^&*()_+{}|":?><,./;[]'
url.UserPassword:
user~~666
~!@#$%^&*()_+{}|":?><,./;[]
user~~666:~%21%40%23$%25%5E&%2A%28%29_+%7B%7D%7C%22%3A%3F%3E%3C,.%2F;%5B%5D
```









*ff*


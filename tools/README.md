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

本工具只负责加密前把用户名和密码编成 `url.UserPassword`. 它不解密 DSN, 也不替驱动还原密码.

```shell
# go run main.go -user="user~~666" -password='~!@#$%^&*()_+{}|":?><,./;[]'
url.UserPassword:
user~~666
~!@#$%^&*()_+{}|":?><,./;[]
user~~666:~%21%40%23$%25%5E&%2A%28%29_+%7B%7D%7C%22%3A%3F%3E%3C,.%2F;%5B%5D
```

### 加密步骤

1. 用上面的 `-user` / `-password` 得到编码后的 `user:password`.
2. 只把这一段拼进 DSN, 不要对整串 DSN 再做 `QueryEscape` / `PathEscape`.
3. 把拼好的 DSN 当作普通明文, 走本工具的 `-key` / `-data` 加密.
4. 数据库账号仍使用原始密码. 编码只存在于待加密的 DSN 字符串里.

```shell
# 1. 先编码用户名和密码
# go run main.go -user="releaseops" -password='ab+cd/ef'
# 得到: releaseops:ab+cd%2Fef
#
# 2. 再拼 DSN (示例, 按实际主机/库名修改)
# releaseops:ab+cd%2Fef@tcp(127.0.0.1:33084)/xy_releaseops?charset=utf8mb4&parseTime=true&loc=UTC
#
# 3. 整串加密
# export BASE_SECRET_KEY=TQeKrAAFJ5godyTxtDw2o1
# go run main.go -key="RELEASEOPS_MYSQL_DSN" -data='releaseops:ab+cd%2Fef@tcp(127.0.0.1:33084)/xy_releaseops?charset=utf8mb4&parseTime=true&loc=UTC' -appname="XY.CICDAgent"
```

注意:

- `url.UserPassword` 不会编码 `+` 和 `@`. `+` 必须保持字面量, 不要写成 `%2B` 以外的二次转义; `@` 出现在用户名或密码里时, 驱动若按最后一个 `@` 切 host, 需要应用自己处理, 本工具不会额外编码它.
- 字面量 `%` 会被编成 `%25`. 不要先手工 `quote` 一次再交给本工具, 否则会变成 `%252B`.
- 不得使用 `url.QueryEscape` / `QueryUnescape`. `QueryUnescape` 会把字面量 `+` 变成空格.
- `xcrypto.GetenvDecrypt` 只还原加密, 不会还原 `url.UserPassword`.

### 应用端使用

`GetenvDecrypt` 之后得到的仍是“编码过的 DSN”, 不是原始密码. 怎么用取决于驱动, 不要假设 GORM 或本工具会自动解码.

**SQL Server / `sqlserver://...`**

驱动走 `net/url.Parse`, 自己会解 userinfo. 应用只需:

```go
dsn := xcrypto.GetenvDecrypt("CT_ADMIN_DSN", config.Config().SYSConf.BaseSecretValue)
db, err := sqlserver.Open(dsn) // 或 gorm 的 sqlserver.Open(dsn)
```

不要对整串 DSN 再做 `PathUnescape`.

**MySQL / `user:pass@tcp(host:port)/db`**

`go-sql-driver/mysql` 不解码 userinfo, `ParseDSN` 把第一个 `:` 到最后一个 `@` 之间的内容当字面密码. 没有引入 GORM 时更不会有人代解码. 应用必须自己拆字段:

```go
dsn := xcrypto.GetenvDecrypt("RELEASEOPS_MYSQL_DSN", config.Config().SYSConf.BaseSecretValue)
cfg, err := mysql.ParseDSN(dsn)
if err != nil {
	return err
}
cfg.User, err = url.PathUnescape(cfg.User)
if err != nil {
	return err
}
cfg.Passwd, err = url.PathUnescape(cfg.Passwd)
if err != nil {
	return err
}
connector, err := mysql.NewConnector(cfg)
if err != nil {
	return err
}
db := sql.OpenDB(connector)
```

不要:

- 对整串 DSN 做 `PathUnescape` 后再 `sql.Open("mysql", dsn)`
- 把解码后的密码再 `FormatDSN` 回字符串; `@` `:` `/` 会被驱动二次截断
- 使用 `QueryUnescape`

离线给人看明文时, 才对 user/password 做 `PathUnescape`. 那不是连接路径.

### 示例

下面用同一组账号走完整路径. 数据库里存的始终是原始密码, 不是编码串.

```text
用户名: releaseops
原始密码: ab+cd/ef#x
```

#### 1. 编码 userinfo

```shell
go run main.go -user="releaseops" -password='ab+cd/ef#x'
```

输出第三行是待拼进 DSN 的片段:

```text
url.UserPassword:
releaseops
ab+cd/ef#x
releaseops:ab+cd%2Fef%23x
```

对照:

| 原始 | 写入 DSN |
| --- | --- |
| `+` | `+` (不编码) |
| `/` | `%2F` |
| `#` | `%23` |
| `ab%2Bcd` (密码本身含百分号) | `ab%252Bcd` |

不要先手工把 `+` 写成 `%2B` 再交给本工具.

#### 2. 拼 DSN 再加密

MySQL:

```text
releaseops:ab+cd%2Fef%23x@tcp(127.0.0.1:33084)/xy_releaseops?charset=utf8mb4&parseTime=true&loc=UTC
```

SQL Server:

```text
sqlserver://releaseops:ab+cd%2Fef%23x@127.0.0.1:1433?database=xy_crontab
```

```shell
export BASE_SECRET_KEY=TQeKrAAFJ5godyTxtDw2o1
go run main.go -key="RELEASEOPS_MYSQL_DSN" \
  -data='releaseops:ab+cd%2Fef%23x@tcp(127.0.0.1:33084)/xy_releaseops?charset=utf8mb4&parseTime=true&loc=UTC' \
  -appname="XY.CICDAgent"
```

把输出的密文写入环境变量. `GetenvDecrypt` 还原出来的仍是上面这串编码 DSN, 密码字段还是 `ab+cd%2Fef%23x`.

#### 3. 应用端: MySQL (本工具不解码, 驱动也不解码)

```go
package example

import (
	"database/sql"
	"net/url"

	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/xcrypto"
	"github.com/go-sql-driver/mysql"
)

func openMySQL() (*sql.DB, error) {
	// 1. 只解密. 得到的仍是编码过的 DSN.
	dsn := xcrypto.GetenvDecrypt("RELEASEOPS_MYSQL_DSN", config.Config().SYSConf.BaseSecretValue)

	// 2. 先按驱动字面量切开, 再只解码 User / Passwd.
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, err
	}
	if cfg.User, err = url.PathUnescape(cfg.User); err != nil {
		return nil, err
	}
	if cfg.Passwd, err = url.PathUnescape(cfg.Passwd); err != nil {
		return nil, err
	}
	// cfg.Passwd == "ab+cd/ef#x"

	// 3. 用 Config 直连, 不要 FormatDSN 后再 sql.Open.
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(connector), nil
}
```

错误示例:

```go
// 错: 整串解码会破坏 query, 解码后的 # / @ 也无法再当 DSN 字符串用.
plain := xcrypto.GetenvDecrypt("RELEASEOPS_MYSQL_DSN", config.Config().SYSConf.BaseSecretValue)
decoded, _ := url.PathUnescape(plain)
db, err := sql.Open("mysql", decoded)

// 错: QueryUnescape 会把密码里的 + 变成空格.
cfg.Passwd, _ = url.QueryUnescape(cfg.Passwd)
```

#### 4. 应用端: SQL Server (驱动自己解码)

```go
package example

import (
	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/xcrypto"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
)

func openSQLServer() (*gorm.DB, error) {
	// GetenvDecrypt 后直接交给驱动. sqlserver:// 走 net/url.Parse, 会还原 ab+cd/ef#x.
	dsn := xcrypto.GetenvDecrypt("CT_ADMIN_DSN", config.Config().SYSConf.BaseSecretValue)
	return gorm.Open(sqlserver.Open(dsn), &gorm.Config{})
}
```

GORM 本身不解码密码. 这里能还原, 只是因为 `sqlserver://` 使用 URL 解析.

#### 5. 离线看明文 (不是连接路径)

```go
cfg, _ := mysql.ParseDSN(xcrypto.GetenvDecrypt("RELEASEOPS_MYSQL_DSN", baseSecret))
user, _ := url.PathUnescape(cfg.User)
pass, _ := url.PathUnescape(cfg.Passwd)
// user == "releaseops"
// pass == "ab+cd/ef#x"
```

*ff*


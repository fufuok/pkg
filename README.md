# pkg

敏捷开发软件包

最低 Go 版本: 1.27

## 生产契约

以下语义已在生产主模块和启动链上冻结, 不能顺手改算法或输出:

- `xcrypto.Encrypt` / `Decrypt` / `GetenvDecrypt`: AES-CBC + MD5 派生密钥 + base58, 默认 IV 为 `key[:blockSize]`
- `xcrypto.Seal` / `Open` / `SealString` / `OpenString`: 新的通用 AEAD, AES-GCM, 随机 12 字节 nonce, 与 Encrypt 独立
- `utils.ToLower` / `ToUpper` / `Trim*` / `EqualFoldBytes`: ASCII 语义, 不是 Unicode
- `xhash.HashString64` / `Sum32`: 稳定 FNV, 不是 per-process `maphash`
- `utils.FastIntn` / `FastRand` / `Rand`: runtime fastrand + `math/rand` v1, 不是密码学随机

细节见本地 `docs/features/merge-utils-go126-plan.md`. `docs/` 仅作本地技术留底, 不纳入 pkg 提交.

## 时间与时区

应用在 common/master 受控启动初始化成功后默认固定 UTC+8, 不依赖宿主时区或 `TZ`; 可提前设置 `config.DefaultTimeLocation` 选择其他时区. 仅导入 pkg 不会改写全局时区, 不支持运行期切换.

完整用法、纽约极简示例及启动安全边界见 [时区配置与美国时区示例](examples/timezone/README.md).

## 部署

```shell
.
├── bin
│   └── ffapp
├── env
│   └── ffapp.env
├── etc
│   └── ffapp.json
└── log
    ├── daemon.log
    └── ffapp.log
```

## 示例

见: [应用规范范本](examples/pkgapp/README.md) 和 [多服务示例](examples/multiserver/main.go).

Fiber 的请求体上限、代理 IP 验证、超时日志及 HTTPS 行为见 [Fiber 封装说明](web/fiber/README.md).

Gin/Fiber 的 GET 与只读 POST 响应缓存、选型依据和迁移方式见 [HTTP 缓存说明](web/README.md).

原生 SQL 按需使用 [sqldb 事务与结果集助手](sqldb/README.md), 可运行 [SQLite 内存数据库示例](examples/sqldb/README.md).

## 敏感信息加密

见: [tools/README.md](tools/README.md)







*ff*

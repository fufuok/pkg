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

见: [examples](examples)

## 敏感信息加密

见: [tools/README.md](tools/README.md)







*ff*

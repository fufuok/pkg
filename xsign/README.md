# 微服务签名

`MSGenSign`、`MSVerifySign` 与 `MSGetSignRaw` 保留旧版 `k=v&` 拼串和 MD5 签名契约. 该拼串不转义字段, 会跳过空值并把 `url.Values` 多值用逗号合并. 已部署协议需要保持旧签名时, 应在应用的签名与验签入口按固定字段 schema 拒绝歧义输入: 未知或重复字段、非法类型, 键中的 `&`/`=`, 值中的 `&`, 以及与业务规则冲突的空值. pkg 无法从任意 `params any` 推断应用 schema, 旧入口不会自动替业务判断这些输入.

新 `MSGenHMACSign`、`MSVerifyHMACSign` 使用 `url.Values` 字符串/多值模型, 直接用 `url.Values.Encode()` 编码全部字段并计算 HMAC-SHA256. 字段名排序, 同名值保持原顺序, 空字符串参与签名. 签名在字段集合外传输, 验签时独立传入, 不复制字段 map 或抽取 `sign`. 非法十六进制、长度错误或不匹配统一返回 `match=false`; 格式非法时直接返回空 raw, 避免先编码字段.

```go
params := url.Values{"a": {"x&b=y"}}
raw, sign := xsign.MSGenHMACSign(params, secret)
// sign 可通过独立请求头传输, params 保持为参与业务的已解析字段.
_, match := xsign.MSVerifyHMACSign(params, sign, secret)
if !match {
	return errors.New("invalid signature")
}
_ = raw
```

签验双方必须同步采用新规范化规则及独立签名参数; 旧签名不能与新签名互验. `params` 的每个字段名均按普通数据处理, 包括名为 `sign` 的业务字段; 若签名来自 query/body 的 `sign`, 应用应先提取并移除该传输字段, 再把相同业务字段交给签名和验签.

应用在初始化时保证密钥非空, 在入口处理字段 schema、解析错误和重复签名; 库不在每次签验时重复检查这些约束. nil/空集合编码为空消息, 无值切片按标准库语义忽略; 从 query/body 正常解析得到的字段至少有一个值, 手工构造字段时应使用 `[]string{""}` 表示空值. 验签与业务使用同一份已解析字段, 调用期间不并发修改; 若业务字段要求单值, 由应用检查 `len(values)==1`. 方法、路径等影响受保护决策的输入也应纳入字段. 对复杂 JSON, 签原始正文字节或其 SHA-256, 避免把任意对象用 `fmt` 转成字符串.

`MSGenHMACSignWithRaw` 保留原样签名和无错误返回契约, 不做规范化及密钥检查. 调用方必须自行保证密钥非空、待签消息与业务输入一一对应, 并让对端使用相同协议. 现有固定字段发布协议若用 `MSGetSignRaw` 拼串后再调用 `WithRaw`, 应维持原协议并严格验证字段格式; 不可静默换成新规范化规则.

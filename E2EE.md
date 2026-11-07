# Ask4Me 端到端加密（E2EE）规范

状态：可选功能，默认关闭。服务端只存储密文、盐和 KDF 参数，**不接触密码，也不做加解密**。

## 1. 威胁模型

保护：

- 数据库、备份、磁盘快照里的请求正文/表单数据/回答
- ServerChan / Apprise / 微信等推送链路的正文
- 服务端日志、事件流里的正文
- 管理员随手翻库

不保护：

- 恶意或被攻破的服务端：交互页 JS 由服务端提供，服务端可以替换 JS 窃取口令。这是浏览器端 E2EE 的固有限制。
- 元数据：谁、何时、请求大小、通知渠道、IP、`request_id`、交互链接。
- 弱口令：KDF 只增加暴力破解成本；口令强度由用户负责。
- 没有被加密的字段：按钮 action、`encryptTitle:false` 时的标题、MCD 的结构、JSON Forms 的 key/type/format 等结构信息。
- 中间人之外的本地环境：浏览器扩展、被感染的终端不在保护范围。

传输层仍然需要 HTTPS；E2EE 不是 TLS 的替代品。

## 2. 密文格式

```
token = "enc:v1:" + base64url(nonce || ciphertext || tag)
```

- `nonce`：12 字节随机数，每条密文独立。
- `ciphertext`：AES-256-GCM 密文。
- `tag`：16 字节 GCM tag。
- `base64url`：RFC 4648 URL-safe，无 `=` padding。

KDF：

```
key = PBKDF2-HMAC-SHA256(
  password = UTF-8(NFC(password)),
  salt     = 16 bytes random, per request, base64url in e2ee.salt,
  iterations = e2ee.iterations (default 310000),
  dkLen    = 32
)
```

AEAD：

```
AES-256-GCM(key, nonce, plaintext, AAD = "ask4me-e2ee-v1")
```

盐不是秘密，随请求的 `e2ee.salt` 传递；迭代次数随 `e2ee.iterations` 传递。

## 3. 请求格式

```json
{
  "title": "enc:v1:...",
  "body": "enc:v1:...",
  "mcd": ":::buttons\n- [enc:v1:...](ok)\n:::\n\n:::input label=\"enc:v1:...\" submit=\"enc:v1:...\"\n:::",
  "jsonforms": {
    "schema": {
      "type": "object",
      "properties": {
        "name": { "type": "string", "title": "enc:v1:..." }
      }
    },
    "uischema": {
      "type": "VerticalLayout",
      "elements": [
        { "type": "Control", "scope": "#/properties/name", "label": "enc:v1:..." }
      ]
    },
    "data": "enc:v1:...",
    "submit_label": "enc:v1:..."
  },
  "e2ee": {
    "enabled": true,
    "salt": "base64url-16-bytes",
    "iterations": 310000,
    "answers": true
  },
  "notify": {
    "serverchan_sendkey": "SCT..."
  }
}
```

规则：

- `title` / `body`：字符串整体加密成 token。
- `jsonforms.data`：整个 JSON 值 `JSON.stringify` 后加密成 token；页面解密后 `JSON.parse`，类型不丢。
- `jsonforms.schema` / `uischema`：**结构保持明文**（`type` / `properties` / `scope` / `format` 等不改），只把展示文本（`title` / `description` / `label` / `text`）和 `submit_label` 加密成 token。
- `mcd`：块结构保持明文；按钮 label、`input` 的 `label` / `submit` 文本加密；按钮 value 保持明文。
- `nested` 对象/数组里的字符串 token 由页面递归替换；JSON Forms 的 `data` 单独按 JSON 解密。
- 服务端校验：`e2ee.enabled` 时 `body` 必须是非空 `enc:v1:` token（或空，服务端回退默认文案）；`salt` 必须 16 字节；`iterations` 必须在 `[100000, 1000000]`。
- E2EE 是**每个请求自己选**的：服务端 `e2ee.enabled` 只是能力开关，不强制任何请求；请求带 `e2ee.enabled: true` 才加密，不带就按普通明文请求处理。
- 同一个多用户实例上可以混用加密和非加密请求。
- `e2ee.enabled: false`（服务端）时，带 `e2ee` 的请求会被拒绝，避免“看起来加密了其实没有”。

## 4. 回答加密

当 `e2ee.answers` 为 `true`：

- 文本回答：页面把 `text` 输入值加密成 token 再 POST；服务端存 token。
- JSON Forms：页面把整个 `payload` `JSON.stringify` 后加密成 token，作为 `payload_json` 提交；服务端存 token，`user.submitted.data.payload` 是 token 字符串。
- 按钮 action：保持明文（用于 ServerChan Action Link 和简单路由）。如果 action 本身敏感，当前版本不覆盖。
- 服务端原样存、原样返回；发起端 SDK/CLI 用同一口令解密。
- 没有口令就无法提交加密回答；页面会提示先解锁。
- 与 ServerChan Action Link 互斥：E2EE 请求不会生成 action links。

## 5. 页面流程

1. 页面从 DB 读出 `e2ee_salt` / `e2ee_iterations` / `e2ee_answers`，把正文/表单先隐藏。
2. 用户输入口令；页面用 WebCrypto 派生 key。
3. 非 JSON Forms：递归替换正文/标题/MCD label 里的 token，再显示。
4. JSON Forms：先 fetch `/spec`，解密 `schema` / `uischema` 的展示文本和 `data`，再 mount 表单。
5. 提交时按 `answers` 在浏览器里加密 `text` / `payload`。
6. 口令只在内存，默认不写 localStorage、不进 URL、不进表单、不发请求。
7. 错误口令（GCM 校验失败）提示重试；页面保持锁定。

WebCrypto 需要 secure context：`https://` 或 `http://localhost` / `127.0.0.1`。服务端在 `e2ee.enabled` 时会校验 `base_url`。

## 6. SDK / CLI

```js
import { ask } from "ask4me-sdk";

const { result } = await ask({
  endpoint: "https://your-domain.com/v1/ask",
  apiKey: "a4m_...",
  e2ee: {
    password: "shared-out-of-band",
    encryptTitle: true,
    encryptAnswers: true
  },
  payload: {
    title: "审批",
    body: "请确认发布",
    jsonforms: { /* ... */ },
    notify: { serverchan_sendkey: "SCT..." }
  }
});

// result.data.text / result.data.payload 已被 SDK 解密
```

- `e2ee.password` 必填；不会被发送。
- `encryptTitle` 默认 `true`。
- `encryptAnswers` 默认 `true`。
- `e2ee.iterations` 可覆盖默认 310000。
- 续接等待（只传 `request_id`）时，SDK 从 `request.created` 事件里的 `e2ee` 元数据派生 key；如果事件已被跳过，可以显式传 `e2ee.salt` / `e2ee.iterations`。
- CLI：`--e2ee-password` 或环境变量 `ASK4ME_E2EE_PASSWORD`。

## 7. 测试向量

```
password   = "correct horse battery staple"
salt       = AAECAwQFBgcICQoLDA0ODw   (base64url; bytes 00 01 ... 0f)
nonce      = AAECAwQFBgcICQoL         (base64url; bytes 00 01 ... 0b)
iterations = 310000
plaintext  = "Ask4Me E2EE test vector"
token      = enc:v1:AAECAwQFBgcICQoLShTpgXdxErUQILyQ_bp44flBr_TuPYPM7mRWF9xPV_CzoL6ODmd3
```

`echo -n 'Ask4Me E2EE test vector' | ...` 不应直接得到该 token；必须按上面的 KDF/AEAD 参数实现。参考实现：

- 浏览器：`ui/src/e2ee.ts`
- Node SDK：`sdk-js/e2ee.js`
- Go 测试：`e2ee_test.go`

## 8. 兼容与限制

- 默认 `e2ee.enabled: false`，旧配置行为不变。
- 服务端没有“强制所有请求加密”的开关；是否加密由每个调用方决定。如果某个团队想强制自己的客户端加密，应在客户端/SDK 侧做策略，而不是让共享服务器一刀切。
- 加密请求的 `body` 必须是 token；如果客户端只加密了 JSON Forms 却留下明文 `body`，服务端会拒绝。
- 服务端不存储密码，也没有找回机制；忘记口令 = 数据不可恢复。
- 服务端仍然能看到请求结构、大小、时间、渠道、IP 和交互链接；如果这些本身敏感，需要额外的流量分析防护。
- 当前版本没有加密 `mcd` 的按钮 value、`jsonforms` 的结构字段、`options` 里的所有字符串；这些字段可能包含有限信息，按需扩展。

# Ask4Me

> [English](README.md) | 中文

Ask4Me 是一个自建的小服务：你通过 API 发起一个“请求”，服务端把交互链接推送到你的通知渠道（Server酱 / Apprise）。你在手机或浏览器里点开链接，按按钮或输入文字提交后，发起请求的那条 HTTP 长连接会拿到最终结果（JSON 或 SSE）。

从概念上说，这是一种 Human-in-the-Loop：这可能是你能自建的最简单的 Human-in-the-Loop 方案之一。

## 一个请求示例（curl）

`mcd` 必须要加。不加 `mcd` 虽然看起来能跑通（能收到通知），但用户没有任何可操作的部分（没按钮/没输入框），因此也就拿不到有意义的数据。

```bash
curl -sS --max-time 120 \
  -X POST 'http://localhost:8080/v1/ask' \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{"title":"Ask4Me Demo","body":"请点一个按钮回复。","mcd":":::buttons\n- [OK](ok)\n- [Later](later)\n:::"}'
```

你会收到一条带交互链接的通知。点开链接并点一下按钮后，这条 curl 请求会返回：

```json
{
  "request_id": "req_xxx",
  "last_event_type": "user.submitted",
  "data": { "action": "ok", "text": "" },
  "last_event_id": "evt_xxx"
}
```

本仓库包含：

- Go 版 server（二进制名：`ask4me`）
- JavaScript SDK：`ask4me-sdk`（目录：`sdk-js/`）
- JavaScript CLI：`ask4me-cli`（目录：`packages/cli/`）
- Node 封装的 server 启动器：`ask4me-server`（目录：`packages/server/`，用于下载/启动二进制）

## 多平台二进制

推荐从 GitHub Releases 下载对应平台的二进制（由 GoReleaser 产出），或使用 `ask4me-server` 自动下载并启动。

## 启动 Server

### 1) 准备配置（.env）

复制一份示例配置：

```bash
cp .env.example .env
```

至少需要：

- `ASK4ME_BASE_URL`：外部可访问的 base URL，用于拼接交互链接（会发到通知里）
- `ASK4ME_API_KEY`：API 鉴权 key
- 通知渠道二选一（否则请求会很快以 `notify.failed` 结束）：
  - `ASK4ME_SERVERCHAN_SENDKEY`
  - 或 `ASK4ME_APPRISE_URLS`
- `ASK4ME_TERMINAL_CACHE_SECONDS`：终态结果的内存缓存时间（秒）。当客户端 nonStream/SSE 长连接中途断开时，可在这段时间内用同一个 `request_id` 再请求一次拿到终态结果；也用于 SSE 订阅者退出后的短期回查。

### 2) 启动

推荐使用 `ask4me-server` 自动下载/启动：

```bash
npm install -g ask4me-server
ask4me-server --config ./.env
```

或直接运行已下载/已编译的 `ask4me`：

```bash
./ask4me -config ./.env
```

启动后默认监听 `:8080`（可用 `ASK4ME_LISTEN_ADDR` 修改）。

## 最简单用法：nonStream 模式 + 裸请求（curl）

nonStream 是默认模式：不带 `stream=true` 时，`/v1/ask` 会一直阻塞，直到用户在网页端提交或请求过期，然后一次性返回 JSON。

### 1) POST 裸请求（推荐）

```bash
curl -sS --max-time 120 \
  -X POST 'http://localhost:8080/v1/ask' \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{"title":"Ask4Me Demo","body":"请点一个按钮回复。","mcd":":::buttons\n- [OK](ok)\n- [Later](later)\n:::"}'
```

说明：

- `mcd` 必须要加。不加 `mcd` 用户虽然能收到通知，但没有任何可提交的控件。
- `--max-time 120` 是一个示例值：避免你在终端里“无限等”。如果 120 秒内你还没点开通知并提交，curl 会超时退出；你可以用 `request_id` 重新发起“续接等待”（见下文）。
- nonStream 模式下，响应不会返回交互链接（interaction_url）；交互链接会通过通知渠道发到你手机/客户端。

返回示例（终态后返回）：

```json
{
  "request_id": "req_xxx",
  "last_event_type": "user.submitted",
  "data": { "action": "ok", "text": "" },
  "last_event_id": "evt_xxx"
}
```

`last_event_type` 可能的终态值：

- `user.submitted`：用户提交成功（按钮或输入）
- `request.expired`：到期未提交
- `notify.failed`：通知发送失败（通常是没配置通知渠道或渠道异常）

### 2) GET 裸请求（无 header 环境）

如果你所在环境不方便设置 `Authorization: Bearer ...` 头，可以用 GET 并在 URL 上带 `key`：

```bash
curl -sS --max-time 120 -G 'http://localhost:8080/v1/ask' \
  --data-urlencode 'key=change-me' \
  --data-urlencode 'title=Ask4Me Demo' \
  --data-urlencode 'body=请点一个按钮回复。' \
  --data-urlencode $'mcd=:::buttons\n- [OK](ok)\n- [Later](later)\n:::'
```

安全提示：URL 参数可能被代理/日志记录；能用 POST + Authorization 时优先用 POST。

### 3) 超时后续接等待（用 request_id）

当你用 `--max-time 40` 这类客户端超时后，可以用 `request_id` 续接等待：

```bash
curl -sS --max-time 40 -G 'http://localhost:8080/v1/ask' \
  --data-urlencode 'key=change-me' \
  --data-urlencode 'request_id=req_xxx'
```

### 4) 预先生成 request_id（推荐在“非交互环境”使用）

你可以自行生成一个 `request_id`（例如在任务队列里预先分配），然后让 server 复用这个 ID 创建请求。这样做有两个好处：

- 即使 nonStream 长连接中途断开，你也能用同一个 `request_id` 重新请求并取回终态结果
- 你可以把 `request_id` 当作外部系统里的幂等键/关联键使用

规则：

- `request_id` 必须以 `req_` 开头，且只包含小写字母、数字与下划线

示例（GET + 预生成 request_id）：

```bash
curl -sS --max-time 40 -G 'http://localhost:8080/v1/ask' \
  --data-urlencode 'key=change-me' \
  --data-urlencode 'request_id=req_myjob_20260131_0001' \
  --data-urlencode 'title=Ask4Me Demo' \
  --data-urlencode 'body=请回复确认。' \
  --data-urlencode $'mcd=:::buttons\n- [OK](ok)\n- [Later](later)\n:::'
```

## 参数一个一个加（nonStream）

下面用 GET 方式演示“逐步加参数”，并用 `--data-urlencode` 避免手写 URL encode。注意：在加入 `mcd` 之前，请求是不可交互的（用户没法提交），因此也就拿不到有意义的数据。

### 1) 加 title

```bash
curl -sS --max-time 40 -G 'http://localhost:8080/v1/ask' \
  --data-urlencode 'key=change-me' \
  --data-urlencode 'title=Ask4Me Demo'
```

### 2) 加 body

```bash
curl -sS --max-time 40 -G 'http://localhost:8080/v1/ask' \
  --data-urlencode 'key=change-me' \
  --data-urlencode 'title=Ask4Me Demo' \
  --data-urlencode 'body=这是一条测试消息，请点一个按钮或输入一段话。'
```

### 3) 加 expires_in_seconds

```bash
curl -sS --max-time 40 -G 'http://localhost:8080/v1/ask' \
  --data-urlencode 'key=change-me' \
  --data-urlencode 'title=Ask4Me Demo' \
  --data-urlencode 'body=请在 10 分钟内回复。' \
  --data-urlencode 'expires_in_seconds=600'
```

### 4) 加 mcd（重点）

```bash
curl -sS --max-time 40 -G 'http://localhost:8080/v1/ask' \
  --data-urlencode 'key=change-me' \
  --data-urlencode 'title=Ask4Me Demo' \
  --data-urlencode 'body=请选择一个动作，或输入一段话。' \
  --data-urlencode 'expires_in_seconds=600' \
  --data-urlencode $'mcd=:::buttons\n- [OK](ok)\n- [Later](later)\n:::\n\n:::input name="note" label="补充说明" submit="提交"\n:::'
```

## MCD 语法（详细）

MCD 是“交互控件描述”。server 会把 `mcd` 存到数据库，并在交互页 `/r/<request_id>/?k=<token>` 里解析它，渲染按钮和输入框。

当前实现是“按行解析”，只识别两类结构，其它内容会被忽略（不会渲染成 Markdown）。

### 1) Buttons 块

语法：

```text
:::buttons
- [<label>](<value>)
- [<label2>](<value2>)
:::
```

规则：

- `label`：按钮显示文本
- `value`：按钮提交值，最终会出现在终态结果的 `data.action`
- 结束行必须是单独一行 `:::`

示例：

```text
:::buttons
- [OK](ok)
- [Later](later)
:::
```

当用户点击 `OK`，终态事件 `user.submitted` 的 `data` 类似：

```json
{ "action": "ok", "text": "" }
```

### 2) Input 行

语法：

```text
:::input name="<name>" label="<label>" submit="<submit>"
:::
```

规则：

- `label`：输入框上方的提示文本
- `submit`：提交按钮文本
- `name`：当前版本会被解析并保存，但提交时服务端仍固定使用 `data.text` 作为结果字段（`name` 预留给后续扩展，不会改变返回 JSON 的字段名）

示例：

```text
:::input name="note" label="补充说明" submit="提交"
:::
```

当用户提交输入，终态事件 `user.submitted` 的 `data` 类似：

```json
{ "action": "", "text": "用户输入的内容" }
```

### 3) 同时使用 buttons + input

你可以同时提供按钮与输入框：用户点按钮或输入文本都能完成一次提交；提交后页面会显示 “Submitted.”。

## SSE 备用模式（stream=true）

当你需要实时拿到 `request.created`（包含 interaction_url）以及后续事件流时，使用 SSE：

```bash
curl -N -sS \
  -X POST 'http://localhost:8080/v1/ask?stream=true' \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{"title":"Ask4Me","body":"Please respond.","mcd":":::buttons\n- [OK](ok)\n:::"}'
```

SSE 输出格式：

- 每个事件一行 `data: <Event JSON>\n\n`
- 结束标记：`data: [DONE]\n\n`
- 响应头会带 `X-Ask4Me-Request-Id`

## JavaScript SDK（ask4me-sdk）

SDK 目前默认使用 SSE 模式（会自动加 `stream=true`），适合在程序里实时消费事件。

安装：

```bash
npm i ask4me-sdk
```

使用示例：

```js
import { ask } from "ask4me-sdk";

const endpoint = "http://localhost:8080/v1/ask";
const apiKey = "change-me";

const { requestId, result } = await ask({
  endpoint,
  apiKey,
  payload: {
    title: "Ask4Me Demo",
    body: "请随便点一个按钮或回一段话。",
    mcd:
      ":::buttons\n" +
      "- [OK](ok)\n" +
      "- [Later](later)\n" +
      ":::\n\n" +
      ":::input name=\"note\" label=\"补充说明\" submit=\"提交\"\n" +
      ":::",
    expires_in_seconds: 600
  },
  onEvent: (ev) => {
    process.stdout.write(`${JSON.stringify(ev)}\n`);
  }
});

console.log("request_id:", requestId);
console.log("final:", result);
```

## CLI（ask4me-cli）

安装：

```bash
npm i -g ask4me-cli
```

调用示例：

```bash
ask4me-cli -h http://localhost:8080 -k change-me --title 'Ask4Me' --body 'Please respond.'
```

## 从源码编译（可选）

本仓库提供了 GoReleaser 配置（[.goreleaser.yaml](./.goreleaser.yaml)）。如果你只想手动交叉编译，也可以用：

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/ask4me-linux-amd64 .
```

## Node Server 启动器（ask4me-server）

用于自动下载/启动 Go server 二进制，并协助生成配置文件。

安装：

```bash
npm i -g ask4me-server
```

启动（默认写入配置到 `~/.ask4me/.env`）：

```bash
ask4me-server
```

指定配置路径：

```bash
ask4me-server --config ./.env
```

后台运行：

```bash
ask4me-server -d
```

## 贡献

欢迎提交 Issue / PR。开发与贡献约定请见 [CONTRIBUTING.md](./CONTRIBUTING.md)。

## 安全

如发现安全问题，请按 [SECURITY.md](./SECURITY.md) 的方式私下报告。

## License

[MIT](./LICENSE)

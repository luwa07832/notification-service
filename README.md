# notification-service

把通知模板、目标渠道、投递状态和重试次数记录成可查询的服务，支持按渠道与时间范围查询投递结果并追踪失败原因。

## 运行要求

- Go 1.26 或以上
- SQLite（本服务自带存储，不需要外部数据库）

## 构建、测试与启动

```bash
go build ./...
go test ./...
go run .
```

服务默认监听 `127.0.0.1:8080`。可用环境变量覆盖：

| 变量 | 默认值 | 用途 |
|---|---|---|
| `ADDR` | `127.0.0.1:8080` | HTTP 监听地址 |
| `DB_PATH` | `notification-service.db` | SQLite 数据库文件路径 |

## 投递记录

每次真实投递尝试独立登记一条记录，字段为：

| 字段 | 说明 |
|---|---|
| `id` | 服务生成的投递记录唯一标识（UUID v4） |
| `template_id` | 通知模板标识，非空 |
| `channel` | 目标渠道，目前接受 `email`、`sms`、`push` |
| `occurred_at` | 投递发生时间，RFC3339 时间戳（如 `2026-10-01T08:00:00Z`） |
| `status` | 投递状态：`pending`、`retrying`、`succeeded`、`failed` |
| `retry_count` | 截至本次尝试的重试次数，非负整数 |
| `failure_reason` | 失败原因；`failed`/`retrying` 必填非空，`pending`/`succeeded` 必须为空（返回 `null`） |

重复发生的通知按实际发生时间独立登记，后续尝试不会覆盖此前记录的失败原因。

## 已公开的入口

### `POST /api/v1/delivery-records`

提交一条投递记录，HTTP 201 返回与输入一致的记录内容和唯一标识，随后可立即按标识查询：

```json
{
  "template_id": "tpl-welcome",
  "channel": "email",
  "occurred_at": "2026-10-01T08:00:00Z",
  "status": "retrying",
  "retry_count": 1,
  "failure_reason": "smtp 421 try later"
}
```

### `GET /api/v1/delivery-records/{id}`

按唯一标识返回单条记录；不存在时 HTTP 404。

### `GET /api/v1/delivery-records?channel={渠道}&start={开始}&end={结束}`

查询单个目标渠道在 `[start, end)` 半开区间内的记录，按发生时间升序返回：

```json
{"records":[
  {"id":"...","template_id":"tpl-welcome","channel":"sms","occurred_at":"2026-10-01T08:00:00Z","status":"pending","retry_count":0,"failure_reason":null},
  {"id":"...","template_id":"tpl-welcome","channel":"sms","occurred_at":"2026-10-01T08:00:05Z","status":"retrying","retry_count":1,"failure_reason":"timeout"}
]}
```

没有命中记录时返回 `{"records":[]}`，不报错。`start`、`end` 均为 RFC3339 时间戳，且 `start` 必须早于 `end`。

### `GET /healthz`

返回服务与存储状态。正常时 HTTP 200：

```json
{"status":"ok","database":"ok"}
```

存储不可用时 HTTP 503：

```json
{"error":{"code":"storage_unavailable","message":"database is not available"}}
```

## 错误约定

所有错误响应都是单个顶层 `error` 对象，包含 `code` 与 `message` 两个字符串字段；`message` 不包含 SQL、堆栈或文件路径。

| HTTP | code | 场景 |
|---|---|---|
| 400 | `INVALID_DELIVERY_RECORD` | 写入校验失败：渠道不存在、状态非法、重试次数非非负整数、失败/重试缺少原因、成功/待发携带原因等 |
| 400 | `INVALID_DELIVERY_QUERY` | 查询校验失败：渠道不存在、时间格式非法、开始时间不早于结束时间 |
| 404 | `delivery_record_not_found` | 按标识查询不到记录 |
| 503 | `STORAGE_UNAVAILABLE` | 登记存储暂时不可用；此时不会返回任何声称记录已保存的结果 |
| 503 | `storage_unavailable` | `GET /healthz` 检测到数据库不可用 |

投递记录登记与通知发送彼此独立：发送入口、模板内容、渠道分发顺序、重试触发规则和既有成功失败语义均保持不变；新记录只在功能启用后的真实投递尝试中产生，查询结果只包含已经成功登记的记录。

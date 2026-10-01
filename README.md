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

## 已公开的入口

### `GET /healthz`

返回服务与存储状态。正常时 HTTP 200：

```json
{"status":"ok","database":"ok"}
```

存储不可用时 HTTP 503：

```json
{"error":{"code":"storage_unavailable","message":"database is not available"}}
```

### `POST /api/v1/delivery-records`

在每次真实投递尝试发生后登记一条记录。每次尝试独立一行，重试产生的新记录不会覆盖此前的失败原因。

请求体：

| 字段 | 类型 | 说明 |
|---|---|---|
| `template_id` | string，非空 | 通知模板标识 |
| `channel` | string | 目标渠道：`sms`、`email`、`push`、`in_app` |
| `occurred_at` | string | 发生时间，RFC 3339（如 `2026-10-01T10:00:00Z`） |
| `status` | string | 投递状态：`pending`、`retrying`、`succeeded`、`failed` |
| `retry_count` | 非负整数 | 截至本次尝试的重试次数 |
| `failure_reason` | string | 失败原因；`failed`/`retrying` 必填非空，`pending`/`succeeded` 必须为空 |

成功时 HTTP 201，返回与输入一致的内容和服务生成的唯一标识 `id`，随后可立即按标识查询：

```json
{
  "id": "f1c2e0d4-9b72-4e6a-8c11-6b2a4f3d0a11",
  "template_id": "tpl-1",
  "channel": "sms",
  "occurred_at": "2026-10-01T10:00:00Z",
  "status": "failed",
  "retry_count": 2,
  "failure_reason": "provider timeout"
}
```

### `GET /api/v1/delivery-records/{id}`

按投递记录唯一标识查询单条记录，命中时 HTTP 200 返回上述记录对象；不存在时 HTTP 404 与错误码 `DELIVERY_RECORD_NOT_FOUND`。

### `GET /api/v1/delivery-records?channel={channel}&start={time}&end={time}`

按单个目标渠道与时间范围查询投递结果。时间范围为半开区间：`start <= occurred_at < end`，时间参数使用 RFC 3339，且 `start` 必须早于 `end`。结果按发生时间升序排列，并保留通知模板标识、投递状态、重试次数和对应失败原因，便于比较同一次通知在不同重试阶段的结果。

没有命中记录时返回 HTTP 200 与空数组，而不是错误：

```json
{"records":[]}
```

### `GET /api/v1/delivery-records/search`

面向失败追踪的高级检索，用于定位同一模板在指定渠道和时间范围内的重试过程，比较每次尝试的状态、重试次数和失败原因。`channel`、`start`、`end` 与上述按渠道查询口径一致：限定单一渠道及半开区间 `start <= occurred_at < end`，时间使用 RFC 3339 且 `start` 必须早于 `end`。

可选查询参数（全部条件同时满足才返回记录）：

| 参数 | 说明 |
|---|---|
| `template_id` | 按去除首尾空白后的模板标识精确匹配 |
| `status` | 只接受 `pending`、`retrying`、`succeeded`、`failed` |
| `retry_min`、`retry_max` | 非负整数，且 `retry_min <= retry_max`；可只给一端 |
| `failure_reason_contains` | 对失败原因原文做区分大小写的子串匹配 |
| `page` | 页码，从 1 开始，默认 1 |
| `page_size` | 每页条数，默认 50，范围 1 到 200 |

结果按 `occurred_at` 升序、同一时刻按 `id` 升序稳定排列。响应包含 `records` 与 `pagination`，`total` 为全部命中记录的准确数量：

```json
{
  "records": [
    {
      "id": "f1c2e0d4-9b72-4e6a-8c11-6b2a4f3d0a11",
      "template_id": "tpl-1",
      "channel": "sms",
      "occurred_at": "2026-10-01T10:00:00Z",
      "status": "failed",
      "retry_count": 2,
      "failure_reason": "provider timeout"
    }
  ],
  "pagination": {"page": 1, "page_size": 50, "total": 1}
}
```

没有命中记录时返回 HTTP 200，`records` 为空数组并仍返回分页信息。高级检索只读取已登记记录，不改变投递、重试触发、记录写入和历史失败原因的保留方式。

## 错误约定

所有错误响应都是单个顶层 `error` 对象，包含 `code` 与 `message` 两个字符串字段；`message` 不包含 SQL、堆栈或文件路径。

| HTTP | code | 触发场景 |
|---|---|---|
| 400 | `INVALID_DELIVERY_RECORD` | 写入校验失败：渠道不存在、状态非法、`retry_count` 缺失或不是非负整数、`failed`/`retrying` 缺少失败原因、`pending`/`succeeded` 携带失败原因、时间格式非法 |
| 400 | `INVALID_DELIVERY_QUERY` | 查询校验失败：渠道不存在、时间格式非法、`start` 不早于 `end`、缺少时间参数 |
| 400 | `INVALID_DELIVERY_SEARCH` | 高级检索校验失败：参数缺失或为空、类型或格式非法、渠道或状态越界、重试边界为负数或逆序、时间不是 RFC 3339、`start` 不早于 `end`、页码或页大小越界 |
| 404 | `DELIVERY_RECORD_NOT_FOUND` | 按标识查询不到记录 |
| 503 | `STORAGE_UNAVAILABLE` | 登记或查询存储暂时不可用；此时不会返回任何声称记录已保存的结果 |

## 与既有通知发送功能的关系

投递记录是在真实投递尝试发生后补充登记的独立服务，通知发送入口、模板内容、渠道分发顺序、发送结果、重试触发规则和既有成功失败语义均保持不变，现有调用方无需迁移。新记录从启用本功能后的投递尝试开始产生，查询结果只包含已经成功登记的记录。登记服务暂时不可用时，发送动作仍按原行为执行，登记入口返回 HTTP 503 与 `STORAGE_UNAVAILABLE`。

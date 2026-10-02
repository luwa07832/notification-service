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
| `notification_id` | string，可选 | 通知实例标识，用于按实例追踪重试链；省略时按原行为登记。提供时非空且无首尾空白 |

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

### `POST /api/v1/delivery-records/batch`

把同一轮的多次真实投递尝试一次提交、批量登记。请求体使用 `records` 数组，每个元素与单条入口的输入同构（同样的 `template_id`、`channel`、`occurred_at`、`status`、`retry_count`、`failure_reason`、`notification_id` 语义与校验规则，各元素的 `notification_id` 可相同、不同或省略），每批必须包含 2 到 100 条：

```json
{
  "records": [
    {"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"retrying","retry_count":1,"failure_reason":"provider busy"},
    {"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:05Z","status":"failed","retry_count":2,"failure_reason":"provider timeout"}
  ]
}
```

每条记录分别生成唯一 `id`，响应严格保留请求顺序。成功时 HTTP 201：

```json
{"records":[
  {"id":"...","template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"retrying","retry_count":1,"failure_reason":"provider busy"},
  {"id":"...","template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:05Z","status":"failed","retry_count":2,"failure_reason":"provider timeout"}
]}
```

只要 JSON 不合法、`records` 缺失或不是数组、条数不在 2 到 100、任一元素不满足单条校验规则，整批都不登记，统一返回 HTTP 400 与错误码 `INVALID_DELIVERY_BATCH`。整批写入在单个事务内完成：存储不可用或无法完整提交时返回 HTTP 503 与 `STORAGE_UNAVAILABLE`，失败批次不会留下部分记录。写入成功的记录立即可由按标识、渠道时间范围、失败检索与各汇总入口读取，排序、分页与统计口径不变。批量入口只登记调用方声明已发生的投递尝试，不触发通知发送、模板渲染或重试，也不改变单条入口的响应或错误。

### `GET /api/v1/delivery-records/{id}`

按投递记录唯一标识查询单条记录，命中时 HTTP 200 返回上述记录对象；不存在时 HTTP 404 与错误码 `DELIVERY_RECORD_NOT_FOUND`。

### `GET /api/v1/delivery-records?channel={channel}&start={time}&end={time}`

按单个目标渠道与时间范围查询投递结果。时间范围为半开区间：`start <= occurred_at < end`，时间参数使用 RFC 3339，且 `start` 必须早于 `end`。结果按发生时间升序排列，并保留通知模板标识、投递状态、重试次数和对应失败原因，便于比较同一次通知在不同重试阶段的结果。

没有命中记录时返回 HTTP 200 与空数组，而不是错误：

```json
{"records":[]}
```

### `GET /api/v1/delivery-records/search`

面向失败追踪的高级检索，用于定位同一模板在指定渠道与时间范围内的完整重试过程。`channel`、`start`、`end` 与基础查询同口径：限定单个渠道与半开区间 `start <= occurred_at < end`，时间参数为 RFC 3339 且 `start` 必须早于 `end`。

其余条件全部可选，且所有条件同时满足才命中：

| 参数 | 说明 |
|---|---|
| `template_id` | 按去除首尾空白后的模板标识精确匹配；只传空白视为非法 |
| `status` | 仅接受 `pending`、`retrying`、`succeeded`、`failed` |
| `retry_min` | 非负整数，仅返回重试次数不小于该值的记录 |
| `retry_max` | 非负整数，仅返回重试次数不大于该值的记录；要求 `retry_min <= retry_max` |
| `failure_reason_contains` | 对失败原因原文做区分大小写的子串匹配；空值视为非法 |
| `page` | 页码，从 1 开始，缺省为 1 |
| `page_size` | 每页条数，缺省 50，范围 1 到 200 |

结果按 `occurred_at` 升序排列，同一时刻按 `id` 升序稳定排列。响应包含 `records` 与 `pagination`，其中 `total` 是全部命中记录（不受分页限制）的准确数量。页码超出范围或没有命中时，`records` 为空数组并仍返回分页信息：

```json
{
  "records": [],
  "pagination": {"page": 3, "page_size": 50, "total": 0}
}
```

示例：在短信渠道 10:00–11:00 之间查找模板 `tpl-1` 重试次数 1 到 3 次、失败原因包含 `timeout` 的尝试：

```
/api/v1/delivery-records/search?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z&template_id=tpl-1&retry_min=1&retry_max=3&failure_reason_contains=timeout
```

高级检索只读取已登记记录，不改变投递执行、重试触发、记录写入与历史失败原因的保留方式。

### `GET /api/v1/delivery-records/failure-summary`

只读的失败原因汇总查询，用于统计指定渠道与时间范围内各失败原因的出现次数。`channel`、`start`、`end` 必填且与基础查询同口径：限定单个渠道与半开区间 `start <= occurred_at < end`，时间参数为 RFC 3339 且 `start` 必须早于 `end`。

| 参数 | 说明 |
|---|---|
| `channel`（必填） | `sms`、`email`、`push`、`in_app` 之一 |
| `start`（必填） | 范围起点（含），RFC 3339 |
| `end`（必填） | 范围终点（不含），RFC 3339，且必须晚于 `start` |
| `template_id`（可选） | 按去除首尾空白后的模板标识精确匹配；显式传入空白值视为非法 |

只统计 `status` 为 `failed` 或 `retrying` 且带有非空 `failure_reason` 的已登记记录，不含 `pending` 或 `succeeded`。`failure_reason` 保留存储原文并按原文精确分组，不做大小写折叠、去空白或其他归一化。响应为单个 JSON 对象：

```json
{
  "groups": [
    {"failure_reason": "Timeout", "attempt_count": 2},
    {"failure_reason": "bounce", "attempt_count": 1}
  ],
  "total_attempts": 3
}
```

`groups` 按 `attempt_count` 降序排列，计数相同时按 `failure_reason` 原文升序排列；`total_attempts` 是各分组计数之和。没有命中时仍返回 HTTP 200，`groups` 为空数组、`total_attempts` 为 0：

```json
{"groups":[],"total_attempts":0}
```

汇总端点只读，不写入或修改记录，也不改变通知发送入口、模板内容、渠道分发、重试触发、记录写入和历史失败原因的保留方式。

### `GET /api/v1/delivery-records/attempt-overview`

只读的投递概览查询，按通知模板分组集中展示同一渠道与时间范围内的尝试数量、重试跨度、最近结果和失败原因。`channel`、`start`、`end` 必填且与基础查询同口径：限定单个渠道与半开区间 `start <= occurred_at < end`，时间参数为 RFC 3339 且 `start` 必须早于 `end`。

| 参数 | 说明 |
|---|---|
| `channel`（必填） | `sms`、`email`、`push`、`in_app` 之一 |
| `start`（必填） | 范围起点（含），RFC 3339 |
| `end`（必填） | 范围终点（不含），RFC 3339，且必须晚于 `start` |
| `template_id`（可选） | 按去除首尾空白后的模板标识精确匹配；显式传入空白值视为非法 |

响应为单个 JSON 对象，`groups` 按 `template_id` 升序排列，每项包含：

| 字段 | 说明 |
|---|---|
| `template_id` | 通知模板标识 |
| `total_attempts` | 范围内该模板的尝试总数 |
| `retry_count_min` / `retry_count_max` | 范围内重试次数的最小值与最大值 |
| `status_counts` | 固定包含 `pending`、`retrying`、`succeeded`、`failed` 四项计数，未出现的状态计数为 0 |
| `last_attempt` | `occurred_at` 最大且 `id` 最大的记录，含 `id`、`occurred_at`、`status`、`retry_count`、`failure_reason` |
| `failure_reasons` | 仅统计 `failed`/`retrying` 中非空失败原因原文，按次数降序、次数相同按原文升序，每项含 `failure_reason` 与 `attempt_count` |

```json
{
  "groups": [
    {
      "template_id": "tpl-1",
      "total_attempts": 3,
      "retry_count_min": 0,
      "retry_count_max": 2,
      "status_counts": {"pending": 0, "retrying": 1, "succeeded": 1, "failed": 1},
      "last_attempt": {
        "id": "f1c2e0d4-9b72-4e6a-8c11-6b2a4f3d0a11",
        "occurred_at": "2026-10-01T10:00:10Z",
        "status": "succeeded",
        "retry_count": 2,
        "failure_reason": ""
      },
      "failure_reasons": [
        {"failure_reason": "Timeout", "attempt_count": 2}
      ]
    }
  ]
}
```

没有命中时仍返回 HTTP 200，`groups` 为空数组：

```json
{"groups":[]}
```

概览端点只读，不写入或修改记录、不补齐未登记的尝试，也不改变通知发送入口、模板内容、渠道分发、重试触发、记录写入和历史失败原因的保留方式。

### `GET /api/v1/delivery-records/trend`

只读的投递趋势查询，把同一渠道在半开区间 `start <= occurred_at < end` 内的已登记尝试切分成连续的 UTC 时间桶，观察投递结果、失败原因和重试次数随时间的变化。`channel`、`start`、`end`、`interval` 必填，时间参数为 RFC 3339 且 `start` 必须早于 `end`。

| 参数 | 说明 |
|---|---|
| `channel`（必填） | `sms`、`email`、`push`、`in_app` 之一 |
| `start`（必填） | 范围起点（含），RFC 3339，换算到 UTC 后必须落在桶边界 |
| `end`（必填） | 范围终点（不含），RFC 3339，必须晚于 `start`，换算到 UTC 后必须落在桶边界 |
| `interval`（必填） | `hour` 按 UTC 整点切桶，`day` 按 UTC 自然日切桶 |
| `template_id`（可选） | 按去除首尾空白后的模板标识精确匹配；显式传入空白值视为非法 |

响应为单个 JSON 对象，`buckets` 按 `bucket_start` 升序覆盖范围内每一个连续桶，没有任何尝试的桶也按全零返回；`total_buckets` 等于 `buckets` 的数量。每项包含：

| 字段 | 说明 |
|---|---|
| `bucket_start` / `bucket_end` | 桶的半开区间边界，UTC RFC 3339 |
| `total_attempts` | 桶内已登记尝试总数 |
| `status_counts` | 固定包含 `pending`、`retrying`、`succeeded`、`failed` 四项计数，未出现的状态计数为 0 |
| `failure_reasons` | 仅统计桶内 `failed`/`retrying` 的非空失败原因原文，不折叠大小写、空白或同义词，按次数降序、次数相同按原文升序，每项含 `failure_reason` 与 `attempt_count` |
| `retry_count_min` / `retry_count_max` | 桶内重试次数的最小值与最大值；空桶为 `null` |

```json
{
  "buckets": [
    {
      "bucket_start": "2026-10-01T10:00:00Z",
      "bucket_end": "2026-10-01T11:00:00Z",
      "total_attempts": 2,
      "status_counts": {"pending": 0, "retrying": 1, "succeeded": 0, "failed": 1},
      "failure_reasons": [
        {"failure_reason": "Timeout", "attempt_count": 2}
      ],
      "retry_count_min": 1,
      "retry_count_max": 2
    },
    {
      "bucket_start": "2026-10-01T11:00:00Z",
      "bucket_end": "2026-10-01T12:00:00Z",
      "total_attempts": 0,
      "status_counts": {"pending": 0, "retrying": 0, "succeeded": 0, "failed": 0},
      "failure_reasons": [],
      "retry_count_min": null,
      "retry_count_max": null
    }
  ],
  "total_buckets": 2
}
```

趋势端点只读，只统计已登记的尝试，不补造尝试或失败原因，也不改变通知发送入口、模板内容、渠道分发、重试触发、记录写入和历史失败原因的保留方式。

### `GET /api/v1/delivery-records/channel-comparison`

只读的跨渠道失败对比查询，以通知模板、目标渠道、投递状态、重试次数和失败原因为事实数据，比较半开区间 `start <= occurred_at < end` 内多个渠道的差异。时间参数为 RFC 3339 且 `start` 必须早于 `end`。

| 参数 | 说明 |
|---|---|
| `channels`（必填） | 逗号分隔的 2 到 4 个渠道，取值为 `sms`、`email`、`push`、`in_app`；不得重复、不得含空白，按请求顺序返回 |
| `start`（必填） | 范围起点（含），RFC 3339 |
| `end`（必填） | 范围终点（不含），RFC 3339，且必须晚于 `start` |
| `template_id`（可选） | 按去除首尾空白后的模板标识精确匹配；显式传入空白值视为非法 |

成功时 HTTP 200 返回单个 JSON 对象：`channels` 按请求顺序给出渠道；`total_attempts` 与 `status_counts` 按渠道给出尝试数和固定的 `pending`、`retrying`、`succeeded`、`failed` 四项计数；`retry_count_min` 与 `retry_count_max` 给出各渠道重试次数的最小与最大值。`failure_reasons` 只统计 `failed` 或 `retrying` 记录的非空失败原因原文，不折叠大小写、空白或同义词；每项含 `failure_reason`、按渠道的 `channel_counts`（缺该原因的渠道为 0）和合计 `attempt_count`，按合计降序、合计相同按原文升序排列。

```json
{
  "channels": ["sms", "email", "push"],
  "total_attempts": {"sms": 3, "email": 3, "push": 1},
  "status_counts": {
    "sms": {"pending": 0, "retrying": 1, "succeeded": 1, "failed": 1},
    "email": {"pending": 1, "retrying": 1, "succeeded": 0, "failed": 1},
    "push": {"pending": 0, "retrying": 0, "succeeded": 0, "failed": 1}
  },
  "retry_count_min": {"sms": 0, "email": 0, "push": 5},
  "retry_count_max": {"sms": 3, "email": 1, "push": 5},
  "failure_reasons": [
    {"failure_reason": "Timeout", "channel_counts": {"sms": 2, "email": 1, "push": 0}, "attempt_count": 3},
    {"failure_reason": "bounce", "channel_counts": {"sms": 0, "email": 1, "push": 0}, "attempt_count": 1},
    {"failure_reason": "timeout", "channel_counts": {"sms": 0, "email": 0, "push": 1}, "attempt_count": 1}
  ]
}
```

没有命中时仍返回 HTTP 200：各渠道计数为 0、`retry_count_min` 与 `retry_count_max` 为 `null`、`failure_reasons` 为空数组，请求的每个渠道仍然出现：

```json
{
  "channels": ["sms", "email"],
  "total_attempts": {"sms": 0, "email": 0},
  "status_counts": {
    "sms": {"pending": 0, "retrying": 0, "succeeded": 0, "failed": 0},
    "email": {"pending": 0, "retrying": 0, "succeeded": 0, "failed": 0}
  },
  "retry_count_min": {"sms": null, "email": null},
  "retry_count_max": {"sms": null, "email": null},
  "failure_reasons": []
}
```

对比端点只读，不补造投递尝试、重试或失败原因，也不改变通知发送入口、模板内容、渠道分发、发送结果、重试触发和失败原因保留方式。

### `GET /api/v1/delivery-records/template-alignment`

只读的渠道配置一致性核对，用于在追踪失败原因时找出投递渠道与当前模板登记不一致的既有记录。`channel`、`start`、`end` 必填且与基础查询同口径：渠道仅限 `sms`、`email`、`push`、`in_app`，时间范围为半开区间 `start <= occurred_at < end`，时间参数为 RFC 3339 且 `start` 必须早于 `end`。

| 参数 | 说明 |
|---|---|
| `channel`（必填） | `sms`、`email`、`push`、`in_app` 之一 |
| `start`（必填） | 范围起点（含），RFC 3339 |
| `end`（必填） | 范围终点（不含），RFC 3339，且必须严格晚于 `start` |
| `template_id`（可选） | 按去除首尾空白后的模板标识精确匹配；显式传入空白值视为非法 |
| `page` | 页码，从 1 开始，缺省为 1 |
| `page_size` | 每页条数，缺省 50，范围 1 到 200 |

对范围内每条既有记录，按当前 `notification_templates` 登记判定唯一一个 `issue_code`，优先级依次为：

| issue_code | 触发条件 |
|---|---|
| `template_missing` | 不存在相同 `template_id` 的模板 |
| `channel_unsupported` | 模板存在，但其 `channels` 不含记录的渠道 |
| `template_disabled` | 模板支持该渠道，但 `enabled` 为 `false` |

模板存在、支持该渠道且已启用的记录完全一致，不出现在结果中。结果按 `occurred_at` 升序、同一时刻按 `id` 升序排列。响应包含 `issues` 与 `pagination`，每项含 `id`、`template_id`、`channel`、`occurred_at`、`status`、`retry_count`、`failure_reason` 和 `issue_code`；`total` 是分页前全部不一致记录的准确数量：

```json
{
  "issues": [
    {
      "id": "f1c2e0d4-9b72-4e6a-8c11-6b2a4f3d0a11",
      "template_id": "tpl-1",
      "channel": "sms",
      "occurred_at": "2026-10-01T10:00:05Z",
      "status": "failed",
      "retry_count": 2,
      "failure_reason": "provider timeout",
      "issue_code": "channel_unsupported"
    }
  ],
  "pagination": {"page": 1, "page_size": 50, "total": 1}
}
```

没有不一致记录时仍返回 HTTP 200，`issues` 为空数组、`total` 为 0。该端点只把既有记录与当前模板登记做核对，不发送通知、不渲染模板、不触发重试、不改写任何数据，也不改变历史失败原因。

### `GET /api/v1/notifications/{notification_id}/delivery-history`

按通知实例追踪重试链：只返回登记时携带该 `notification_id` 的投递尝试，未携带标识的记录不参与。`records` 按 `occurred_at` 升序、同一时刻按 `id` 升序，每条含现有记录字段及 `notification_id`；`summary` 汇总整条重试链：

| 字段 | 说明 |
|---|---|
| `total_attempts` | 该通知实例已登记的尝试总数 |
| `status_counts` | 固定含 `pending`、`retrying`、`succeeded`、`failed` 四项计数，未出现为 0 |
| `retry_count_min` / `retry_count_max` | 重试次数最小值与最大值；无记录时为 `null` |
| `first_attempt_at` / `last_attempt_at` | 首次与最近一次尝试的发生时间；无记录时为 `null` |
| `failure_reasons` | 只统计 `failed` 与 `retrying` 记录的非空失败原因原文，按次数降序、次数相同按原文升序；每项含 `failure_reason` 与 `attempt_count`；无记录时为空数组 |

```json
{
  "records": [
    {"id":"...","template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"retrying","retry_count":1,"failure_reason":"provider busy","notification_id":"ntf-1"},
    {"id":"...","template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:05Z","status":"failed","retry_count":2,"failure_reason":"provider timeout","notification_id":"ntf-1"}
  ],
  "summary": {
    "total_attempts": 2,
    "status_counts": {"pending": 0, "retrying": 1, "succeeded": 0, "failed": 1},
    "retry_count_min": 1,
    "retry_count_max": 2,
    "first_attempt_at": "2026-10-01T10:00:00Z",
    "last_attempt_at": "2026-10-01T10:00:05Z",
    "failure_reasons": [
      {"failure_reason": "provider busy", "attempt_count": 1},
      {"failure_reason": "provider timeout", "attempt_count": 1}
    ]
  }
}
```

还没有任何登记记录时返回 HTTP 200 与空 `records`，`summary` 计数为 0、重试与时间上下界为 `null`、`failure_reasons` 为空数组。路径标识为空或全空白返回 HTTP 400 与 `INVALID_NOTIFICATION_ID`；已有登记记录但标识不存在返回 HTTP 404 与 `DELIVERY_RECORD_NOT_FOUND`；存储不可用返回 HTTP 503 与 `STORAGE_UNAVAILABLE`。该查询只读，不补造投递事实。

### `POST /api/v1/notification-templates`

登记一个通知模板，补齐投递记录中只有 `template_id` 时缺少的名称、正文和支持渠道。模板登记是终止性的：登记后不提供修改或删除入口。

请求体：

| 字段 | 类型 | 说明 |
|---|---|---|
| `template_id` | string | 模板标识；去除首尾空白后非空，存储去空白后的值 |
| `name` | string | 模板名称；去除首尾空白后非空，存储去空白后的值 |
| `body` | string | 模板正文；保留原文（含首尾空白），但原文至少含一个非空白字符 |
| `channels` | string 数组 | 支持渠道：一到四个不重复的 `sms`、`email`、`push`、`in_app`，必须使用精确原文（不接受带空白的值），并按提交顺序存储 |
| `enabled` | boolean | 是否启用；缺失、为 null 或不是布尔值均视为非法 |

成功时 HTTP 201，返回模板对象：

```json
{
  "template_id": "tpl-1",
  "name": "Login code",
  "body": "Your code is {{code}}",
  "channels": ["sms", "email"],
  "enabled": true
}
```

字段非法返回 HTTP 400 与 `INVALID_TEMPLATE_REQUEST`；`template_id` 重复返回 HTTP 409 与 `TEMPLATE_ALREADY_EXISTS`，已存模板不会被覆盖；存储不可用返回 HTTP 503 与 `STORAGE_UNAVAILABLE`。

### `GET /api/v1/notification-templates`

列出已登记模板，支持两个可选的精确过滤条件（均为精确匹配，其他未知参数忽略）：

| 参数 | 说明 |
|---|---|
| `channel`（可选） | `sms`、`email`、`push`、`in_app` 之一，且不接受带空白或空白值；显式传入非法值返回 HTTP 400 |
| `enabled`（可选） | 字面量 `true` 或 `false`；其他取值（含空白）返回 HTTP 400 |

结果按 `template_id` 升序返回 HTTP 200：

```json
{"templates":[{"template_id":"tpl-1","name":"Login code","body":"Your code is {{code}}","channels":["sms","email"],"enabled":true}]}
```

没有命中时仍返回 HTTP 200 与空数组：

```json
{"templates":[]}
```

### `GET /api/v1/notification-templates/{template_id}`

返回单个模板对象与该模板的投递事实汇总 `delivery_summary`。汇总只读取已经成功登记的投递记录，不补造任何尝试：

| 字段 | 说明 |
|---|---|
| `total_attempts` | 该模板全部已登记尝试数 |
| `status_counts` | 固定含 `pending`、`retrying`、`succeeded`、`failed` 四项计数，未出现为 0 |
| `retry_count_min` / `retry_count_max` | 重试次数最小值与最大值；没有尝试时两者均为 `null` |
| `failure_reasons` | 只统计 `failed` 与 `retrying` 记录的非空失败原因原文，按次数降序、次数相同按原文升序；每项含 `failure_reason` 与 `attempt_count`；没有尝试时为空数组 |

```json
{
  "template_id": "tpl-1",
  "name": "Login code",
  "body": "Your code is {{code}}",
  "channels": ["sms", "email"],
  "enabled": true,
  "delivery_summary": {
    "total_attempts": 5,
    "status_counts": {"pending": 1, "retrying": 1, "succeeded": 1, "failed": 2},
    "retry_count_min": 0,
    "retry_count_max": 5,
    "failure_reasons": [
      {"failure_reason": "Timeout", "attempt_count": 2},
      {"failure_reason": "bounce", "attempt_count": 1}
    ]
  }
}
```

没有任何尝试时，计数为 0、上下界为 `null`、`failure_reasons` 为空数组。标识不存在（或路径标识为空白）返回 HTTP 404 与 `TEMPLATE_NOT_FOUND`；存储不可用返回 HTTP 503 与 `STORAGE_UNAVAILABLE`。模板详情端点只读，不会改变投递记录或模板本身。

## 错误约定

所有错误响应都是单个顶层 `error` 对象，包含 `code` 与 `message` 两个字符串字段；`message` 不包含 SQL、堆栈或文件路径。

| HTTP | code | 触发场景 |
|---|---|---|
| 400 | `INVALID_DELIVERY_RECORD` | 写入校验失败：渠道不存在、状态非法、`retry_count` 缺失或不是非负整数、`failed`/`retrying` 缺少失败原因、`pending`/`succeeded` 携带失败原因、时间格式非法 |
| 400 | `INVALID_DELIVERY_BATCH` | 批量登记校验失败：JSON 不合法、`records` 缺失或不是数组、条数不在 2–100、任一元素不满足单条记录校验规则；整批均不登记 |
| 400 | `INVALID_DELIVERY_QUERY` | 查询校验失败：渠道不存在、时间格式非法、`start` 不早于 `end`、缺少时间参数 |
| 400 | `INVALID_DELIVERY_SEARCH` | 高级检索校验失败：缺少或非法的 `channel`/`start`/`end`、时间不是 RFC 3339、`start` 不早于 `end`、渠道或 `status` 越界、可选条件为空值、`retry_min`/`retry_max` 为负数或逆序、`page` 小于 1、`page_size` 超出 1–200 或类型格式非法 |
| 400 | `INVALID_DELIVERY_SUMMARY` | 失败原因汇总校验失败：缺少或非法的 `channel`/`start`/`end`、时间不是 RFC 3339、`start` 不早于 `end`、`template_id` 显式为空 |
| 400 | `INVALID_DELIVERY_OVERVIEW` | 投递概览校验失败：缺少或非法的 `channel`/`start`/`end`、时间不是 RFC 3339、`start` 不早于 `end`、渠道越界、`template_id` 显式为空白 |
| 400 | `INVALID_DELIVERY_TREND` | 趋势查询校验失败：缺少或非法的 `channel`/`start`/`end`/`interval`、时间不是 RFC 3339、`start` 不早于 `end`、`start`/`end` 未落在桶边界、`template_id` 显式为空白 |
| 400 | `INVALID_DELIVERY_COMPARISON` | 跨渠道对比校验失败：`channels` 缺失、数量不在 2–4 个、含未知或重复渠道、含空白，`start`/`end` 缺失或不是 RFC 3339、`start` 不早于 `end`、`template_id` 显式为空白 |
| 400 | `INVALID_TEMPLATE_ALIGNMENT_QUERY` | 模板一致性核对校验失败：缺少或非法的 `channel`/`start`/`end`、时间不是 RFC 3339、`start` 不早于 `end`、`template_id` 显式为空白、`page` 小于 1、`page_size` 超出 1–200 或类型格式非法 |
| 400 | `INVALID_TEMPLATE_REQUEST` | 模板登记或列表过滤校验失败：`template_id`/`name` 去空白后为空、`body` 无非空白字符、`channels` 数量不在 1–4 个、含未知或重复渠道或带空白、`enabled` 缺失或不是布尔值；列表的 `channel`/`enabled` 不是合法精确值 |
| 400 | `INVALID_NOTIFICATION_ID` | 投递历史查询的路径通知标识为空或全空白 |
| 404 | `DELIVERY_RECORD_NOT_FOUND` | 按标识查询不到记录，或已有登记记录但按通知标识查询不到投递历史 |
| 404 | `TEMPLATE_NOT_FOUND` | 按标识查询不到模板，或路径中的 `template_id` 为空白 |
| 409 | `TEMPLATE_ALREADY_EXISTS` | 登记模板时 `template_id` 已存在；已存模板保持不变 |
| 503 | `STORAGE_UNAVAILABLE` | 登记或查询存储暂时不可用；此时不会返回任何声称记录已保存的结果 |

## 与既有通知发送功能的关系

投递记录是在真实投递尝试发生后补充登记的独立服务，通知发送入口、模板内容、渠道分发顺序、发送结果、重试触发规则和既有成功失败语义均保持不变，现有调用方无需迁移。新记录从启用本功能后的投递尝试开始产生，查询结果只包含已经成功登记的记录。登记服务暂时不可用时，发送动作仍按原行为执行，登记入口返回 HTTP 503 与 `STORAGE_UNAVAILABLE`。

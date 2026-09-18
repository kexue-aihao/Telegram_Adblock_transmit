# 内置广告库管理

入口：WebUI **规则管理 → 内置广告库**（`#/builtin`）。

支持查看检测条件、总开关和逐项启停。内置检测逻辑随程序版本维护，
检测表达式只读；需要新增或编辑正则时使用「自定义规则」。

## 配置与生效

- 新安装且没有保存配置时，总开关取自 `ADFILTER_ENABLED`，所有单项默认开启。
- 面板保存后，`builtin_settings` 表中的配置优先于环境变量。
- 保存成功后立即用于后续检测；已经开始处理的消息可能仍使用原配置快照。
- 保存失败时运行中的配置保持不变。并发修改不同检测项会合并，修改同一项以最后成功的请求为准。
- 重启后会恢复数据库配置，包括 WebUI 被关闭的情况。
- 关闭总开关会暂停所有内置检测，但保留单项选择；自定义规则不受影响。
- 单项关闭仅移除该项判断，同一消息仍可能命中其他内置项或自定义规则。
- 程序升级新增的检测项默认开启，已有检测项的停用状态按稳定 ID 保留。

数据库迁移 `0006_builtin_settings.sql` 随程序启动自动执行。

## API

接口使用现有 WebUI 登录会话 Cookie。PATCH 请求还需
`Content-Type: application/json` 和 `X-Requested-With: fetch`。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/builtin-rules` | 获取总开关、检测目录、单项选择及实际生效状态 |
| PATCH | `/api/builtin-rules` | 部分更新总开关或指定检测项 |

GET 和成功的 PATCH 返回：

~~~json
{
  "enabled": true,
  "rules": [
    {
      "id": "ad_bot_mention",
      "name": "机器人提及广告",
      "description": "消息包含机器人提及，并同时包含广告关键词或链接时命中。根据 Telegram 消息实体识别机器人。",
      "enabled": true,
      "effective": true
    }
  ]
}
~~~

示例只展示一个检测项，实际返回六项。正则检测项还包含只读的 `pattern`。
`enabled` 是单项选择，`effective` 表示总开关与单项开关同时开启。

关闭总开关：

~~~json
{"enabled": false}
~~~

开启总开关并停用机器人提及检测，其他检测项保持原配置：

~~~json
{"enabled": true, "rules": {"ad_bot_mention": false}}
~~~

| 检测项 ID | 条件 |
| --- | --- |
| `ad_invite_link` | `t.me/+` 邀请链接 |
| `ad_invite_joinchat` | `t.me/joinchat/` 邀请链接 |
| `ad_shortlink` | 内置短链接域名 |
| `ad_keyword_link` | 广告关键词后 40 个字符内出现链接 |
| `ad_bot_mention` | 机器人提及实体与广告关键词或链接组合 |
| `ad_channel_forward_link` | 频道转发来源与链接组合 |

空更新、未知检测项、非布尔值或未知字段返回 400；未登录返回 401；
缺少防护请求头返回 403；保存失败返回 500，并保持当前运行配置。
内置库没有配置到服务时返回 503。

## 验证

单元及竞态测试覆盖六项分别停用、总开关、数据库默认值优先级、
模拟存储失败、配置重载和并发部分更新。浏览器回归覆盖页面入口、
开关状态、失败回滚、刷新后状态及手机布局。

真实数据库往返测试需要单独提供 `TEST_DATABASE_URL`：

~~~text
go test ./internal/store -run '^TestBuiltinSettingsRepositoryIntegration$' -v
~~~

没有该变量时测试会跳过。详见 [WebUI 开发验证](webui-development.md)。

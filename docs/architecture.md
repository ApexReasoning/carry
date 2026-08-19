# Carry 架构设计

## 1. 这份文档解决什么问题

Carry 的架构只需要保证一件事：团队交给 Carry 的 Work，不会因为一次对话、一个 Agent、一个进程或一台机器结束而丢失。

本文定义第一版必须存在的事实、边界和失败语义。它不提前设计未来平台，不列完整数据库字段，也不为尚未实现的能力创建 package。

新增任何边界前都要回答：

1. 它拥有哪个独立事实？
2. 哪条当前产品路径正在使用它？
3. 它保护什么不能由相邻边界直接表达的不变量？
4. 删除它以后，产品具体失去什么？

答不清楚，就不创建。

## 2. 第一版架构

Carry 是一个模块化单体，加一个独立执行 Host。

```text
Web / Carry CLI / Lark / Slack
        │
        ▼
┌───────────────────────────┐
│       carry-server        │
│                           │
│ User API                  │
│ Connector callbacks       │
│ Work / Run / Action 控制  │
│ Host API / Agent API      │
│ Background workers        │
└─────────────┬─────────────┘
              │
             PostgreSQL
              │
              ▼
┌───────────────────────────┐
│ carry host（carry 子命令）│
│                           │
│ Pi / Codex                │
│ Run 临时目录              │
│ 已选择的 MCP transport    │
│ 可选 sandbox              │
└───────────────────────────┘
```

### carry-server

服务端是唯一 server composition root，显式构造 PostgreSQL、Lark、Slack、Pi/Codex Host 协议和各个当前 worker。Artifact 首次出现后再加入具体 Object Storage。

它不通过 provider registry、全局变量或自动发现决定使用哪个实现。

### carry 二进制

`carry` 同时提供成员 CLI 和 Host 运行模式，但两种模式使用不同身份。

成员命令通过 User API 创建、查看和管理 Work，也可以回复问题、验收 Result 或批准 Action：

```text
carry login
carry work list
carry work show <id>
```

Host 是同一二进制的无人值守子命令：

```text
carry host enroll
carry host start
carry host status
```

`carry host enroll` 由已登录成员发起并在服务端验证 Space 权限。成功后服务端签发独立 Machine identity 和证书。此后 `carry host start` 只使用 Machine mTLS，报告本机可用的 Pi/Codex Runtime 并领取 Run，不继续使用成员 token。

因此“通过 CLI 注册 Agent”保持成立，但更准确地说，是 CLI 完成 Machine enrollment，Host 再报告本机可用 Runtime。Pi/Codex 本身不注册成成员，也不直接连接服务端。

Runtime observation 只记录 Machine 上的物理探测结果，不能进入 Work、Run admission 或 claim eligibility。具体 Attempt 使用哪个 adapter，是 Host 在获得通用 claim 后的执行事实，不是 Work 类型或服务端 provider 路由。

同一个发布物不代表同一个 principal：成员 token 与 Machine key 分开保存，Host 进程不能借用成员权限。

Host 负责调用 Pi 或 Codex CLI、准备临时目录、运行当前已选择的本地 MCP transport、诊断进程并回传结果。它不拥有 Work 权限，最终授权判断始终在服务端完成。

### PostgreSQL

PostgreSQL 同时承担权威存储和协调：

- 事务；
- 连续序号；
- 唯一 winner；
- lease 与 fence；
- 幂等；
- queue 与 outbox。

第一版不引入 Kafka、Redis queue、Temporal 或微服务事务。

### Object Storage

Object Storage 不是空仓库阶段的基础依赖。第一份需要长期保存的 Plugin package、附件或 Result bytes 出现时，Artifact owner 才引入具体 Object Storage；PostgreSQL 保存身份、digest、权限和来源。

## 3. 边界总览

第一版只有以下核心 owner：

| 边界 | 拥有的事实 | 当前消费者 | 删除后会失去什么 |
| --- | --- | --- | --- |
| Identity | 成员身份和认证关系 | User API、Space | 无法证明当前成员是谁 |
| Space | Membership、团队权限、可用连接与执行许可 | 所有授权路径 | 无法确定团队边界 |
| Conversation | 私人消息、参与者和隐私 | Web、Lark、Slack、Conversation Run | 无法保持私人对话 |
| Work | 责任、负责人、当前理解、消息、Result、Timer | 成员和 Work Run | 无法持续承担工作 |
| Run | 一次执行请求及其物理 attempts | Host、Pi、Codex | 无法安全执行和恢复 |
| Delivery | 远端会话关联和普通消息的出站投递 | Conversation、Work、Lark、Slack | 无法沿已授权渠道回复 |
| Action | 外部改变的授权和结果 | Connector、Plugin | 无法安全执行高后果操作 |
| Plugin | 安装版本、权限和 MCP/Skill 运行绑定 | Pi、Codex、Host | 无法一致使用 Agent Plugins |
| Host | Machine 身份、执行许可和 Runtime 控制 | Run | 无法把执行安全交给远端机器 |

Event 只有在第一条生产 Event 来源出现后才成为独立 owner。Artifact 只有在第一份必须长期保存的 bytes 出现后才成为独立 owner。在此之前不创建对应空 package。

具体 provider 不属于通用平台边界：

- `connector/lark` 和 `connector/slack` 拥有各自远端协议；
- `agent/pi` 和 `agent/codex` 拥有各自 Runtime 协议。

## 4. Work 是连续性的事实源

Work 拥有：

- Space；
- 一句话目标；
- Open、Paused、Closed 生命周期；
- 当前负责人和任期历史；
- Work Message；
- 当前理解 revision；
- Result 和 review；
- Timer；
- 需要成员回答或决定的事项；
- 尚未应用的新输入；
- 当前 writer。

Work 不保存 Pi、Codex、模型、Session、机器、Git repository 或 sandbox。

### 4.1 Work 输入

会改变 Carry 理解的事实，在各自强类型记录中获得连续 `input_seq`：

- Work Message；
- Timer firing；
- Event admission（只在 Event owner 已经存在时）；
- Result review；
- Action result；
- 负责人或生命周期变化。

不建立开放 JSON WorkEvent 表。

Work 只需要三个进度值：

```text
input_head_seq
applied_input_seq
current_revision
```

含义分别是：

- 已可靠记录到哪里；
- 已反映到当前理解哪里；
- 当前理解是哪一版。

因此产品可以准确区分“已记录”和“已理解”，不需要 FactBatch、enqueue cursor 或逐事实协调状态机。

### 4.2 Work 协调

同一个 Work 同时最多有一个协调 Run。

```text
读取 (applied_input_seq, input_head_seq]
→ 固定本次输入上限和 base revision
→ 当前 writer 形成新理解
→ CAS 提交当前理解
→ 原子推进 applied_input_seq
```

运行期间到达的新输入留给下一次协调，不改变本次提交范围。

一个简单 coordinator worker 持续查找 `Open AND applied_input_seq < input_head_seq` 且没有 active coordinator 的 Work，并用一个事务创建唯一协调 Run。因此输入在已有 Run 执行期间到达也不会搁置，不需要额外 wake-up entity。

如果输入不需要改变当前理解，可以只推进 `applied_input_seq`，但必须记录明确原因，不创建空 revision。

### 4.3 单 writer

第一版只有当前协调 Run 持有 writer token并完成本次理解。并行 Child Run 不预建；只有真实 journey 证明一个 coordinator 无法自然完成时，才加入 parent-scoped ContributionTask。

提交 Work 当前理解时，服务端检查：

- Work 仍为 Open；
- Work authority version 没有变化；
- writer token 仍属于当前 Run；
- base revision 仍然有效；
- 提交覆盖准确输入范围。

失去 writer token 的 Run 不能修改 Work。

### 4.4 负责人

每个开放 Work 有且只有一名当前负责人。

转交需要明确 proposal 和目标成员接受。接受时在一个事务中：

- 验证当前任期；
- 验证目标成员仍属于 Space；
- 结束旧任期；
- 建立唯一新任期；
- 使竞争 proposal 失效；
- 轮换 Work authority version；
- 撤销旧 writer。

Agent 可以建议，不能自行转交。

### 4.5 Pause、Close 与 Reopen

Pause 或 Close 会轮换 Work authority version，并阻止：

- 新协调 Run；
- 旧 Run 提交 revision；
- 新 Timer firing；
- 新 Plugin tool call；
- 尚未开始外部 I/O 的 Work-bound Action。

已经可能发出的 Delivery 或 Action 继续按照证据进入成功、失败或 Unknown，不能假装被撤销。

Reopen 必须来自成员明确决定。尚未处理的输入仍保留，由新 authority version 下的协调 Run 继续。

### 4.6 Needs You

Needs You 是查询，不是 package 或通用 Attention 表。

它只聚合已有 owner 的未决事实：

- 等待某个成员回答的问题；
- Result review；
- 负责人转交；
- 生命周期决定；
- Action 批准或 Unknown 处理。

Agent 重试、lease 和内部恢复不进入 Needs You。

## 5. Conversation 与 Work 创建

Conversation 保存成员与 Carry 的私人对话。

私人消息不会因为语义相关自动进入共享 Work。

明确委托可以通过两条路径创建 Work：

1. 成员直接通过 User API 创建；
2. Conversation Run 引用一条准确的成员消息，请求服务端创建。

第二条路径中，服务端验证成员、Membership、原消息和幂等身份。Agent 只能提交目标理解，不能伪造成员或添加权限。

Work 保存成员授权后形成的新目标和归因。共享 Work 读者不能通过来源关系读取私人原文。

## 6. Run 与执行恢复

Run 表示一次明确执行请求。基线 subject 只有两种：

- 回复一条 Conversation Message；
- 推进一个 Work 的准确输入范围。

Event routing subject 只在第一个 Event owner 与生产来源实现时加入。ContributionTask 只在一个真实并行执行 journey 出现时加入；两者都不作为空分支预埋。

### 6.1 Attempt

Run 是逻辑任务，Attempt 是一次物理执行。

同一个 Run 可以依次产生多个 attempts，但同一时刻最多一个有效。

Host claim 时原子完成：

- 验证 Run 仍可执行；
- 验证该 Machine 仍被 Space 允许；
- 创建新的 attempt fence；
- 获得 lease；
- 记录实际 Pi/Codex 和安全环境；
- 签发短期 Run credential。

只有当前 fence 可以续租、提交或结束。

### 6.2 Pi 与 Codex

Pi 和 Codex 是两个具体 production adapter。Host 只共享真实共同能力：

```text
Start
Resume
Send
Diagnose
Close
```

各 adapter 自己解释 provider Session。它们不能读取或伪造对方的 Session。

启动前，Host 获得一个不可变 Run descriptor，包含 subject、输入引用、Plugin 版本、Skills、MCP endpoint 和短期 Agent 能力。

Pi 与 Codex 必须通过同一产品 conformance suite，但不要求拥有相同内部事件模型。

### 6.3 恢复

lease 过期不代表 Runtime 已停止。

恢复者先获得唯一 recovery claim。任何恢复都会先终结旧 attempt、轮换 fence 和短期 credential，使分区中的旧 Host 不能再提交，然后创建一个新 attempt：

- 有可靠 provider 证据时，新 attempt 可以 Resume 原生 Session；
- 没有可恢复 Session 时，新 attempt 从持久 subject 重新开始；
- 无法证明外部后果时保持 Unknown，不通过重放猜测。

新 attempt 从持久 Work 和 subject 继续，不继承另一个 Runtime 的隐藏上下文。

## 7. Agent API

权威 Agent API 运行在 `carry-server`。Host 上的 Bridge 只转发本地调用，不决定权限。

每次调用验证：

- 当前 Run 和 attempt fence；
- 当前 Space 与 Work authority；
- 当前能力；
- 涉及的 Plugin、Endpoint 或 credential binding 是否仍有效。

只有提交 Work 当前理解需要 writer token。Conversation 回复和只读调用不需要虚构 writer 权限；Event routing 与 Contribution 能力只随对应真实 journey 一起加入。

Agent API 只提供窄能力：

- 读取准确 Conversation 或 Work context；
- 提交一条幂等的 Conversation reply；
- 提交 writer revision；
- 从准确成员消息创建 Work 或 Timer；
- 提出 Action；
- 调用经过 policy mediation 的 Plugin tool；
- 结束或诊断当前 Run。

它不提供原始数据库、长期 credential、通用 provider client 或 server shell。

所有 mutating Agent API 调用在各自 owner 中保存 Run-scoped idempotency identity，不建立通用 operation log。第一版至少保证：

- 每个 Conversation response Run 只有一个 accepted reply；
- writer revision 由 base revision、输入上限和 writer token 唯一裁决；
- Action proposal retry 返回同一个 proposal。

Conversation reply 的事务同时验证 Run/fence、保存 Carry-authored Conversation Message，并为仍有效的 EndpointLink 创建 Delivery outbox。Attempt 在该事务之后崩溃，新的 attempt 重试时得到同一 Message，而不是再发送一条。

## 8. Delivery：渠道关联与出站投递

Delivery 不拥有入站消息内容或 provider callback。它只拥有两类跨渠道事实。

### 8.1 EndpointLink

EndpointLink 把一个准确的 Lark/Slack conversation 或 thread 关联到 Carry Conversation 或 Work。

它固定：

- Connector installation；
- 远端 conversation/thread identity；
- 目标 Conversation 或 Work；
- inbound/outbound 范围；
- generation 和撤销状态。

EndpointLink 是通信同意，不是 Membership 或 Work 权限。

### 8.2 Delivery

Delivery 把一条已经保存的 Conversation Message 或 Work Message 送往一个有效 EndpointLink。

状态只有：

```text
Pending
Sending
Sent
Failed
Unknown
```

普通原会话回复属于 Delivery，不属于 Action。

worker 获得 fenced claim 后才开始发送。响应丢失或 Sending 超时进入 Unknown。只有 provider 提供可靠幂等或只读确认时才能安全重试。

### 8.3 Delivery 与 Action 的界线

规则只有一句：

> 沿已有、已授权的会话关系传递一条已有消息，是 Delivery；扩大受众或改变外部系统，是 Action。

例如：

- 回复原 Lark 私聊：Delivery；
- 回复已关联的 Slack Work thread：Delivery；
- 主动向另一个频道发送：Action；
- 修改、删除远端消息：Action。

主动发送型 Action 由 Action 记录结果，不再额外创建一条重复 Delivery 状态机。

## 9. Connector

`connector/lark` 和 `connector/slack` 分别拥有：

- 安装与远端身份；
- 签名和 callback 协议；
- native actor、message、thread 和 event；
- raw payload 与保留；
- provider API client；
- provider 幂等和限流；
- provider-specific Action command 与 observation。

两者共享 Delivery 合同，但不共享虚构的 native 消息模型。

没有 provider registry，也不建立语义不明的通用 webhook Connector。

### 9.1 入站事务

收到 callback 后，一个 PostgreSQL 事务完成：

```text
验证签名与 native 幂等身份
→ 映射真实 actor
→ 验证 Membership 与 EndpointLink
→ exactly one：
   Conversation Message
   Work Message
   Event（仅当该 Connector 已实现 Event owner）
→ 保存固定 native-to-internal 映射
```

没有明确目标、且尚未实现 Event owner 的 callback，由具体 Connector 按其协议拒绝、忽略或隔离，不进入一个预埋的通用 Event 分支。

事务成功后才向 provider 返回成功。

同一个 native ID 携带不同内容必须 fail closed。不保存第二份通用 InboundMessage。

## 10. Event

本节是第一个生产 Event 来源的 promotion contract，不属于没有消费者时的基线实现。

Event 是经过授权采集、但没有明确 Conversation 或 Work 目标的外部事实。

Event 不能创建 Work或授予权限，只能被关联到零个、一个或多个已有 Work。

语义不明确时可以创建临时 routing Run。Agent 只提出关联；服务端提交时重新验证 Space、Work lifecycle、privacy 和去重。

第一版共享 Work 对 Space 成员可见，因此只有允许在该 Space 共享的 Event 才能直接进入 Work。受限 Event 必须由有权成员明确形成一条经过删减的新 Work Message。

只有第一条生产 Event 来源出现后，才创建独立 `event` owner。在此之前由具体 Connector 保存原生事件证据。

## 11. Action：新的外部后果

Action 表示 Carry 请求改变外部系统，例如：

- 创建、修改或删除远端数据；
- 退款、付款或权限变化；
- 主动向新受众发送；
- 调用具有写入效果的 Plugin tool。

普通同会话回复不是 Action。

具体 Connector 或 Plugin 保存不可变 typed command。Action 只引用 command binding 和 digest，不保存开放 command JSON。

Proposal 固定：

- originating Work 和 Run；
- command digest；
- 准确目标与参数；
- Connector 或 Plugin installation；
- credential binding；
- 所需授权等级。

生命周期保持最小：

```text
Proposed
Authorized / Declined
Submitting
Succeeded / Failed / Unknown
CancelledBeforeSubmit
```

提交前重新验证 Work、成员权限、installation、credential 和目标。网络调用发生在 submit claim 事务之后。

Unknown 只能通过只读证据或已经验证的幂等协议解决。Operator 不能无证据地改成 Failed，也不能直接重放原 Action。

授权后的 Action 由独立 worker 执行，不依赖提出它的 Agent Session。Pi 和 Codex 不获得批准后的长期 credential。

没有真实写入路径时不创建空 `action` package；第一条 Plugin 或 Connector 写操作出现时再建立它。

## 12. Plugin

Plugin 实现 Agent Plugins 1.0 客户端合同。

安装记录只拥有：

- Space；
- manifest identity 和规范版本；
- 固定 package digest；
- enable/disable；
- permission grant；
- credential binding；
- 独立 PLUGIN_DATA；
- update 与 revocation history。

Plugin package 只读、固定 digest、禁止路径逃逸且不自动更新。第一版不建立 marketplace。

### 12.1 Skills

Skills 是同时提供给 Pi 和 Codex 的只读内容。文本不能产生权限。

### 12.2 MCP

第一条真实 Plugin journey 只选择一种 transport：

- 如果选择 Remote MCP，服务端负责 origin、timeout、rate limit、认证和 policy mediation；
- 如果选择 stdio MCP，Host 在独立进程或 sandbox 中运行它。

另一种 transport 只有出现具体消费者后才加入，不能为了规范完整性预建。

Agent 不直接获得长期 credential。每次 credential-bearing 调用都验证当前 Run、Work、Plugin permission 和 binding。第一版不把长期 credential 作为环境变量或文件交给 stdio Plugin；只能这样认证的 Plugin 暂不获得该凭据。

### 12.3 Tool policy

MCP annotation 只是提示：

- 只有被 Carry 明确验证的只读 tool 可以直接执行；
- 其他 credential-bearing tool 默认形成 Action；
- Action 固定 Plugin digest、tool、参数、目标和 credential binding；
- 响应丢失时进入 Unknown。

Pi 和 Codex 使用同一个 Plugin fixture 验证安装、Skill、MCP、Action gating 和 credential 隔离。

## 13. Artifact

本节是第一份必须长期保存的 bytes 的 promotion contract。没有 Plugin package、附件或 Result bytes 消费者时，不创建 Artifact owner 或 Object Storage。

Artifact 是 Space 内长期可引用的不可变内容，保存：

- object key；
- digest、size 和 content type；
- provenance；
- security classification；
- retention。

Conversation Message、Work Message、Result 和 Run 可以引用 Artifact。

Connector 临时下载 URL 不进入长期 Work 或 Agent prompt。需要长期使用的附件复制到 Carry 管理的 Object Storage 并验证 digest。

不建立通用 Resource、Binding 或 polymorphic attachment package。

## 14. 权限和凭据

Carry 只承认来自真实关系的权限：

- 经过认证的 Identity；
- 当前 Space Membership；
- 当前 Work authority；
- 当前有效的 Connector、Plugin 或 credential binding；
- 当前 Run attempt fence。

消息、文件、网页、Webhook、Plugin manifest、MCP annotation 和模型输出都不能产生权限。

第一版只需要几类明确的失效标记：

- Membership version；
- Work authority version；
- writer token；
- Run attempt fence；
- Endpoint、Plugin 和 credential generation。

每个提交只检查自己实际依赖的标记，不创建全局 generation 系统。

### 三种协议身份

User、Host 和 Agent 使用不同身份：

- User（Web 或 Carry CLI）：Session/token、CSRF（浏览器）、Membership、idempotency key；
- Host：mTLS、Machine enrollment、Run claim/renew/finish；
- Agent：绑定准确 attempt 的短期 bearer credential。

三种 credential 不能互换。

## 15. 必须原子的事务

| 用例 | 同一事务中完成 |
| --- | --- |
| 创建 Work | 成员权限、目标、首任负责人、来源幂等、首个输入 |
| 记录 Work 输入 | 强类型事实、连续 `input_seq`、真实 actor |
| 创建协调 Run | 扫描未应用输入、Work lifecycle、输入范围、base revision、唯一 coordinator |
| 提交 Conversation reply | Run/fence、唯一 accepted reply、Message、有效 EndpointLink 的 Delivery outbox |
| 提交当前理解 | Work authority、writer、base revision、新理解、`applied_input_seq` |
| 转交负责人 | proposal winner、旧任期结束、新任期开始、authority rotation |
| Pause/Close/Reopen | lifecycle、authority rotation、writer/Run/Timer fence |
| Run claim | Machine permission、唯一 attempt、lease、fence、短期 credential |
| 接收渠道消息 | native 去重、actor、Membership、EndpointLink、唯一最终归属 |
| 创建 Delivery | 已有 Message、EndpointLink generation、pending outbox |
| Action 决定 | typed command digest、当前权限、唯一 decision |
| Action submit claim | 当前授权、credential、唯一 submit winner、fence |
| Plugin update | 新 digest、权限差异、新 generation、旧 Run 固定旧版本 |

所有网络调用都在事务提交后发生，再通过带 fence 的事务记录结果。

## 16. 数据设计

### 强类型物理事实

权限、负责人任期、lease、Timer、Result review、Delivery、Action 和 provider receipt 使用明确列与约束。

开放自然语言可以保存为文本，但 JSON 不能代替：

- authority；
- lifecycle；
- time；
- idempotency；
- causality；
- external outcome。

### 不采用 Event Sourcing

Carry 保存必要历史和规范化当前状态，不通过重放万能事件流恢复整个产品。

### 删除保持真实

撤销权限会停止未来访问，但不会抹掉已经发生的外部操作。

私人 Conversation、Work、Connector raw payload、Artifact、credential audit、Delivery 和 Action 使用不同保留策略。具体期限与恢复流程暂不在架构中猜测，必须在首次生产部署前由独立运维设计冻结。

## 17. 代码边界

目标结构只列已经赚得的边界：

```text
cmd/
  carry/              # 成员 CLI + `carry host` 运行模式
  carry-server/

apps/
  web/

internal/
  identity/
  space/
  conversation/
  work/
  run/
  delivery/
  plugin/
  artifact/             # 第一份长期 bytes 出现时创建
  host/
  agent/
    pi/
    codex/
  connector/
    lark/
    slack/
  postgres/
  server/
```

`action/` 在第一条真实外部写操作出现时创建。`event/` 在第一条生产 Event 来源出现时创建。未启用 gVisor 时不创建 sandbox 占位目录。

禁止按对称性预建：

```text
common
utils
integration
platform
provider
registry
resource
tool
runtime
orchestrator
readmodel
workview
```

### 依赖方向

- Domain owner 不导入 HTTP、PostgreSQL、Pi、Codex 或 provider client；
- adapter 实现消费者定义的窄接口；
- PostgreSQL 实现完整 use case transaction，不暴露通用 CRUD repository；
- `cmd/carry-server` 和 `cmd/carry` 显式组合具体实现；
- package 不从全局变量发现 store、credential、Connector 或 Runtime。

## 18. 第一版纵向验证

架构必须通过真实旅程证明。

### 不请求 repository capability 的 Attempt

成员创建 Work，Carry 记录新输入、产生 Result、等待 Timer，并在 Pi 与 Codex 之间切换执行。这个旅程中的 Attempt 不请求 repository capability；该事实不进入 Work identity、schema、admission、continuity 或 lifecycle。

### 私人消息创建 Work

成员在私人 Conversation 中明确委托。系统创建共享 Work，但 Work 读者无法读取私人原文。

### Lark 与 Slack

相同 Work 可以从 Lark 或 Slack 接收消息并回复。重复 callback 不产生重复 Message，投递响应丢失进入 Unknown。

### Plugin

相同固定 Plugin 同时供 Pi 与 Codex 使用。只读 tool 返回证据，写入 tool 形成 Action，长期 credential 不进入 Agent 或 stdio Plugin 环境。

### Host 恢复

Host 在执行中断线。系统保证只有一个有效 attempt，旧 attempt 的晚到提交被拒绝，新 attempt 从持久事实继续。

## 19. 必须通过的正确性测试

- 并发输入获得连续且不重复的 `input_seq`；
- 同一 Work 只有一个 active coordinator 和 writer；
- stale revision、writer token 和 attempt fence 被拒绝；
- 旧 Run 不会把运行期间的新输入标成已应用；
- active coordinator 期间到达的输入会触发后续 coordinator 并最终完成；
- Pause、Close、负责人转交和权限撤销会 fence 旧执行；
- Pi 与 Codex 通过同一 Work 和 Plugin conformance suite；
- provider Session 不会跨 Runtime 伪恢复；
- recovery 会轮换 attempt fence，分区旧 Host 的晚到提交被拒绝；
- Conversation reply 已提交但 attempt 未结束时，恢复不会产生第二条回复；
- Lark/Slack callback 重放不会复制内容或权限；
- 私人原文不会通过 Work 来源泄漏；
- Delivery 与 Action response loss 保持 Unknown；
- Plugin、消息和模型输出不能获得权限；
- PostgreSQL focused test 使用真实数据库，数据库缺失导致的 skip 不算 pass。

## 20. 明确不采用

第一版不采用：

- 微服务；
- Kafka、Redis queue 或 Temporal；
- Event Sourcing；
- provider registry；
- 通用 webhook payload；
- workflow DSL；
- 持久 Plan、Step、Memory 或思维链；
- 把 Git、repository 或 worktree 放进 Work/Run 核心 schema；
- Plugin marketplace；
- 长期 Agent Session 作为连续性来源；
- 为未发布合同保留兼容分支；
- 为目录美观创建空 package。

## 21. 尚未冻结

以下选择等待第一个真实实现：

- Object Storage provider；
- Plugin reverse-domain extension namespace；
- 第一条生产 Event 来源；
- 第一条高后果 Action；
- gVisor 是否成为默认 Host policy；
- Web 页面、路由和组件结构。

它们不影响本文已经确定的 owner、权限边界和失败语义。

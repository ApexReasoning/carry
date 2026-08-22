# Carry 架构

本文件拥有事实 owner、拓扑、权限、并发和依赖方向。产品语言见 `docs/product.md`；每个节点的文件与证据见 `docs/implementation.md`。

责任由 PostgreSQL 和已认证进程固定，路径由 Agent 自由决定。流程中的一个阶段不因此成为 package、表、类型或 API。表结构、列、索引和基数由赚得它们的节点在研究后冻结，不在本文件预设。

## 1. 拓扑

```text
browser ──member session──► Carry Server ──┐
                                 ▲          │ 记录目标 Agent
                                 │ pull/claim（PostgreSQL 裁决）
                                 │          ▼
                        carry Host ──► Pi / Codex 进程 session
                             ▲
        本机 Agent 接口 ─────┘
```

- Web 是唯一的人类界面。人在机器上的命令只服务于把一台 Host 接入 Space、查看和移除它。
- `carry` 可执行文件同时是长驻 Host、浅层 operator 接入命令和面向 Agent 的接口。Agent 进程通过本机 Host 到达 Server；它不持有成员凭据、Machine 私钥或 Server 地址。
- **Server 不推送。** Conversation、Work 和 Run 记录准确的目标 Agent 身份；拥有该 Agent 的 Host 主动拉取并在 PostgreSQL 里 claim。目标 Agent 不可用是一个显式的、用户可见的状态，永不按名字回退到另一个 Agent。
- Host 补齐当前 Machine、Agent、Run、attempt 与 fence 事实，是 Agent 工作唯一的 Server 调用方。
- Server 不 import 也不启动 Pi 或 Codex。Host 不决定 Membership、Work 持久真相、claim 权威或外部授权。

## 2. 事实 owner

只有七个 owner。

| Owner | 拥有 | 不拥有 |
| --- | --- | --- |
| Identity | User、GitHub/Google/Email 登录方式、浏览器会话、恢复；一次短期 login transaction 可以携带一个语法有效的 invitation UUID 作为不授权的因果续接值，它没有 FK、不会读取或解释邀请 | Space、邀请真相、Agent 或 Machine 权威 |
| Space | Space、显示名与全局唯一 slug、Membership、管理成员/连接 Host 两项窄权限、邀请身份/收件 Email/内容/终态/一次性与撤销、成员生命周期 | 私人对话内容、Work 负责人变更、通用角色系统 |
| Agent | Agent 身份：ID、所属 Space、Space 内唯一归一化名字、确定性头像（新身份的人类 owner 来自浏览器批准该 Host 的成员）、唯一 Host 绑定、Active/Removed、创建时间 | 进程在场、模型目录、Work 真相、发现内容改写身份 |
| Conversation | 成员与一个确定 Agent 的消息及其准确受众、Web 表单形成的 target-Agent 结构化请求、Conversation 固定的 Agent（与可选模型） | provider 续接句柄、共享 Work 权威、Work 创建结果 |
| Work | 责任、人类负责人、Agent 负责人及其交接事实、生命周期、有序计划项、产出项及其与计划项的关联、需要人的事项、未来与周期继续 | 进程 lease、provider 内部状态、Inbox 视图 |
| Machine | 逻辑 Host 身份、浏览器批准的接入与批准成员、证书谱系、已识别 adapter occurrence 的有界完整 present/absent 报告及其数据库时间 | 成员身份、Agent 身份、Work 成功、物理机器唯一性 |
| Run | 一次有界 Agent 执行：目标 Agent、claim、attempt、lease、fence、用户可见输出、因果来源 | Work 关闭、人类接受 |

派生而非存储：Agent 在线由 Active lifecycle、Machine 最新完整报告中的 present 与数据库 freshness 推出，最近活跃由在场与 Run 事实推出，当前 Run 写权仍由 lease/fence 推出，参与中的 Work 由查询推出；Inbox 查询 Work 的需要人事项，以及当前 Agent 负责人 Removed 或不再在场的 Open/Paused Work。owner unavailable 由已有 Work、Agent 与 Machine 事实直接推出，不要求失联 Agent 再写一项，也不获得 owner、表或字段。

Agent 身份与 Agent 进程在场是两件事：Host 掉线只改变 Machine 拥有的在场事实，不改变 Agent 拥有的身份事实。第一版一个逻辑 Machine 明确组合 Pi 与 Codex adapter，它们各自可以发现 default occurrence，也可以在未安装时返回空结果；只有发现的 occurrence 创建身份，后续完整报告遗漏已创建 occurrence 时只令其离线。持久调和键从一开始就是 `(Machine, adapter key, adapter-local occurrence key)`，使后续受信任的 native family 或一个 family 的多个稳定 occurrence 不需要改 schema 或公开报告结构。它不形成用户可见 slot、profile 或 registry。Occurrence key 由具体 adapter 从非模型的本机安装事实得到，不是显示名、persona、provider session 或授权。新 Agent 的人类 owner 只从锁内读取 Machine 已拥有的浏览器批准成员，并同时证明该 Membership 仍 Active；这个 Active 证明只裁决新身份的 owner 分配，批准成员后来离开不妨碍准确 Machine claim 为仍 Active 的既有 Agent 调和在场，但该 Host 不能再创建新 occurrence。报告不接受 Space、owner、Agent ID、名字、头像或 lifecycle。重复发现只更新这一有界组合的完整 present/absent 在场，永不改写已有 Agent 的 owner、名字或 Active/Removed。每份报告带一次性 report ID 和它最后观察到的 PostgreSQL revision；PostgreSQL 锁住 Machine 后裁决准确重放、损坏 identity、stale base 与下一个 revision，并只用数据库时间发布 winner。Unknown 重试同一 ID/base/body；stale 重新观察，不用 Host wall clock 或到达顺序冒充因果。Online 是 `Active + latest complete report says present + database freshness` 的查询；明确 absent 立即离线，Last active 取数据库最近一次确认 present 的时间。

Agent owner 只认可一张不可变的内部 adapter descriptor 表：稳定 key、canonical name base、允许的 occurrence 上限；它不包含 factory、路径、凭据、模型、工具、优先级、fallback 或动态 registration。PostgreSQL 在 Space 锁内按 family 独立分配不可复用 ordinal；该 family 的第一个 Agent 使用 canonical base，之后使用数字后缀。名字和归一化键由 Agent owner 生成，Host 无命名输入，也没有冲突建议协议；Removed 名字保持占用。Agent ID、adapter/occurrence binding、名字、Machine binding 与人类 owner 是不同事实。

复制完整 Machine 本地状态等同复制同一个逻辑 Host 权威：两个物理进程调和到同一组 Agent，最新提交的完整报告拥有当前在场，Carry 不声称物理 clone 检测。丢失本地状态或重装后重新批准会创建新 Machine 和新 Agent，永不按 hostname、显示名、adapter 本地 ID、配置路径或内容合并；旧、新 Host 均保留，旧 Host 由当前 Machine revoke 旅程显式撤销。Machine remote/self revoke 与它绑定的 Active Agent 在同一 PostgreSQL 裁决中转为 Removed；强制 Membership removal 同样先转移目标 Active Agent lifecycle，再撤销 Membership。Node 28/29 仍拥有完整 Work 交接、Run 后果与离场 UX，不延迟 Node 15 引入身份时必须成立的引用/lifecycle 不变量。

`carry setup` 的 public begin/poll/cancel 与浏览器 verification URL 使用 canonical external origin；批准后 Server 返回独立的 canonical Host API origin，安装凭据只把它用于 Machine mTLS。两者可以相同但不得由客户端互相推导；Server 配置分别拥有 `CARRY_EXTERNAL_ORIGIN` 与 `CARRY_HOST_API_ORIGIN`。这不是新的 authority owner，而是把 Browser 与 Machine 两个既有 transport audience 保持准确。

Pi、Codex 和未来 DeepSeek Harness 等是 Host 明确构造的具体进程 adapter。一个有界 adapter set 只负责 construction-time duplicate rejection、完整 observation 与按准确 key/occurrence 查找；它没有运行时注册、任意代码发现、profile、priority 或 fallback，不是 provider/model/runtime registry。可选模型只在具体 Agent 能可靠报告当前取值时呈现，发现失败时使用 adapter 默认值。

provider 侧的会话续接句柄是拥有该 Agent 的 Host 的本地私有状态，持久化在本机。没有公开 Session API，没有通用 Session owner 或表。句柄丢失时用有界的 Carry 侧历史新开一个 provider session：效率下降，Conversation 与 Work 真相不变。

计划项、产出项和需要人的事项是 Work 拥有的事实，不是独立 owner。计划是展示真相：Server 不执行计划项，也不从中推导流程语义。中间产出属于产生它的 Run，用户可见的产出项属于 Work。

没有 `Assignment`、`Coordinate`、参与者 owner、通知 owner、调度 owner、workflow 引擎。

## 3. 两位负责人与多个 Agent

每个 Work 恰好有一名人类负责人和一名 Agent 负责人。人类负责人拥有目标与范围、外部授权、Inbox 回应、验收与关闭；Agent 负责人拥有推进、计划、评论理解与转达、协作者选择与检查、需要人的事项与时间安排。

Agent 负责是**责任**，不是当前写权限。当前写权限由 Run 的 claim、lease 与 fence 决定，可以在协作者之间移动，负责人不变。

一个 Work 可以有一个或多个并发或顺序的 Agent session。"至少两个 Agent"是 Agent 负责人在有用且可用时的行为选择，不是数据库不变量：只有一个 Agent 的 Work 必须完整成立。协作者不改变任何一位负责人。

已定的权威要求（表示由节点研究赚得）：

- 任何时刻 Work 的持久真相只有一个被授权的当前写者；其他并发执行只能写自己的状态和输出；
- 每一次协作请求都可以追到发起它的执行与成员，因果不可丢失；
- 一次执行获得的权限不超过发起它的执行，也不超出 Work 边界；
- 一个 Work 的并发度与扩散深度必须有上限，上限由赚得它的节点用真实旅程确定；
- winner 由 PostgreSQL 决定，不由进程内锁决定。

描述因果位置时可以说来源与后继；它们不得成为类型、package、角色或产品词汇，也不预先冻结为某种列或深度模型。

移除 Host 或 Agent 不自动改写 Agent 负责人：Work 保持 Open，并派生为 owner unavailable。只有当前人类负责人能把 agent ownership 转给同一 Space 中 Active 且提交当时可用的 Agent。

Work 的人类负责人变更与 Agent 的人类 owner 变更是两条不同权限。通常只有当前 owner 能把自己的事实转给同一 Space 的 Active 成员；Host 接入者、Agent 内容和成员移除者都不能为第三方任意选择 successor。自愿离场必须先清空本人负责的 Open/Paused Work 与 Active Agent。

强制移除是唯一例外，但没有 successor 选择：持有成员管理权限的执行者在同一事务里承接目标的全部 Open/Paused Work，目标的 Active Agent 同时转为 Removed，然后 Membership 才撤销。承接原因是 Work 拥有的负责人变更事实，并派生进入执行者 Inbox。执行者承担自己发起移除造成的已有责任，不获得替别人分派未来责任的通用权限。

Agent 负责人转移是 Work 的权威状态变化，不是内容建议、Run 协作或 Agent 自治行为。事务必须锁住 Work 并校验当前人类负责人、预期旧 Agent、Work version、替代 Agent 的 Space 与 Active 状态，并校验提交当时数据库可见的在场事实；随后记录准确的旧/新负责人和授权成员，使旧 Agent 的活动 Run 与未来继续失去继续权，再让新 Agent 获得未来责任。这个在场检查只证明提交当时可用，不承诺提交后的存活。并发转移只有一个 winner；旧 Agent 恢复后的写入由当前 owner、version、lease 与 fence 拒绝。Host/Agent 移除权限不携带 Work owner 权限。

交接只携带 Work 的持久事实：目标、评论、计划、产出、参与活动、未解决问题与 Unknown 外部动作。它不复制私人 Conversation、provider session、完整 tool trace 或模型思维。旧 Agent 保留为历史参与者；替代 Agent 随后掉线时，Work 回到同一个 owner unavailable 查询。替代 Agent 可用并 claim 后才启动新的有界 Run、写用户可见的接手摘要；摘要不属于转移事务。准确的表、列和事务语句由离场节点研究冻结。

产出验收与 Work 生命周期也由 Work 拥有。只有当前人类负责人能验收准确 output version，或把 Work 在 Open、Paused、Closed 之间显式转移。验收不关闭 Work；后续 output version 不继承旧验收。暂停和关闭使新的 claim、旧 lease/fence 提交与未来继续失去权威，但不改变已记录的 Unknown；恢复和重开只从当前持久事实创建新的执行机会，不复活旧 Run、批准、schedule 或 provider session。PostgreSQL 锁住 Work 与 expected version 裁决并发 winner。

## 4. 权限

### PostgreSQL 拥有

唯一身份与成员关系；Membership 的两项窄权限及 Space 未结束时各自至少一名 Active 持有人；Space slug 的全局唯一；邀请码的一次性消费与撤销；Agent 名字在 Space 内唯一；Agent 人类 owner 的当前 Membership 有效性；成员资格撤销与两条 Agent-authenticated Work 创建（Web 请求、local create）之间的串行化；数据库时间与顺序；Work version、两位当前负责人、负责人转移的 single winner 与当前写者；强制成员移除承接的幂等；Run 的 claim、attempt 谱系、lease、fence 与迟到写入拒绝；协作请求的幂等与扇出上限；投递提交幂等；未来继续的唯一 winner 与周期推进；已记录的外部结果（含 Unknown）。

应用代码拥有校验和事务形状。内存状态可以缓存，永不授予权限。

### 两类授权来源、三条入口不混合权限

- **成员在 Web**：对话与 `Create with <Agent>` 表单是两条产品入口，但成员只创建由 Conversation owner 持久化的准确 target-Agent 请求，不创建 Work。目标 Agent 的 Host 拉取并 claim；最终 Work 创建事务锁读请求成员的 Membership，并同时校验请求准确目标、当前 Agent/Run/attempt/lease/fence，并从这些权威事实推导两位负责人。表单选择和正文不能改写它们；
- **本机已注册 Agent**：没有成员请求；授权来自 Host 接入时获得的、限于那一个 Space 的常驻 create-only 权限。创建事务锁读并证明该 Agent 的人类 owner 当前仍是同一 Space 的成员；它不能列出、读取或修改任何已存在的 Work，除非拿到准确的当前 Work 上下文；
- 因为权限来源不同，成员请求后的 Agent 创建与本机 Agent 创建使用**不同的 Agent-authenticated transport handler 与不同的 Work 事务**，不合并成一个带分支的入口。成员 transport 只能接纳请求，Server 不推送、不替 Agent 接下。移除 Host 或 Agent 只撤销未来权限。

不存在不经 Agent 的 Work 创建路径。请求本身不是 Work；进程丢失后仍保留准确目标，响应丢失通过同一请求重放/查询恢复，PostgreSQL 保证同一请求最多创建一份 Work。

### Agent 写入按上下文使用两条权威链

**已有 Work 上下文的写入，以及 Agent 接下 Web request 后创建 Work**同时需要：已认证的当前 Machine 证书；归属到唯一 Active Agent；当前 Run 与 attempt；当前 lease 与 fence；该操作由准确请求或 Work 上下文允许；受众不越出边界。

**没有 Work 的本机 create-only 路径**没有 Run/attempt/lease/fence。它需要：已认证的当前 Machine 证书；归属到唯一 Active Agent；Host 接入授予的当前 create-only 权限；该 Agent 的人类 owner Membership 在同一事务锁读后仍有效。它不能读取或修改既有 Work。本机凭证的形状与传输由节点 18 的研究冻结。

Agent 提供的内容永不携带身份或权限事实：Host 与 Server 已经拥有的 Agent、Machine、Run、Work、成员身份不接受由调用方内容改写。

### Unknown

发出有后果的请求后超时，在 provider 支持的幂等查询证明之前是 Unknown。不从意图推断成功，不从沉默推断失败。

### 内容

Agent 与外部内容只能在当前 Work 合同允许的范围内提议改变；具体可提议事实由赚得它们的节点冻结，不在这里形成操作枚举。Server transport 只做认证、解码与 wire-shape 校验；Work/Run owner 的 PostgreSQL 事务校验受众、当前 Run 权威和 expected version。内容永不授予 Membership、受众、凭据或外部权限。

## 5. 时间与投递

数据库时间拥有 `created_at`、顺序、lease 过期、一次性状态转移、未来继续与周期推进。未来继续与周期是 Work 拥有的事实，由 Agent 负责人设定；没有 Timer package、调度器 owner 或 workflow 图。

邮件和飞书是具体 outbound adapter。渠道连接后投递自动发生，不对每条事项再次批准。每次提交记录准确的事项版本、收件人、载荷 digest、幂等键、provider 引用和 Succeeded/Failed/Unknown。投递状态永不修改底层事实，也永不改变 Inbox。

## 6. 恢复（已定部分）

- Host 意外掉线：Agent 身份保留，在场事实失效，进行中的 Run 在 lease 失效后失去权威，Work 保持 Open 并显示 owner unavailable；恢复前不自动换 Agent；
- Host 或 Agent 被撤销：撤销立即阻止新认证和新 claim，并使旧执行失去继续权，不等待交接完成；被撤销 Host 绑定的 Agent 与被单独移除的 Agent 转为 Removed，但 Agent 身份与 Work 历史保留；
- Agent 负责人转移：只由 Work 当前人类负责人发起，目标必须是同一 Space 中 Active 且提交当时可用的 Agent；旧执行失去 Server 提交权，未来继续暂停；替代 Agent 可用并成功 claim 后才从持久 Work 事实启动新 Run，旧 Agent 回来后不能继续写；
- 多 Work 影响：Host/Agent 移除页只列出受影响 Work，不改变任何 Work owner；各人类负责人从自己的 Work 页面逐份转移，互不共享权限或事务；
- 成员自愿离场：本人逐份转移或关闭 Open/Paused Work，并转移或移除本人拥有的 Active Agent；未清空时撤销失败并列出剩余项；
- 成员被强制移除：持有成员管理权限的执行者与目标 Membership 串行化；同一事务把目标 Active Agent 置为 Removed、把目标 Open/Paused Work 人类负责人改为执行者并记录原因；目标若是连接 Host 权限的最后持有人，该项由执行者承接；随后才撤销 Membership。并发 Web-request 或本机创建要么先提交并被承接，要么在锁后因 prospective human owner Membership 失效而被明确拒绝；
- 窄权限最后持有人：Space 未结束时管理成员与连接 Host 各至少保留一名 Active 持有人；自愿离场先把最后持有项授予另一名 Active 成员，强制移除只按上一条确定性承接，不提供第三方选择器；
- Space 结束：所有 Open/Paused Work 必须先由各自人类负责人关闭；随后在准确展示 Host、Agent、渠道、Unknown 与保留后果后，撤销所有未来 Membership、Machine/Agent 执行与 continuation 权威，不改变既有 Work lifecycle；历史保留与读取边界由节点 30 研究冻结，永不从本地撤销推断远端擦除；
- provider 会话句柄丢失：用有界 Carry 历史新开 session，不丢 Conversation 与 Work 真相；
- 响应丢失：记为 Unknown，不自动重放，交接也不把 Unknown 当作可重试；
- 迟到写入：由当前 owner、fence 与 version 在 SQL 中被拒绝。

## 7. 依赖方向与 package 标准

下一次遇到新的 owner 行为时，按同一个决定落位：

1. owner package 拥有产品词汇、恢复错误、纯归一化与 digest 规则、command 和 result；
2. PostgreSQL adapter 实现所有依赖数据库事实的锁、数据库时间、重放、当前权限、winner 与状态转移；事实 ownership 不要求把物理事务搬进 owner package；
3. interface 放在实际消费方，只列这个消费方当前调用的最小方法集；它描述消费方需要的 capability，不是由其具体 PostgreSQL 实现决定的“数据库接口”；
4. 只有一个行为需要组合多个 capability 或一次外部后果时，才建立 stateful owner behavior；
5. 不为了让 owner 看起来拥有行为而保留纯转发方法。删除转发不得把 owner 规则推入 server 或 PostgreSQL；纯规则仍由 owner 的函数、构造或非转发行为拥有；Space 创建因此由 Space 构造 canonical command、Server 消费一个最小创建 capability、PostgreSQL 裁决重放、slug winner 与原子初始 Membership，不保留只持有该 capability 的 `space.Creator`；
6. 一个 owner 只有在当前规则需要时，才直接消费另一个 owner 原样导出的准确 fact、type、error 或纯规则；不得包一层改名、复制事实、取得对方权限或形成 import cycle。Go acyclicity 与 review 约束这种边，不维护 owner-pair 中央 allowlist；脚本只表达当前真实且稳定的 adapter 禁止方向。

```text
cmd/carry-server → internal/server → owner packages → internal/postgres
cmd/carry        → internal/cli    → internal/host
internal/host    → internal/host/<concrete-adapter>（V1 为 pi 与 codex），以及 Carry Server 的 Host API
apps/web         → protocol/user/v1
```

- `internal/agent` 是 Agent 身份 owner；具体进程 adapter 从 `internal/agent/pi|codex` 迁到 `internal/host/pi|codex`，后续 family 各有准确 concrete package，只有 Host composition root 可以显式构造；不使用 `init()`、反射、插件发现或动态 registry；
- 一个 package 只为一个事实 owner、一个具体进程 adapter，或一个明确命名的 composition/transport 边界存在；`cmd`、`server`、`postgres`、`cli`、`host`、`e2e` 这类边界不得拥有产品策略；
- owner package 不 import server、postgres、host、具体 adapter 或 Web；
- `carry-server` 不 import Pi 或 Codex；
- 面向 Agent 的接口不调用 User API，也不读成员凭据文件；
- transport 校验线格式并转交，不重新拥有策略；postgres adapter 不发明产品默认值；生成代码保持生成；
- 文件按一个 owner 行为命名：server 下是 transport 或 worker，postgres 下镜像 owner 行为，Web 按产品概念组织；没有当前 owner 行为就没有 `service.go`、`manager.go`、`utils.go`、`common.go`；
- 边界检查只表达少数稳定的禁止方向，不维护中央 package allowlist。

## 8. 没有新批准旅程就禁止

`Assignment`、`Coordinate`、`Plan`/`Step`/`Artifact` owner、通用 `Effect`/`Action`/`Capability`、Session owner、参与者 owner、通知 owner、调度 owner、workflow 引擎、动态 provider/model/runtime registry 或产品目录；Server 主动推送执行；通用依赖工厂或 provider-specific 参数袋；`common`、`utils`、`helpers`、`platform`、`integration`、`manager`、`resource`、`runtime`、`readmodel` package；Event Sourcing、CRDT、Kafka、Temporal、微服务；没有任何消费者收到过的路径的兼容 API。

## 9. 架构改动的证据

针对被改路径的直接测试：完整 happy path；无效身份、受众或成员关系；重复与重放；并发 winner；lease 过期与迟到 fence；目标 Agent 不可用；Host 或 Agent 丢失与恢复；响应丢失与 Unknown；两位负责人在界面上都成立；被替代的 schema、API、CLI、UI 已删除。旧合同的测试全绿不是证据。

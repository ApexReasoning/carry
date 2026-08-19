# Carry 架构设计

## 1. 唯一目标

Carry 的架构只保证一件事：团队交给 Carry 的 Work，不会因为一次对话、一个 Agent、一个进程或一台机器结束而丢失。

第一版使用一个模块化单体、一个独立执行 Host 和 PostgreSQL：

```text
Web / carry CLI
       │ User identity
       ▼
┌──────────────────────┐
│     carry-server     │
│ User API / Host API  │
└──────────┬───────────┘
           │
      PostgreSQL
           ▲
           │ Machine mTLS
┌──────────┴───────────┐
│     carry host       │
│ one local executor   │
│ Pi or Codex adapter  │
└──────────────────────┘
```

PostgreSQL 拥有事务、唯一 winner、lease、fence、幂等和恢复裁决。第一版不引入 Kafka、Redis queue、Temporal、微服务或 Object Storage。

## 2. 架构哲学：可信内核，自由边缘

`docs/product.md` 定义的“克制与自由”在架构上不是平均分配复杂度，而是把不同事实放在不同强度的边界中。

### 2.1 内核克制

越接近团队事实和真实后果，表示必须越窄、越明确：

- owner 唯一，持久事实不在多层复制；
- identity、authority、causality、time、sequence、fence、idempotency 和 outcome 强类型；
- 并发决定由 PostgreSQL transaction、constraint 和 conditional write 裁决；
- public protocol 只包含真实消费者需要的承诺；
- privacy 与 external consequence 默认 fail closed；
- 没有独立生命周期和权限边界的角色不获得表、API、package 或 identity。

这里的克制不是机械减少行数。删除一项约束如果会让事实可伪造、authority 可竞争或 Unknown 被猜测，就是破坏架构；必要复杂度必须留在能够证明它的 owner 中。

架构可以机械证明系统拥有的 identity、authority、causality、time、sequence 和 external outcome，不能机械证明开放自然语言的全部语义真伪。Work 当前理解因此是 Carry 产生并明确归属于 Carry 的可见、可纠正解释；结构验证和 fenced commit 只能证明它由谁在什么 authority 下写入，不能把其中内容自动认证为外部事实。这个边界不能通过 Evidence entity、通用 semantic validator 或模型自我声明替代。

### 2.2 边缘自由

越接近目标、自然语言、推理和具体执行方法，架构越少预设：

- Work core 不按领域、Git、内容、provider、model、Host 或 Runtime 分类；
- 开放内容保持开放，不用核心 enum 模拟人的全部表达；
- concrete adapter 保留 native protocol、终态和资源管理；
- capability 只绑定获得它的准确 Attempt，不反向改变 Work identity；
- 尚未发布的内部路径可以直接删除和重建，不维护假兼容；
- 未来 owner 从真实旅程 promotion，不从预想的完整平台向下填空。

这里的自由不是绕过核心。任何路径一旦要读取私人事实、提交团队事实或产生外部后果，就必须重新进入当前 authority owner 的窄入口。

### 2.3 决策分界

架构评审先判断一个变化位于哪一侧：

| 问题 | 默认设计 |
| --- | --- |
| 谁、对什么、何时有权写入？ | 克制：准确 owner、credential、transaction、fence |
| 什么已经发生，结果是否确定？ | 克制：typed fact、constraint、Success/Failed/Unknown |
| 用户可以委托什么？ | 自由：自然语言 Work，不建领域分类 |
| Carry 可以怎样完成？ | 自由：边界内选择方法，concrete adapter 原生实现 |
| 未来是否可能有第二种能力？ | 自由：暂不冻结共同抽象 |
| 当前消费者是否需要稳定协议？ | 克制：只发布最小真实合同 |

新增约束必须指出它保护的当前不变量或用户伤害；新增自由必须证明它没有越过 authority、privacy 和 consequence。不能回答其中一项的设计不进入核心。

## 3. 概念准入

新增名词、表、状态、接口、API audience 或 package 前必须同时回答：

1. 当前哪条用户旅程需要它？
2. 它拥有哪个独立持久事实？
3. 它是否有自己的生命周期？
4. 它保护相邻 owner 不能表达的权限或并发不变量吗？
5. 删除后用户具体失去什么？

“这份事实可以在某个场景作为依据”“以后可能有第二种实现”“字段放在一起不够对称”都不是新边界。

尤其禁止把角色提升成实体：Evidence、Contribution、Completion、Observation、Draft 等词只有在独立生命周期真正出现时才获得 identity。Artifact 是 bytes；Artifact 被用于支持判断时仍然只是 Artifact。

## 4. 当前事实 owner

当前只有这些 owner：

| Owner | 持久事实 | 当前消费者 |
| --- | --- | --- |
| Identity | 成员认证、User token、Browser Session | User API、Web、CLI |
| Space | Membership、Machine enrollment 权限 | User API、Host enrollment |
| Work | 目标、负责人、消息、当前理解 | 成员、执行路径 |
| Machine | 独立执行身份、证书、撤销 | Host API |
| Run | 一次固定 Work 推进及其 Attempts | Host API、Host worker |

以下不是 owner：

- Carry：产品同事，不是数据库实体；
- Host：运行进程，不保存团队事实；
- Pi/Codex Runtime：具体 adapter，不进入 Work 或 Run identity；
- Agent：原生 subprocess，不是服务端 principal；
- Evidence：既有事实在某次判断中的角色；
- Coordinator：当前只有一种 Work Run，不需要 subtype；
- Result、Question、Timer、Event、Delivery、Plugin、Artifact：等第一条无法由当前 owner 表达的真实旅程再 promotion。

## 5. Identity、Space 与 Machine

### 5.1 User

成员 CLI 使用 User token。Web 用 User token 换取短期 HttpOnly Browser Session，避免 JavaScript 保存长期 bearer。

User token 和 Browser Session 是两种 credential 表示，但都映射到同一成员身份。它们不能作为 Machine credential。

### 5.2 Machine

`carry host enroll` 由已登录成员发起。服务端在同一事务中验证 Membership 与 enrollment 权限，并签发独立 Machine certificate。

此后 `carry host start` 只使用 Machine mTLS。Machine 被撤销后不能 claim、renew、commit 或 finish。

Machine 只保存 durable identity、Space、显示名、证书 serial、enrollment 与 revocation。服务端不保存 Runtime report、binary path、版本、availability 或 Machine status projection。

### 5.3 Host 与本地 executor

Host 启动时在本地只读 Diagnose Pi 和 Codex，并选择一个可用的 concrete executor。选择发生在 claim 前并在 worker 生命周期内保持稳定；claim 后不切换 provider 或自动 fallback。

普通成员不选择 executor。服务端也不按 provider、Runtime、model 或 Work 类型路由。

Pi 与 Codex 保持独立 adapter：

```text
Diagnose(ctx) error
Execute(ctx, immutable Work context) (UnderstandingUpdate, error)
```

取消由 `context` 表达。进程、RPC/app-server、临时目录、协议终态和 cleanup 由具体 adapter 拥有。共同合同不包含 Resume、Discard、Session identity、provider event 或 checkpoint。

## 6. Work

Work 是连续性的事实源。它保存：

- Space；
- 一句话目标；
- Open 生命周期；
- 当前负责人和创建者；
- 按顺序追加的 Work Messages；
- 当前 understanding 与 next step；
- 内部输入进度和 understanding version；
- 创建幂等事实。

内部进度字段用于准确区分“消息已记录”和“已经反映到当前理解”，但不进入 User API。

### 6.1 输入

目标是 Work 的稳定字段。第一轮执行读取目标，但不再把目标复制成一条 tagged Agent input。

Work Messages 按准确顺序追加。Run 固定本次要应用到哪里的输入上限；执行上下文只包含：

- goal；
- previous understanding 与 next step；
- 本次新增消息，保持顺序和真实作者。

模型不需要 input sequence、base version、Run ID、Attempt ID、fence 或 Machine identity。这些物理字段由 Host 保留并只用于提交。

### 6.2 当前理解

当前 understanding 和 next step 直接属于 Work。第一版不保存独立 understanding revision rows，因为没有历史浏览、回滚或审计消费者。

Work 保留内部 `understanding_version` 作为 CAS，不把 version 提升成用户可见对象。

Work 查询还可以派生 `needs_retry`：存在一个尚未被成员明确允许重新推进的 terminal Failed/Unknown Run。它不是新的 Work lifecycle，也不公开 terminal outcome、Run identity 或 retry generation。

提交时 PostgreSQL 原子检查：

- Work 仍可执行；
- Run 仍是当前 unresolved Run；
- Attempt、Machine、lease 与 fence 仍有效；
- base understanding version 没有变化；
- 提交覆盖固定输入上限。

成功后同一事务更新 Work 当前理解、推进 applied input、终结 Attempt 与 Run。

## 7. Run 与 Attempt

Run 是一次固定的 Work 推进。Attempt 是这次推进的一次物理执行。

这个区别保留，因为它们拥有不同生命周期：Run 可以在 Host 失败后继续；旧 Attempt 必须永久失去 authority。

### 7.1 没有 coordinator queue

第一版没有后台 coordinator、pending Run 或 coordinator subtype。

Machine claim 在一个 PostgreSQL 事务中：

1. 锁定并验证 Machine；
2. 优先寻找可恢复的 expired active Run，或者寻找有未应用输入且没有 unresolved Run 的 Open Work；
3. 必要时创建 Run，固定 Work、输入上限和 base understanding version；
4. 增加 fence 并创建唯一 active Attempt；
5. 设置数据库时间 lease；
6. 返回不可变执行 descriptor。

如果没有可领取 Work，返回 empty claim。服务端不需要每秒扫描 Work，也不需要独立 goroutine 预先制造 pending rows。

### 7.2 Claim descriptor

Claim 只包含当前 Host 真正需要的字段：

- Run、Attempt、Work identity；
- fence 与 lease expiry；
- goal；
- previous understanding 与 next step；
- fixed new messages；
- base understanding version 与 input upper bound。

它不包含 writer token、Agent credential、provider、Runtime、model、Session、repository 或 future capability fields。

### 7.3 Authority

当前执行 authority 由以下事实共同组成：

```text
Machine mTLS
+ exact Run ID
+ exact Attempt ID
+ current fence
+ unexpired lease
+ Work base version/input range
```

没有第二个 writer token。Host 已经持有 Machine identity，原生 Agent subprocess 当前也不直接调用 carry-server，因此不签发没有消费者的 Agent bearer credential，也不发布 Agent API audience。

将来只有原生 Agent 或本地 bridge 需要直接调用服务端能力时，才引入 Attempt-scoped credential。那时 credential 必须绑定准确 Attempt/fence/capability，并与 Machine mTLS 分离。

### 7.4 Host API

Host API 使用 Machine mTLS，只提供当前 worker 需要的行为：

```text
Claim
Renew
CommitUnderstanding
FinishFailedOrUnknown
```

Claim 直接返回执行上下文，不再增加独立 LoadContext round trip。

每个 mutation 都重新验证 Machine、Run、Attempt、fence 和 lease。协议 response 不泄漏 provider state。

### 7.5 失败与恢复

lease 过期只撤销旧 Attempt 的提交权，不证明旧进程停止或失败。

下一次 claim 可以原子地：

- 锁定 expired active Run；
- 把旧 Attempt 标记 expired；
- 增加 fence；
- 创建新 Attempt 和 lease；
- 返回同一个持久 Work context。

新 Attempt 总是从 Work fresh Execute。旧 Host 的 renew、commit 和 finish 都被拒绝。

明确记录为 Failed 或 Unknown 的 Run 不自动恢复。Run 保存成员是否已经明确请求 fresh retry 的时间、成员和幂等 identity；这是一条 Run causality fact，不是新的 owner。只有 active Space member 通过 Work 的 `Try again` 明确请求后，后续 claim 才能创建新的 Run。旧 Run 和 Attempt 保持 terminal，旧 authority 不复活。

当前无 tool、Action 或外部后果的 native execution 可以安全重新推理；因此核心不保存 opaque completion evidence，不上传 native Session locator，也不定义 Resume/Discard。如果未来模型成本或时延证明原生终态重取有产品价值，优先让同一 Machine 的 concrete adapter 用本地、Run-keyed 状态自然优化；只有跨 Machine 持久化成为真实需求时才重新设计服务端合同。

## 8. Native Agent adapters

Pi adapter 直接使用 Pi documented RPC；Codex adapter 直接使用 Codex app-server。两者通过同一产品 conformance suite，但不共享虚构 provider protocol。

执行输入使用固定 instruction 加不可信 JSON context。Prompt 不包含 credential、fence、Machine identity 或权限声明。

共同输出只有：

```text
understanding
next_step
```

模型输出先经过严格结构验证，再由当前 Host 使用 fenced Commit 写入 PostgreSQL。Agent settled、thread idle、进程退出或一段看似正确的文本都不能单独修改 Work。

Codex 缓冲结束但缺少准确 terminal notification 时，只可有界只读核对；不能证明完成则 Finish Unknown。Host 不在 claim 后切换到另一 adapter。

## 9. User API

User API 只表达成员旅程：

- 建立/撤销 Browser Session；
- 读取当前成员与 Spaces；
- 创建、列出、读取 Work；
- 追加 Work Message；
- 对需要成员选择的 Work 显式请求重新推进；
- enrollment/revocation Machine。

User Work response 包含：

- Work ID、Space、goal、lifecycle；
- owner、creator；
- current understanding 与 next step；
- 是否仍有新信息待 Carry 应用；
- 是否需要成员显式选择重新推进；
- created time。

它不公开 input head、applied input、revision number、Run、Attempt 或 Runtime。

Web 使用 `protocol/user/v1/openapi.yaml` 生成 client。CLI 与 Web 不复制权限规则。

## 10. 必须原子的事务

| 行为 | 同一事务中完成 |
| --- | --- |
| 创建 Work | Membership、目标、首任负责人、幂等身份、初始待应用状态 |
| 追加 Work Message | Membership、Work lock、连续输入顺序、真实作者、幂等 |
| Claim 新 Work | Machine、eligible Work、唯一 unresolved Run、fixed range、Attempt/fence/lease |
| Recover Run | expired Attempt、旧 Attempt 终结、fence rotation、新 Attempt/lease |
| Renew | Machine、exact active Attempt、current fence、unexpired old lease |
| Commit understanding | Machine、Attempt/fence/lease、Work base version/range、Work update、terminal states |
| Finish unresolved | Machine、Attempt/fence/lease、terminal Run/Attempt outcome |
| Request Work retry | Membership、Open Work、唯一未请求 retry 的 terminal Run、成员与幂等 identity |
| Revoke Machine | 成员权限、Machine revocation；后续 Host mutation 由条件更新拒绝 |

所有网络和 native Agent I/O 都在数据库事务外发生。

## 11. 数据形态

当前持久表应收敛为：

```text
carry_users
spaces
space_memberships
user_tokens
browser_sessions
machines
works
work_messages
runs
run_attempts
```

迁移历史可以保留已经进入共享 main 的旧表创建与后续删除；当前 schema、queries、domain 和 API 不保留旧概念的 compatibility path。

不保存：

```text
machine_runtime_observations
work_understanding_revisions
native_completion_evidence
writer_tokens
agent_credentials
provider/runtime/model/session columns
```

自然语言可以是 text；authority、identity、time、sequence、lease、fence、idempotency 和 outcome 必须强类型并由 constraint 保护。

Carry 不使用 Event Sourcing，也不建立开放 JSON operation/evidence/event 表。

## 12. 代码边界

当前结构：

```text
cmd/
  carry/
  carry-server/
apps/
  web/
internal/
  cli/
  identity/
  space/
  work/
  run/
  host/
  agent/
    pi/
    codex/
  postgres/
  server/
protocol/
  user/v1/
```

规则：

- domain owner 不导入 HTTP、PostgreSQL 或 concrete Agent adapter；
- adapter 实现消费者定义的窄接口；
- PostgreSQL 实现完整 use-case transaction，不暴露 CRUD repository；
- `cmd/carry-server` 与 `internal/cli` 显式组合实现；
- 不建立 common、utils、platform、integration、registry、resource、runtime、orchestrator 或 readmodel；
- 删除最后一个消费者时同时删除 package、route、query、generated code、test 和文档。

## 13. Promotion contracts

未来能力只保存最小退出条件，不在当前架构冻结表、API 或 package。

### Conversation

当第一条私人对话 journey 到达时，需要证明私人消息不能通过 Work 来源泄漏，且一次明确委托可以幂等创建 Work。

### Result / Needs You

先尝试用 Work 当前内容和查询表达。只有独立结果确实需要可引用 revision 与 review lifecycle 时才建立 identity。Needs You 始终是查询。

### Future continuation

先实现 Work 的一个明确 `continue_at` 条件。只有多个独立时间约定和 occurrence lifecycle 被证明必要时才建立 Timer identity。

### Channels

第一条渠道 journey 从 provider-native identity、目标 Conversation/Work 和真实 outbound outcome 推导最小事实。不要预建 Connector registry、通用 inbound message 或第二个 provider abstraction。

### Action

第一条真实外部写操作必须有 immutable typed command、真实授权、唯一 submit winner 和 Unknown。它的独立后果生命周期可以赚得 Action identity。

### Artifact

第一份必须长期保存的 bytes 出现后再引入 Artifact 和 Object Storage。Artifact 不因为被作为依据使用而复制成 Evidence。

### Third-party capabilities

第一条 Skill/MCP journey 只选择一种 transport 和一个 fixture。没有真实 credential-bearing consumer 前不建立 marketplace、tool registry 或通用 Plugin runtime。

## 14. 正确性证据

当前架构必须由以下测试证明：

- 并发 Work create/message 保持幂等和连续顺序；
- 并发 Machine claim 对同一 Work 只有一个 winner；
- Run 与 Attempt 在 claim 事务中一起建立，没有 pending Run；
- expired Attempt 不能 renew 复活；
- recovery 增加 fence，旧 Host 不能 commit 或 finish；
- Work 运行期间到达的新消息不会被旧 range 错误标记已应用；
- Failed/Unknown 不自动 replay，active member 的幂等 `Try again` 才允许一个 fresh Run；
- Commit 直接更新 Work 当前理解，不创建 revision row；
- revoked Machine 不能领取或修改执行；
- Pi 与 Codex 分别通过同一 execution conformance；
- User API 不暴露内部 sequence/version/Run/Attempt；
- PostgreSQL focused tests 使用真实隔离数据库，缺失数据库不是 pass。

## 15. 明确不采用

第一版不采用：

- 微服务、Kafka、Redis queue、Temporal；
- background coordinator 或 pending Run queue；
- Agent API、writer token 或无消费者的 Attempt credential；
- native Session evidence、Resume framework 或 provider checkpoint；
- provider/Runtime registry 与 Work routing；
- ACP 作为核心 wire；
- Event Sourcing；
- WorkKind、Plan、Step、Child Run、Contribution、Evidence entity；
- Git/repository/worktree 作为 Work 或 Run identity；
- workflow DSL 和通用 Plugin marketplace；
- 为未发布合同保留 compatibility branch。

## 16. 用克制与自由检查架构

一次改动完成前回答：

1. 从成员动作到 PostgreSQL authority 的路径是否更短？
2. 是否出现两个 credential、状态或表保护同一个决定？
3. 是否把本地物理状态上传成了服务端产品事实？
4. 是否让 public API 传输了只有内部 CAS 需要的字段？
5. 是否能删除一个 goroutine、endpoint、interface、row type 或转换层？
6. 是否为一个尚不存在的第二消费者建立了抽象？
7. 是否无必要地禁止了某个仍在当前 authority 内的合法目标、表达或执行方法？

只有删除后当前用户旅程及其真实、authority、privacy、失败恢复和 external consequence 证据仍然完整，并且合法路径没有被缩窄，才删除。

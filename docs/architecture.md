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
| Conversation | 成员与 Carry 的私人消息、reply claim、private-side Work consequence | 准确成员、受限 Machine claim |
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

Space-enrolled Machine 是该 Space 的受信 Carry 执行基础设施。除了共享 Work，它可以在 exact Conversation reply claim、current fence 和 unexpired database-time lease 下读取生成一条私人回复所需的有界上下文。这个 authority 不提供通用 Conversation list/read，不跨 Space，不在成员 Membership 失效后继续，也不能把私人文本写入日志、Work 或 provider Session。要求连 Space 管理者控制的 Host 都无法读取私人内容，需要成员专属执行信任或端到端加密，是另一条尚未进入的旅程。

### 5.3 Host 与本地 executor

Host 启动时在本地只读 Diagnose Pi 和 Codex，并选择一个可用的 concrete executor。选择发生在 claim 前并在 worker 生命周期内保持稳定；claim 后不切换 provider 或自动 fallback。

普通成员不选择 executor。服务端也不按 provider、Runtime、model 或 Work 类型路由。

Pi 与 Codex 保持独立 adapter，并只共享 Host 当前消费的两个产品行为：

```text
Diagnose(ctx) error
Execute(ctx, immutable Work context) (UnderstandingUpdate, error)
Reply(ctx, bounded private Conversation context) (ReplyCandidate, error)
```

`Execute` 只推进 Work，`Reply` 只形成私人回复及可空 delegation goal；Run 仍然只属于 Work。取消由 `context` 表达。进程、RPC/app-server、临时目录、协议终态和 cleanup 由具体 adapter 拥有。共同合同不包含 Resume、Discard、Session identity、provider event 或 checkpoint。

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

Work Messages 按准确顺序追加。新 Run 选择尚未应用输入的最长连续前缀，最多 32 条 Work Message 且消息文本合计最多 256 KiB；合法单条消息总能进入某个 Run。Run 持久固定本次输入上下限，成功后只推进到该上限，余量由后续 Run 继续。Recovery 使用原范围，不重新扩张。执行上下文只包含：

- goal；
- previous understanding 与 next step；
- 本次有界新增消息，保持顺序和真实作者。

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

## 7. Conversation

Conversation 是准确成员在一个 Space 中与 Carry 的私人消息 owner。第一条旅程每个 `(space, member)` 只需要一个内部 Conversation identity；成员不创建、命名或管理它，Carry 是隐含参与者。

Conversation 保存：

- 按 PostgreSQL 分配的连续顺序追加的 member/Carry messages；
- member message 的请求幂等 identity 与 canonical digest；
- 一条尚未回复的 source message 对应的 reply claim、current Machine、fence、lease 和 fixed context range；
- 成功 reply 的 canonical output digest；
- 如果清晰委托创建了 Work，只在私人侧保存 resulting Work identity。

当前每个 Conversation 同时只允许一个 outstanding member turn。完全相同的请求重放返回原消息；不同请求在 Carry 回复前冲突。这个限制保持严格交替因果，不提前建立 queued follow-up、parent/reply graph 或乱序展示。

Machine claim 只能返回同一 Space、当前成员 Membership 仍 active 的一条 source message及其有界 fixed context。首次 claim 固定 context range；lease recovery 增加 fence 并返回相同事实。claim、renew 与首次 commit 都重新验证 Machine、Space、member Membership、exact source、fence 和 lease。

首次 commit 在一个事务中插入至多一条 Carry reply，并可为清晰委托创建至多一份 Work。Agent 只返回严格的 `reply` 与可空 `delegation_goal`；creator、owner、Space、authority 和 idempotency 由服务端从已认证 source member 与数据库事实建立。Work 只获得新形成的共享 goal，不保存 Conversation/message identity、private digest 或可反向读取的 source relation。

成功 commit 的 exact response-loss replay 不产生新后果：同一原 Machine、fence 和 output digest 在 Machine 未撤销且成员 Membership 仍 active 时返回原 reply/Work，不要求已经过去的 lease 仍有效。不同 digest 冲突；没有 committed result 的 stale fence 不能提交。

第一条 Conversation contract 固定以下界限：member/reply text 各最多 16 KiB UTF-8；Agent fixed context 最多 32 条完整消息且最多 256 KiB；User API 每页最多 50 条，并提供 newest initial page 与 before/after incremental cursor。当前不建立 summary、删除/retention lifecycle、provider continuation 或成员 CLI chat。

## 8. Run 与 Attempt

Run 是一次固定的 Work 推进。Attempt 是这次推进的一次物理执行。

这个区别保留，因为它们拥有不同生命周期：Run 可以在 Host 失败后继续；旧 Attempt 必须永久失去 authority。

### 8.1 没有 coordinator queue

第一版没有后台 coordinator、pending Run 或 coordinator subtype。

Machine claim 在一个 PostgreSQL 事务中：

1. 锁定并验证 Machine；
2. 优先寻找可恢复的 expired active Run，或者寻找有未应用输入且没有 unresolved Run 的 Open Work；
3. 必要时创建 Run，固定 Work、输入上限和 base understanding version；
4. 增加 fence 并创建唯一 active Attempt；
5. 设置数据库时间 lease；
6. 返回不可变执行 descriptor。

如果没有可领取 Work，返回 empty claim。服务端不需要每秒扫描 Work，也不需要独立 goroutine 预先制造 pending rows。

### 8.2 Claim descriptor

Claim 只包含当前 Host 真正需要且可由有界 wire 完整传输的字段：

- Run、Attempt、Work identity；
- fence 与 lease expiry；
- goal；
- previous understanding 与 next step；
- fixed new messages；
- base understanding version 与 input upper bound。

它不包含 writer token、Agent credential、provider、Runtime、model、Session、repository 或 future capability fields。

### 8.3 Authority

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

### 8.4 Host API

Host API 使用 Machine mTLS，只提供当前 worker 需要的两组窄行为：

```text
Work:         Claim / Renew / CommitUnderstanding / FinishFailedOrUnknown
Conversation: ClaimReply / RenewReply / CommitReply
```

两种 Claim 都直接返回各自有界执行上下文，不再增加独立 LoadContext round trip。Work mutation 重新验证 Machine、Run、Attempt、fence 和 lease；Conversation mutation 重新验证 Machine、source message、reply fence、lease、Space 与当前 Membership。两者不共享 target union，Run 仍然只属于 Work，协议 response 不泄漏 provider state。

### 8.5 失败与恢复

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

## 9. Native Agent adapters

Pi adapter 直接使用 Pi documented RPC；Codex adapter 直接使用 Codex app-server。两者通过同一产品 conformance suite，但不共享虚构 provider protocol。

执行输入使用固定 instruction 加不可信 JSON context。Prompt 不包含 credential、fence、Machine identity 或权限声明。

两种产品行为各有一个严格、互不混合的共同输出合同：

```text
Work Execute:        understanding + next_step
Conversation Reply:  reply + nullable delegation_goal
```

模型输出先经过对应的严格结构验证，再由当前 Host 使用该 owner 的 fenced Commit 写入 PostgreSQL。私人 Reply 的 actor、Space、owner、idempotency 与 authority 不来自模型。Agent settled、thread idle、进程退出或一段看似正确的文本都不能单独修改 Work 或 Conversation。

Codex 缓冲结束但缺少准确 terminal notification 时，只可有界只读核对；不能证明完成则 Finish Unknown。Host 不在 claim 后切换到另一 adapter。

Node 10 的第一条第三方能力是一个不持久化的 Reference Catalog。Operator 通过 `CARRY_REFERENCE_BASE_URL` 为 Host 固定一个 HTTPS base URL；Pi/Codex 在 Work Execute 行为中各自通过 native tool wire 暴露 `lookup_reference(key)`。tool handler 只接受 key，固定执行一次 `GET /v1/references/{url_escaped_key}`，拒绝 redirect、非成功状态、无效 UTF-8、超过 64 KiB 的 response 和 cancellation/timeout 以外的隐式 retry。返回文本只作为当前 Attempt 的不可信上下文，不写入 PostgreSQL、Work、Conversation、browser storage、日志或 provider Session。能力失败使 native execution 失败，不能形成虚假的 Work update；既有 Machine/Run/Attempt/fence/lease/base-version commit 仍是唯一写入 authority。

Reference Catalog 不是 Carry 的产品对象、Plugin、MCP server、provider registry 或持久 owner。Pi 的 concrete extension 与 Codex 的 experimental dynamic tool contract 只在各自 adapter 内实现；两者共享产品语义和 bounded HTTP 行为，不共享 provider wire。

## 10. User API

User API 只表达成员旅程：

- 建立/撤销 Browser Session；
- 读取当前具名成员与 Spaces；
- 分页读取并幂等追加当前成员在一个 Space 中的私人 Conversation；
- 创建 Work，分页列出轻量 Work summaries，并分页读取一份 Work 的有界消息历史；
- 追加 Work Message；
- 对需要成员选择的 Work 显式请求重新推进；
- enrollment/revocation Machine。

所有 credential-bearing User API 与 Host API 响应统一 `Cache-Control: no-store`。Work list 使用 Work UUID 作为 exclusive cursor，每页最多 50 条 summary；Work detail 的消息页使用 Message UUID cursor，同时最多 50 条且消息文本合计最多 256 KiB。cursor 必须属于准确 Space/Work。User API 返回 Identity 当前 display name 作为读取时 projection，不把名字复制成 Work 持久事实。

User Work response 包含：

- Work ID、Space、goal、lifecycle；
- owner、creator；
- current understanding 与 next step；
- 是否仍有新信息待 Carry 应用；
- 是否需要成员显式选择重新推进；
- created time。

它不公开 input head、applied input、revision number、Run、Attempt 或 Runtime。

Web 使用 `protocol/user/v1/openapi.yaml` 生成 client。CLI 与 Web 不复制权限规则。

## 11. 必须原子的事务

| 行为 | 同一事务中完成 |
| --- | --- |
| 创建 Work | Membership、目标、首任负责人、幂等身份、初始待应用状态 |
| 追加 Work Message | Membership、Work lock、连续输入顺序、真实作者、幂等 |
| 追加 private Conversation message | Membership、Conversation lock、请求重放、单一 outstanding turn、连续顺序、reply claim |
| Claim private reply | Machine、Space、member Membership、唯一 unresolved source、fixed context、fence/lease |
| Commit private reply | Machine/fence/lease、member Membership、reply-once、可选 Work creation、private-side consequence |
| Claim 新 Work | Machine、eligible Work、唯一 unresolved Run、fixed range、Attempt/fence/lease |
| Recover Run | expired Attempt、旧 Attempt 终结、fence rotation、新 Attempt/lease |
| Renew | Machine、exact active Attempt、current fence、unexpired old lease |
| Commit understanding | Machine、Attempt/fence/lease、Work base version/range、Work update、terminal states |
| Finish unresolved | Machine、Attempt/fence/lease、terminal Run/Attempt outcome |
| Request Work retry | Membership、Open Work、唯一未请求 retry 的 terminal Run、成员与幂等 identity |
| Revoke Machine | 成员权限、Machine revocation；后续 Host mutation 由条件更新拒绝 |

所有网络和 native Agent I/O 都在数据库事务外发生。

## 12. 数据形态

当前持久表应收敛为：

```text
carry_users
spaces
space_memberships
user_tokens
browser_sessions
machines
conversations
conversation_messages
conversation_reply_claims
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

## 13. 代码边界

当前结构：

```text
cmd/
  carry/
  carry-server/
apps/
  web/
internal/
  cli/
    userapi/
  identity/
  space/
  conversation/
  work/
  machine/
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

## 14. Future promotion

当前架构只定义上述六个 owner。未来能力的用户顺序和进入条件由 `docs/implementation.md` 唯一维护；每次进入时重新应用第 3 节的概念准入，不从本文件预建 Result、Timer、Channel、Action、Artifact、Plugin 的表、API、package 或共同框架。Needs You 保持 Work 查询；一个事实被用作依据时仍属于原 owner。

## 15. 正确性证据

当前架构必须由以下测试证明：

- 并发 Work create/message 保持幂等和连续顺序；
- private Conversation 请求重放返回同一消息，不同内容冲突，且同时只有一个 outstanding member turn；
- 同 Space 的其他成员、former member 和 Work query 都不能读取私人原文；
- private reply 并发 claim 一个 winner，recovery 增加 fence 并返回相同 fixed context；
- private reply 首次 commit 需要 live authority，exact completed replay 不复制 reply/Work；
- 普通问题不创建 Work，清晰委托只创建一个由 source member 拥有的 Work；
- 并发 Machine claim 对同一 Work 只有一个 winner；
- Run 与 Attempt 在 claim 事务中一起建立，没有 pending Run；
- expired Attempt 不能 renew 复活；
- recovery 增加 fence，旧 Host 不能 commit 或 finish；
- 预存的大量 Work Messages 被切成有界连续 Run ranges，后续 Run 无遗漏、无重复地继续；
- Work 运行期间到达的新消息不会被旧 range 错误标记已应用；
- Failed/Unknown 不自动 replay，active member 的幂等 `Try again` 才允许一个 fresh Run；
- Commit 直接更新 Work 当前理解，不创建 revision row；
- revoked Machine 不能领取或修改执行；
- Pi 与 Codex 分别通过同一 execution conformance；
- User API 的 Work summaries 与消息历史有界分页，不暴露内部 sequence/version/Run/Attempt；
- credential-bearing response 都是 `no-store`；
- PostgreSQL focused tests 使用真实隔离数据库，缺失数据库不是 pass。

## 16. 明确不采用

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

## 17. 用克制与自由检查架构

一次改动完成前回答：

1. 从成员动作到 PostgreSQL authority 的路径是否更短？
2. 是否出现两个 credential、状态或表保护同一个决定？
3. 是否把本地物理状态上传成了服务端产品事实？
4. 是否让 public API 传输了只有内部 CAS 需要的字段？
5. 是否能删除一个 goroutine、endpoint、interface、row type 或转换层？
6. 是否为一个尚不存在的第二消费者建立了抽象？
7. 是否无必要地禁止了某个仍在当前 authority 内的合法目标、表达或执行方法？

只有删除后当前用户旅程及其真实、authority、privacy、失败恢复和 external consequence 证据仍然完整，并且合法路径没有被缩窄，才删除。

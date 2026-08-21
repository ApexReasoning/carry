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
| Identity | User、邮箱/Google/GitHub proof、短期 proof 事务、显式登录方式变更、User token、Browser Session | User API、Web、CLI |
| Space | Membership、准确邮箱邀请、邀请 submission outcome、Machine enrollment 权限 | User API、Host enrollment |
| Work | 目标、负责人、消息、当前理解、阶段结果检查与接受事实 | 成员、执行路径 |
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

User 是经过认证的人，不因认证自动成为任何 Space 的成员。Node 6 的邮箱方法先证明准确邮箱 possession：trim 后按严格 addr-spec 验证，整体 Unicode lowercase 形成该方法的 canonical key；不做 provider-specific dot、plus 或 alias 重写。该 key 只关联 email method，未来 Google/GitHub email 不能据此自动 linking 或 merge。

邮箱 challenge 是 Identity 的短期内部事实，不是产品对象：六位十进制 code、五分钟 expiry、五次唯一错误尝试、六十秒 resend cooldown，且只有最新 challenge 有效。code 由随机 challenge identity 与 server-held root secret 经 domain-separated MAC 推导，数据库不保存 plaintext 或可离线枚举的裸 digest。PostgreSQL 原子裁决 request/verify idempotency、attempt、expiry、invalidation、single use 与唯一首次成功；exact replay 不重复消耗 attempt，也不产生第二个 User 或 Session。

Resend 是官方 Cloud 的唯一 concrete 邮件 adapter。发送意图先持久化，网络 I/O 在事务外，以固定 recipient、payload 和 provider idempotency key 提交。challenge 只记录 prepared、accepted、rejected 或 unknown 这类 submission fact；Accepted 只证明 provider 接受，Unknown 只允许在 challenge 有效期和 Resend 相同-key窗口内重放完全相同请求。当前不建立 provider registry、Delivery owner、webhook 或 outbox framework。

成功 login verify 在同一权威 transaction 中建立或读取 User、消费 challenge 并建立一个 Browser Session。既有方法先锁 stable identity key，再锁已发现的 User 并重读 exact mapping，才允许签发 Session；首次方法则仍在 stable key 下收敛，不制造 orphan User。Session cookie credential 由稳定 session identity 与 server-keyed MAC 组成，服务端可以在 exact committed replay 中重现同一 credential；PostgreSQL 保存 session identity、User、expiry、`identity_proved_at`、内部 `identity_proof_method` 与 revocation，不保存可直接使用的 secret。新 session 的 proof time 等于数据库创建时间；Identity Settings 只把数据库时间十分钟内的 proof 视为 recent，并且不向协议公开 proof method。Cookie 使用 `__Host-`、Secure、HttpOnly、SameSite Strict 与 Path `/`，logout 撤销准确 Session，stale cookie fail closed。

Google 和 GitHub authentication 继续属于 Identity，不建立 OAuth/provider owner 或 registry。Google identity 只按 canonical issuer 与 case-sensitive `sub` 保存；GitHub identity 只按每次用短期 access token 调用 authenticated `/user` 得到的正整数 ID 保存。provider email、login、name、node ID 和 token 都不能选择 User、查询 `email_identities`、修改 display name 或产生 Membership。

每次 external proof 由一个十分钟、provider-fixed、server-fixed `login`、`reauthenticate` 或 `link` purpose 的 PostgreSQL transaction 与一个 `__Host-`、Secure、HttpOnly、SameSite Lax 的临时 browser binding 共同保护。非 login purpose 还固定 exact target User 与 initiating Browser Session。Identity root 用不同 MAC domain 从 transaction identity 分别派生 state、binding、Google nonce 和 PKCE verifier；数据库不保存这些 plaintext 或 provider code/token。callback 先原子 claim exact response digest，唯一 winner 才在事务外 exchange；Google 验证 ID-token signature/JWKS、issuer、exact audience、expiry、issued time 与 nonce，GitHub 每次重新读取 `/user`。stored purpose 决定 completion，不能由 callback 或 provider content 切换；exact committed replay 不重做 provider I/O。provider exchange ambiguity 终结为 Unknown，不重放 code，也不建立 Carry authority；本地 completion response loss 则先用独立有界 context 重新读取同一 provider 与 callback digest，已提交时恢复准确结果，未提交时才收敛为 Unknown。

OAuth callback URI 只从必填 canonical HTTPS external origin 和固定 Google/GitHub callback path 构造；Host 不匹配时 fail closed，`Forwarded`/`X-Forwarded-Host`/`X-Forwarded-Proto` 不改变它。两种 concrete client 固定官方 endpoint、bounded timeout/body、禁用 redirect；启动不做 provider discovery 或网络调用。GitHub 不请求 email/repository/organization scope，Google 只请求 `openid`，两者都使用 authorization code + PKCE S256。

Identity Settings 只允许 Browser Session principal，不接受过渡 bearer 或 Machine mTLS。方法 projection 固定按 Email、Google、GitHub 返回标签和是否需要重新确认，不公开 email、issuer/sub、GitHub ID、profile、proof 或内部时间。`reauthenticate` 必须证明一个已经属于 exact current User 的方法，只旋转当前 session 并更新 replacement session proof time，不创建 User 或方法。

`link` 必须同时依赖 recent initiating session 与 fresh candidate proof；PostgreSQL 统一先锁 candidate stable identity key，再锁 target User 并重读 exact mapping，occupied identity fail closed without merge，插入至多一个 concrete mapping，并在同一 transaction 撤销该 User 全部旧 sessions、为当前浏览器创建一个 replacement session、记录 proof completion。`unlink` 先只读获得 concrete method key，按同一顺序锁 stable key 与 User 后重读 mapping，再要求 recent session 的内部 proof method 不等于待移除方式、保留至少一个方式，并用窄 command replay fact原子记录删除、全 session revocation 与同一个 replacement session。exact committed replay 可以从 stale initiating credential 恢复仍有效的准确 replacement；different digest 冲突，replacement 后续过期或撤销后不 resurrect。

Email challenge 以相同 closed purpose 固定目标：reauthenticate 不接收地址而投递至已关联 canonical email；link 只证明 candidate email，不能先创建/查找/切换 User 或 session。不存在 pending-link product/table/list、通用 authenticator/provider table、method generation、merge 或 change-history framework。

新 User 在创建首个 Space 前允许没有 display name 和 Membership。`/v1/me` 的 User 与当前 Memberships 是 Web routing 的唯一事实；不增加 profile-completed、onboarding-state 或 default-Space。

现有 User token 与 operator bootstrap 只为已发布 CLI 的过渡消费者保留到 Node 11。Browser 不再接受 token exchange；User token 和 Browser Session 都不能作为 Machine credential。

### 5.2 Space invitation 与 Membership admission

Space invitation 是 Space-owned、只为建立一份准确 Membership 服务的内部事实，不是新的顶层 owner 或 bearer credential。邀请固定 canonical recipient email、Space、inviter、七天 database-time expiry 与现有 `can_manage_members`、`can_enroll_machines` 两个 booleans；没有 role、permission registry、secondary email、public preview 或 pending User。Issue transaction 锁定当前 actor Membership、要求 `can_manage_members`，并禁止授予 actor 当前不持有的 authority。

邀请邮件只包含 canonical HTTPS external origin 下固定 `/invitations` 路径。Identity 当前 `email_identities` mapping 决定邀请 projection；Google/GitHub profile email 永远不参与。Acceptance 要求 active Browser Session 的 `identity_proof_method = email` 且 `identity_proved_at` 在数据库时间十分钟内，并在同一 transaction 重读 exact email ownership、invitation、Space 与 Membership。查看邀请、authentication、email linking 或 reauthentication 都不调用 acceptance。首次 User 缺少 display name 时，accept command 必须提供显式名字并与 Membership consequence 原子提交；provider profile 不填充名字。

Issue 原子建立 invitation 与首个 immutable prepared submission；Resend 只为同一 pending invitation 建立新的 submission，六十秒 cooldown，不修改 expiry、recipient 或 grants。Concrete Resend I/O 在 transaction 外。submission 的 `prepared`、`accepted`、`rejected`、`unknown` 只描述 provider submission；Unknown 不猜测，也不盲目产生新 consequence。每个 issue、resend、revoke、accept command 都绑定 actor-scoped idempotency identity 与 canonical request digest。

Acceptance、revoke 与 concurrent accepts 由 invitation row lock 和 conditional write 裁决一个 winner。already-active Membership 不被 invitation grants 覆盖；invitation 仍以准确 already-member acceptance result 终结。Committed replay 只在同一 actor、same digest 且 resulting Membership 仍 active 时返回原结果；后续 removal 不被 replay resurrect。权限编辑、removed Membership reactivation 与 invitation reminder 属于后续 Node。

### 5.3 Membership removal

Member removal 是 Space-owned 的当前 Membership transition，不删除 User，也不建立 Role、Membership history、reason、generation 或 `left`/`removed` 状态 framework。命令固定 Space、actor、target、可空的 active successor、actor-scoped idempotency identity 与 canonical digest。若 target 仍负责任何 Open Work，successor 必须显式提供；PostgreSQL 在一个事务中把 target 负责的全部 Open Work 转给该 successor 并设置 target Membership `revoked_at`，两种 consequence 不能部分成功。

事务先锁准确 Space，再按稳定 user ID 顺序锁 actor、target 与可空 successor Membership，重读 actor 当前 `can_manage_members`、所有参与者 active 状态与两个 authority holder 数量，随后按稳定 Work ID 锁定 target 的完整 Open Work 集合。移除必须保留至少一名 active `can_manage_members` 与至少一名 active `can_enroll_machines` 成员。Work create/owner mutation 继续先锁 prospective owner 的 active Membership，因此 create/transfer 与 removal 只可能有一个有效顺序；同一 Space 的 removal 由 Space row 串行裁决。

准确 committed replay 可以在 actor 已通过同一命令移除自己后返回同一成功，但只能匹配同一 actor、Space、target、successor 与 digest；different target/successor/request 冲突且不能重放任何 consequence。普通成员自行离开、通用 Work transfer、权限编辑和 Membership reactivation 不由这个入口提供。

Removal 后 Browser Session 与过渡 CLI credential 仍只代表同一 User，Machine certificate 仍代表独立 Space Machine；它们都不缓存 Membership authority。所有 User Space operation 与私人 Conversation claim/renew/commit 继续重读 active Membership，因而 former member 不能再次读取或修改该 Space，而其他 Space 不受影响。历史作者、Work、Message、Conversation、invitation 与 submission facts 保留；已签发 pending invitation 不因 inviter 后来被移除而失效，当前 manager 可以使用既有 revoke 明确终止它。Machine 不因 `enrolled_by_user_id` 自动撤销，服务端也不声称远端进程或已复制数据已经停止或删除。

### 5.4 Machine

`carry host enroll` 由已登录成员发起。服务端在同一事务中验证 Membership 与 enrollment 权限，并签发独立 Machine certificate。

此后 `carry host start` 只使用 Machine mTLS。Machine 被撤销后不能 claim、renew、commit 或 finish。`carry host revoke` 只有在服务端明确确认撤销后，才把本地 credential 原子移出 active Host 路径并删除；响应丢失时保留 credential 供准确重试。已确认撤销后的本地 cleanup 可以在下一次 revoke/enroll 继续，新的 enrollment 不复用旧 Machine identity。

Machine 只保存 durable identity、Space、显示名、证书 serial、enrollment 与 revocation。服务端不保存 Runtime report、binary path、版本、availability 或 Machine status projection。

Space-enrolled Machine 是该 Space 的受信 Carry 执行基础设施。除了共享 Work，它可以在 exact Conversation reply claim、current fence 和 unexpired database-time lease 下读取生成一条私人回复所需的有界上下文。这个 authority 不提供通用 Conversation list/read，不跨 Space，不在成员 Membership 失效后继续，也不能把私人文本写入日志、Work 或 provider Session。要求连 Space 管理者控制的 Host 都无法读取私人内容，需要成员专属执行信任或端到端加密，是另一条尚未进入的旅程。

### 5.5 Host 与本地 executor

Host 启动时在本地只读 Diagnose Pi 和 Codex，并选择一个可用的 concrete executor。选择发生在 claim 前并在 worker 生命周期内保持稳定；claim 后不切换 provider 或自动 fallback。

普通成员不选择 executor。服务端也不按 provider、Runtime、model 或 Work 类型路由。Host 对 transport failure、429 和 5xx 这类明确临时的控制面失败等待后继续 polling；认证、authority、协议或内容错误仍终止进程并交给 operator。这个自愈只重试控制面交互，不复活 stale Attempt，也不建立 heartbeat/status projection。

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
- 绑定准确 understanding version 与内容 digest 的阶段结果检查及幂等接受事实；
- 创建幂等事实。

内部 input sequence 与 understanding version 用于准确区分“消息已记录”和“已经反映到当前理解”，但不进入 User API。User API 只公开当前派生的 `needs_review` 与同一 Work detail 响应中的 opaque review identity。

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

Agent `Execute` strict output 包含 `understanding`、`next_step` 与 `review_required`。最后一个字段只能解释当前输出是否是需要负责人检查的重要阶段结果；它不能提供 actor、owner、version、review identity、lifecycle 或 authority。只有该 Run 的 fixed input end 在同一 Work lock 下仍等于当前 input head 时，`review_required` 才能创建 Work-owned result check。较旧 bounded Run 可以提交其准确前缀理解，但不能让已经遗漏新输入的结果进入 Needs You。

当前 result check 不保存结果正文；正文仍是 Work 当前 understanding/next step。记录只绑定内部 version、canonical content digest、opaque review identity，以及可空的接受 actor、idempotency identity、request digest 和时间。Work Message 前进 input head 或后续 understanding version 会让旧 check 不再 current，不需要破坏历史接受证据，也不建立 Result owner。

提交时 PostgreSQL 原子检查：

- Work 仍可执行；
- Run 仍是当前 unresolved Run；
- Attempt、Machine、lease 与 fence 仍有效；
- base understanding version 没有变化；
- 提交覆盖固定输入上限。

成功后同一事务更新 Work 当前理解、推进 applied input、按上述 current-head 条件创建 result check，并终结 Attempt 与 Run。

负责人接受 result check 时，PostgreSQL 在一个事务中锁定 active Membership、Work 与 exact check，验证 actor 是当前 owner、Work 仍为 Open、version/digest 仍匹配且没有未应用输入，再记录幂等接受。准确 committed replay 可以在当前 active Membership 下先按 actor、idempotency identity 与 request digest 恢复；不同请求冲突，revoked member fail closed。接受不修改 Work lifecycle、不创建新输入、不启动 Run，也不产生外部 authority。

Needs You 是直接查询：只返回当前成员负责且存在 current pending result check 或 `needs_retry` 的 Work。普通 progress、unapplied input、active/succeeded Run、Attempt recovery、lease 或自然语言 next step 都不能使 Work 进入该查询；不建立 Attention、NeedsYou table、异步 read model 或新 owner。

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
Work Execute:        understanding + next_step + review_required
Conversation Reply:  reply + nullable delegation_goal
```

模型输出先经过对应的严格结构验证，再由当前 Host 使用该 owner 的 fenced Commit 写入 PostgreSQL。私人 Reply 的 actor、Space、owner、idempotency 与 authority 不来自模型。Agent settled、thread idle、进程退出或一段看似正确的文本都不能单独修改 Work 或 Conversation。

Codex 缓冲结束但缺少准确 terminal notification 时，只可有界只读核对；不能证明完成则 Finish Unknown。Pi prompt 或 Codex `turn/start` 在 subprocess 启动后发生 write failure 时，request 可能已经部分或完整送达，同样归类为 Unknown，不能猜成 Failed。Host 不在 claim 后切换到另一 adapter。

Adapter cancellation、subprocess kill、temporary-directory cleanup 或 native Session discard 只回收本地资源，不能撤回已经发送给 provider 的 bytes、证明远端工作停止、撤销 PostgreSQL authority 或把 Unknown 改写成 Failed。未来 Attempt 能产生外部后果时，持久事实必须区分“尚未 dispatch”与“已经 dispatch 但 outcome 未观察”，并区分 worker 报告与服务端 recovery 合成；只有这些事实与后续 reconciliation 可以决定 retry safety。

## 10. User API

User API 只表达成员旅程：

- 请求/重发邮箱 code，并验证准确 login challenge 以建立 Browser Session；
- 从 same-origin POST 开始 Google/GitHub login，并在固定 callback 消费 provider proof 以建立同一种 Browser Session；
- 以 Browser Session 读取固定登录方式标签、重新确认已有方式、显式关联新方式并幂等移除仍可安全移除的方式；
- 撤销当前 Browser Session；
- 读取当前 User 与 Spaces；
- authenticated User 显式创建首个 Space；
- 分页读取并幂等追加当前成员在一个 Space 中的私人 Conversation；
- 创建 Work，分页列出轻量 Work summaries，并分页读取一份 Work 的有界消息历史；
- 追加 Work Message；
- 查询当前成员的 Needs You Work，并接受准确当前阶段结果；
- 对需要成员选择的 Work 显式请求重新推进；
- 以 Browser Session 管理准确 Space 的成员邀请，查看当前 pending invitations，并显式 resend/revoke；
- 按当前 User 的准确 Email method 查询邀请，在 recent Email proof 后显式 accept；
- 分页查看 active Space members，并在需要时显式选择一个 active successor，把目标负责的全部 Open Work 与 Membership removal 原子提交；
- enrollment/revocation Machine。

所有 credential-bearing User API 与 Host API 响应统一 `Cache-Control: no-store`。Work list 使用 Work UUID 作为 exclusive cursor，每页最多 50 条 summary；Work detail 的消息页使用 Message UUID cursor，同时最多 50 条且消息文本合计最多 256 KiB。cursor 必须属于准确 Space/Work。User API 返回 Identity 当前 display name 作为读取时 projection，不把名字复制成 Work 持久事实。

User Work response 包含：

- Work ID、Space、goal、lifecycle；
- owner、creator；
- current understanding 与 next step；
- 是否仍有新信息待 Carry 应用；
- 是否需要成员显式选择重新推进；
- 当前阶段结果是否需要 owner 检查；
- 只在准确当前内容可检查时返回 opaque review identity；
- created time。

它不公开 input head、applied input、understanding version、Run、Attempt 或 Runtime。

Web 使用 `protocol/user/v1/openapi.yaml` 生成 client。CLI 与 Web 不复制权限规则。

## 11. 必须原子的事务

| 行为 | 同一事务中完成 |
| --- | --- |
| 完成 Email/Google/GitHub login | exact proof claim、provider identity、User、Browser Session、transaction completion |
| 重新确认已有登录方式 | exact purpose-bound proof、exact User/session、旧 session revoke、recent replacement Session、completion |
| 关联登录方式 | target User/recent session、fresh candidate proof、stable identity ownership、concrete mapping、全旧 session revoke、replacement Session、completion |
| 移除登录方式 | exact User/recent session、concrete mappings、至少一种保留、command replay、全旧 session revoke、replacement Session |
| 创建成员邀请 | actor Membership/authority attenuation、exact recipient/Space/grants、七天 expiry、issue replay、首个 prepared submission |
| Resend/Revoke 邀请 | current manager authority、exact invitation、cooldown/terminal state、command replay、immutable submission 或 revoke fact |
| 接受成员邀请 | active User/session、recent Email proof、exact email ownership、invitation/expiry、Membership、display name、accept replay |
| 移除成员 | Space、actor/target/successor Membership、双 authority liveness、完整 Open Work 集合、原子 owner transfer、removal replay |
| 创建 Work | Membership、目标、首任负责人、幂等身份、初始待应用状态 |
| 追加 Work Message | Membership、Work lock、连续输入顺序、真实作者、幂等 |
| 追加 private Conversation message | Membership、Conversation lock、请求重放、单一 outstanding turn、连续顺序、reply claim |
| Claim private reply | Machine、Space、member Membership、唯一 unresolved source、fixed context、fence/lease |
| Commit private reply | Machine/fence/lease、member Membership、reply-once、可选 Work creation、private-side consequence |
| Claim 新 Work | Machine、eligible Work、唯一 unresolved Run、fixed range、Attempt/fence/lease |
| Recover Run | expired Attempt、旧 Attempt 终结、fence rotation、新 Attempt/lease |
| Renew | Machine、exact active Attempt、current fence、unexpired old lease |
| Commit understanding | Machine、Attempt/fence/lease、Work base version/range、Work update、current-head result check、terminal states |
| Accept Work result check | active Membership、Work owner/lifecycle、exact current version/digest、无未应用输入、成员与幂等 identity |
| Finish unresolved | Machine、Attempt/fence/lease、terminal Run/Attempt outcome |
| Request Work retry | Membership、Open Work、唯一未请求 retry 的 terminal Run、成员与幂等 identity |
| Revoke Machine | 成员权限、Machine revocation；后续 Host mutation 由条件更新拒绝 |

所有网络和 native Agent I/O 都在数据库事务外发生。

## 12. 数据形态

当前持久表应收敛为：

```text
carry_users
email_identities
email_login_challenges
email_login_attempts
google_identities
github_identities
external_login_transactions
identity_method_unlinks
spaces
space_memberships
space_invitations
space_invitation_submissions
user_tokens
browser_sessions
machines
conversations
conversation_messages
conversation_reply_claims
works
work_messages
work_result_checks
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
- `cmd/carry-server` 只是 concrete composition root：构造 PostgreSQL、Identity/Space/Machine 行为、Resend transport、证书 authority 与 HTTP routes，不拥有业务规则或跨步骤应用决定；
- `internal/server` 只是 inbound HTTP transport 与显式 route composition：验证 wire syntax、认证 credential、映射 status/cookie 并调用一个准确行为，不拥有验证码、重放、投递、首个 Space 或证书签发策略；
- 一个 journey 确实需要多步应用编排时，由现有事实 owner 的 concrete behavior 负责；其 persistence 与外部 submit port 由消费它的 owner 定义，adapter 只实现该窄需求，不新增 `service`/`orchestrator` package 或 owner；
- PostgreSQL 实现并保留完整 use-case transaction，不暴露 CRUD repository，也不把一个必须原子的决定拆成 server/domain 多次调用；
- Work、Conversation 与 Run handler 如果只把一个 exact command 翻译给一个完整 PostgreSQL use case，可以直接调用它；不为形式对称增加 forwarding Service；
- `cmd/carry-server` 与 `internal/cli` 显式组合实现；
- Node 6 的 concrete Resend HTTP implementation 与 Node 7 两个 concrete Google/GitHub client 只留在 `cmd/carry-server`；固定官方 endpoint，不建立 adapter package、provider interface hierarchy、registry 或 fallback；
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
- Commit 直接更新 Work 当前理解，不创建 revision row；只有覆盖 current input head 的准确结果才能创建 result check；
- result check 绑定准确 version/digest，新消息或新理解后旧接受失败，并发接受一个 winner；
- 接受只记录 Work-owned 检查事实，不关闭 Work、不创建输入、Run 或外部 authority；
- Needs You 只返回当前 owner 的 current result check 或 explicit retry，不从普通进度、active/recovered Attempt 推断；
- revoked Machine 不能领取或修改执行；
- Pi 与 Codex 分别通过同一 execution conformance；
- User API 的 Work summaries 与消息历史有界分页，只公开派生 `needs_review` 与同一 current detail 中的 opaque review identity，不暴露内部 sequence/version/Run/Attempt；
- Google/GitHub state、browser binding、PKCE、Google nonce、provider proof、denial/outage、callback replay 与同 subject 并发保持一个 exchange winner 和准确 committed Session；
- Email/Google/GitHub reauthenticate 只接受 exact current User 的既有方式；link 要求 recent current proof 与 fresh candidate proof，occupied identity 不 merge；
- link/unlink 撤销全部旧 sessions并只签发一个 replacement；concurrent final-method removals 只允许一个 winner，response loss exact replay 不重复 mutation 或 resurrect revoked replacement；
- 相同邮箱或相同字面 subject 的未关联 email/Google/GitHub identity 保持不同 User，且不能产生 Membership；
- invitation 只投影给 exact Email owner；accept 要求 recent Email proof，provider profile email 与 authentication/linking 单独不能创建 Membership；
- invitation issue/resend/revoke/accept 的 exact replay、expiry、wrong email、already-member、accept/accept 与 accept/revoke race 都由真实 PostgreSQL 直接证明；
- invitation submission 区分 Accepted/Rejected/Unknown，固定 `/invitations` 邮件不含 credential 或 authority；
- provider code/token/ID token 不进入数据库、Carry cookie、clean redirect URL 或 browser storage；
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

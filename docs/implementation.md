# Carry 第一版实施计划

## 1. 目标

这份文件是 V1 建设期间唯一活动计划。它只保存当前顺序、Node 合同和关闭证据，不预先设计未来表、API、package 或状态机。V1 关闭后删除本文件。

实施原则：

- 一条 Node 是一条可演示用户旅程；
- 先从产品路径推导最少 owner，再写代码；
- PostgreSQL 证明 authority、并发、lease、fence 和幂等；
- Pi/Codex 解释开放内容，系统不建立 Plan/Step/Role/Workflow；
- 删除与新增同等重要；
- 一个事实的用途不是新实体；
- 以克制固定真实、authority 和后果，以自由保留目标表达、执行方法和未来演化；
- review 不能为未来便利扩大已经冻结的关闭证据。

### 克制与自由 gate

每条 Node 在研究、合同、实现和 review 四个阶段都回答两组问题。

克制：

- 新 owner、状态、credential、协议或抽象保护哪个当前不变量？
- 它是否把推测、角色或本地物理状态错误提升成产品事实？
- 它是否扩大了权限、隐私可见性或外部后果？
- 删除后如果真实与用户承诺不受损，为什么还要保留？

自由：

- 是否把 Work 按领域、工具、provider、Runtime 或完成方法提前分类？
- 是否用统一框架抹平 concrete adapter 已经有价值的原生能力？
- 是否让未发布兼容、未来 flags 或中央 registry 限制后续重新设计？
- 在准确 authority 内，成员和 Carry 是否仍可选择合法的表达与推进路径？

克制 gate 失败时不能实现；自由 gate 失败时先删除不必要约束。自由不得绕过真实、隐私、成员授权或 external consequence。

## 2. 来源与研究

每个 Node 开始时：

1. 读取五份 canonical 文档；
2. 比较至少五个与当前决策直接相关的官方实现或协议；
3. 对用户指定的历史实现做只读考古；
4. 分开 observed fact、inference 和 Carry recommendation；
5. 不复制旧 package tree、schema、API、migration、generated code、兼容路径或 Web route。

研究细节保存在 Issue/PR 或临时 artifact，不在仓库增加 plan/review/evidence 文档。

Node 3 已完成 Pi 0.84.2、Codex 0.148、OpenHands、Multica、Trigger.dev、Temporal 的恢复研究，以及 `/Users/zane/Dev/loop` 对应 seams 的只读考古。研究证明原生 Session 重取技术可行，也证明 lease/fence 必须由数据库裁决；它没有证明 Carry 现在需要把 native evidence 提升为服务端事实。

## 3. Node contract

进入 Node、写生产代码前，在 conversation、Issue 或 PR 中冻结：

```text
Node:
User journey:
Fact owners:
Allowed paths:
Not doing:
Success evidence:
Failure evidence:
Authority/concurrency evidence:
Focused commands:
Milestone:
```

实现若需要新的 owner、root directory、public protocol audience 或 materially different journey，先停止并更新 canonical 文档和合同。

## 4. 开发循环

每个实现步骤按同一顺序：

```text
读取当前 owner
→ 写失败行为或合同测试
→ 实现最小完整行为
→ formatter、LSP、focused tests
→ 删除被替代代码
→ 回到同一条纵向旅程
```

禁止先连续建立 schema、Store、Service、HTTP 和 UI 五层骨架，最后才第一次运行用户旅程。

### 简洁反思

每一步结束前问：

- 能否删除一个表、状态、credential、endpoint、interface、goroutine 或转换层？
- 是否把同一 authority 拆成了两份 token？
- 是否把本地物理信息上传成服务端产品事实？
- 是否为尚不存在的第二消费者建立抽象？
- 是否把 evidence、completion、draft、observation 等角色升级成了 entity？
- public API 是否泄漏内部 CAS 字段？

### AI-native 反思

- 开放内容是否交给原生 Agent 理解？
- 系统是否只硬编码 authority、identity、time、causality 和 external outcome？
- 是否错误增加 Work 分类、provider routing、Plan、Step 或角色？
- Pi 与 Codex 是否在同一 Work 合同下自然工作？
- Prompt 是否被用来代替 capability 或事务？

## 5. Review 预算

### 普通实现步骤

作者自检、formatter、LSP 和 focused tests，不启动 reviewer。

### 每个 Node 关闭的三门独立审查

每个 Node 在关闭前都并行运行三名 fresh-context reviewer；Milestone 不能替代 Node review：

1. **逻辑与证据：**检查冻结的用户旅程、成功/失败、并发、幂等、恢复与 authority 证据；不确定性必须显式，不能从绿色命令推断未执行路径或未知结果。
2. **架构、产品哲学与 AI-native：**以“责任确定，路径自由”为 gate。Identity、authority、causality、time、privacy 和 external outcome 必须有唯一 owner 与窄入口；内容不能产生权限；没有被当前旅程赚得的 owner、状态、抽象、registry 或兼容层必须删除；准确边界内保留自然语言、concrete adapter 与执行方法的自由。
3. **实现美学：**检查命名、文件 ownership、类型与函数边界、主路径线性、依赖克制和删除质量；范围必须克制但纵向完整，不能用减少行数换取第二份事实、弱 authority、silent fallback 或缺失证据。

三门共同应用四条执行原则：不确定性先显式化；只建立当前旅程赚得的结构；范围克制且纵向完整；以直接证据定义完成。三门接收同一份 frozen Node contract，只报告本 gate 的 blocker 与高价值删除项，不为未来便利扩大范围。

父进程 disposition 全部 finding。修 blocker 后由原 reviewer 最多做一次窄确认；只有修复实质跨越多个 gate 时才重跑完整三门。

### Milestone 或用户明确要求的全仓架构切割

对完整累计 Milestone diff、已发布 journey 和删除机会再次运行同样三门审查。它扩大审查范围，不降低或替代每个 Node 的关闭门。

检查与规定 review 无 blocker后，检查完整 diff、排除无关文件，创建一个 Node-scoped commit 并 push。等待 CI 通过后停止，不自动进入下一 Node。

## 6. Milestones

```text
M0 Foundation                  Nodes 0–1         complete
M1 Native core                 Nodes 2–4         complete
M2 Product access and recovery Nodes 5–10
M3 Responsibility              Nodes 11–12
M4 Connections and capability  Nodes 13–16
M5 V1 closure                  Node 17
```

Node 不是 package 或 migration 批次，而是用户结果。默认一到三个工作日；第三天仍不能关闭时应删减或拆分。

## 7. 已完成切割：Node 5 — 结果检查与 Needs You

M1、进入前 polish 与 Node 5 已关闭。当前先关闭 Pre-Node6 reliability correction，再进入新的 Node 6。Node 5 让当前 Work owner 检查 Carry 的重要阶段结果，并通过个人 Needs You 查询只看到必须由自己处理的 Work。

### 用户结果

Carry 形成一个重要阶段结果时，当前 Work owner 可以从 Needs You 打开准确当前内容并接受它。接受只确认这一阶段结果已经检查，不关闭 Work，也不自动开始下一次推进。成员需要修改时继续使用现有 Work Message。

Needs You 当前只包含两类 Work：准确当前结果等待 owner 检查；或 terminal Failed/Unknown 等待 owner 决定是否 `Try again`。普通进度、未应用输入、Run/Attempt 活动、lease recovery 和自然语言 next step 不进入该查询。

### 事实与 authority

1. Result 仍是 Work 当前 `understanding` 与 `next_step`，不建立 Result owner、结果正文副本或历史浏览。
2. Agent strict output 增加 `review_required` boolean。它只能解释当前输出是否是重要阶段结果，不能提供 actor、owner、version、review identity、lifecycle 或 authority。
3. 成功 Run commit 只在 `review_required` 且本次固定 input range 覆盖事务内当前 input head 时，原子创建一条 Work-owned review fact；较旧 Run 可以正常提交其理解，但不能要求成员检查已经遗漏新输入的结果。
4. Review fact 绑定准确 Work、内部 understanding version 与 canonical content digest。User API 不公开数值 version；opaque review identity 只与 Work detail 的当前内容一起返回。
5. Work Message 前进 input head 后，旧 review 通过 currentness 条件立即失效，不需要破坏性改写 review history。
6. 首次接受在一个 PostgreSQL transaction 中验证 active Membership、当前 Work owner、Open Work、exact review、current version/digest 和没有未应用输入。接受记录 actor、request idempotency 与时间，不改变 lifecycle，也不创建输入或外部授权。
7. 准确已提交接受可以先按 actor、idempotency identity 与 request digest 恢复 response-loss replay；新接受仍要求当前 authority，revoked member 始终 fail closed。
8. Needs You 直接查询 Work/Run/review 当前事实，不建立 Attention、NeedsYou package、表或异步索引。

### 关闭证据

- Pi 与 Codex 都严格产生 `understanding`、`next_step` 和 `review_required`，未知字段与缺失字段失败；
- 有更新输入时，旧 fixed-range Run 的成功提交不产生 review；
- current review 与 Work detail 内容准确绑定，较新消息后旧接受失败；
- owner 接受幂等且 concurrent acceptance 一个 winner，非 owner、former member 与跨 Space actor 被拒绝；
- 接受后 Work 仍是 Open，没有自动 Run 或外部后果；
- Needs You 只返回当前 owner 的 pending review 或 `needs_retry`，不返回普通 progress/recovery；
- Web 完成 Needs You → 打开 Work → 接受当前阶段结果的公开旅程，并对 response loss 使用同一 identity + reload 收敛；
- focused checks、`make check`、race、三门独立 Node-close review、普通 commit、push 与 CI 成功。

### 明确不做

独立 Result/Attention/Question/Decision owner，结果历史正文、accept-and-continue、reject/challenge/withdraw、CLI 交互式 review、Node 11 lifecycle/transfer、Node 12 continuation、Action/Artifact/capability、Agent credential/API、provider registry、全局 Web state/query framework或 migration rewrite。

## 8. 已完成基线：M0、M1 与 Node 5

详细 Node 关闭记录属于 Git/PR 历史；当前稳定行为由 Product、Architecture、协议和测试定义。

- **Node 0 — Foundation/Machine enrollment:** 发布 `carry-server` 与 `carry`；成员以 User identity enrollment/revoke Machine；Host 只使用独立 Machine mTLS；private key 与 pending enrollment identity 先以 `0600` 保存；Runtime diagnosis 只在本地。
- **Node 1 — First durable Work:** CLI/Web 可幂等创建、读取和补充 Work；PostgreSQL 保持消息顺序与重启后连续性；Web 用 HttpOnly Browser Session，不保存长期 bearer；内部 sequence/CAS 不进入 User API。
- **Node 2 — Native execution parity:** Pi/Codex 保留原生 adapter，只共享 Carry-owned Execute/Reply 合同；strict output 只有经 Machine fenced commit 才能修改 owner facts；核心不保存 provider/runtime/model/session。
- **Node 3 — Host interruption recovery:** expired Attempt 永不复活；新 Machine/Attempt 增加 fence 并取得相同 fixed Run range；旧 Machine renew/commit/finish 均失败；Failed/Unknown 只由成员幂等 `Try again` 开启 fresh Run。
- **Node 4 — Private Conversation:** 每个 `(Space, member)` 一个私人 Conversation、一个 outstanding turn；User API 与 Agent context 有界；exact Machine claim/fence/lease 才能读 fixed context；reply-once commit 可原子创建至多一份 Work，但 Work 不保存私人 source、digest 或 transcript；Browser 只保存随机 request key并 fail-closed sign-out。
- **Node 5 — Result review and Needs You:** Agent 只解释 `review_required`；PostgreSQL 仅为覆盖 current input head 的准确 Work 内容创建 result check；Needs You 查询当前 owner 的待检查结果或 explicit retry；幂等接受绑定 exact version/digest，不关闭 Work、不自动继续，也不产生外部 authority。

当前基线明确不包含 native Session recovery、Agent credential/API、provider routing、Work Offer、queued private turns、retention lifecycle、Work/Conversation target union、独立 Result/Attention owner、结果历史正文或 accept-and-continue。

## 13. Node 5：结果检查与 Needs You

状态：complete。

### 用户结果

成员可以检查 Carry 的重要结果，并只看到必须由自己处理的事项。

### 进入原则

先尝试让结果属于 Work 当前内容。只有独立结果需要可引用 revision 和接受/修改/撤回 lifecycle 时才提升 Result identity。Needs You 始终是查询，不建立 package 或 Attention 表。

### 关闭证据

- review 绑定准确内容版本；
- 普通进度和内部恢复不进入 Needs You；
- 接受阶段结果不自动关闭仍有后续责任的 Work。

## Pre-Node6 reliability correction

状态：complete。focused/full checks、三门 review 与窄确认已关闭本次 bounded reliability correction；Node 6 仍需自己的研究、合同、实现和关闭证据。

Node 0–5 中期审计确认 PostgreSQL authority、Work/Conversation privacy、concrete Pi/Codex adapter 与三对象产品预算应保留。本次 bounded closure 只关闭已经实现的可靠性行为：Host 跨 transport、429 和 5xx 这类明确临时控制面故障继续 polling；Pi prompt 与 Codex `turn/start` 在 subprocess 启动后的 write loss 保持 Unknown；服务端确认 Machine revoke 后，本机凭据先原子移出 active Host path，再可恢复地清理并允许 fresh enrollment；Node 5 的 Needs You → exact result → acceptance response-loss → reload 由真实 Web journey 证明。

旧 token-to-browser 与 CLI bearer 仍只是 Node 0–5 的过渡基线，本次不把它们扩展成长期产品合同。云端 Identity、成员准入与恢复、Machine 操作链路和私人回复终态已经提升为正式 Nodes 6–10；每个 Node 重新从当前用户旅程、至少五个一手外部比较与 Loop 只读考古推导，不能把审计 finding 当成实现模板。

## 14. Node 6：Cloud Identity and first Space

### 用户结果

用户可以在官方云端通过 Google、GitHub 或邮箱一次性验证码建立稳定 Carry User，并显式创建一个 Space 成为首位管理成员。自托管部署至少配置 SMTP 或 OIDC/OAuth，并保持相同的 User、Browser Session 与 Space authority；完全离线 Passkey 后置。

Google、GitHub 和 email 是 concrete authentication methods，不是 Carry User。外部 identity 不能直接创建现有 Space Membership；相同邮箱不静默合并不同方法或既有 User。公开注册后的 Space 创建必须由用户明确发生，不自动制造默认 Space。

### 关闭证据

- 三种首发方法都完成真实 Browser journey，并在 repeat login 下解析到准确稳定 User；
- provider subject、邮箱验证、login transaction、Browser Session 和 Space creation 都有准确 owner、expiry、replay 与并发证据；
- 相同邮箱 collision fail closed 并进入明确 linking path，不自动 merge；
- logout、stale cookie、provider outage 和 self-hosted auth misconfiguration 有直接失败证据；
- 旧 bootstrap/member bearer 不再是正常 human login truth。

### 明确不做

成员邀请、method linking/recovery、CLI credential、Passkey、Apple、Microsoft、企业 SSO、domain auto-join 或 broker Organization authority。

## 15. Node 7：Member admission

### 用户结果

管理成员可以从 Settings → Members 邀请准确邮箱；受邀者认证并验证该邮箱后，看到 Space、邀请人与权限，明确接受后加入现有 Space。

### 关闭证据

- `can_manage_members` 是窄 authority，不建立角色 framework；
- invitation expiry、revoke、resend、wrong-email、already-member、single winner 与 response-loss replay 由 PostgreSQL 裁决；
- 第一位新 User 与既有 User 都能接受；
- Space manager 不能恢复、登录或冒充另一成员，也不能读取其私人 Conversation。

## 16. Node 8：Identity recovery and CLI access

### 用户结果

成员可以在已登录状态下明确关联另一种首发认证方法，并通过浏览器批准准确 `carry login` installation；丢失一种方法或 CLI credential 后可以安全替换而不恢复旧 secret。

### 关闭证据

- 当前方法与新方法都经过 fresh proof，provider identity collision 不 merge User；
- 至少一种可用方法保留，revoke 后旧 Browser Session/CLI credential 立即失效；
- CLI approval 显示准确 server/device context，poll secret 与 final credential secret 分离；
- exact approval、redeem、response loss 与 concurrent replay 收敛；
- provider token、Browser Session、CLI credential 与 Machine certificate 不混用。

## 17. Node 9：Machine connection and recovery

### 用户结果

成员运行 `carry host connect`，在 Browser 中检查准确 public key、Space 与显示名后批准这台 Machine；之后可以 list、远程 revoke、本地 cleanup 并重新连接一台新 Machine。

Node 进入时优先重新研究 Multica 的当前 daemon/runtimes 操作链路，并与 GitHub Actions runner、Tailscale device、OpenHands runtime 和至少两个相关一手实现比较。只吸收清楚的产品旅程、确认与恢复优点；不复制 Multica 的 package tree、schema、heartbeat、online/offline、last-seen、Runtime/provider/version 或 capacity projection。

### 关闭证据

- Browser approval 准确绑定 public key、Space、display name 与 approving member；
- Machine certificate 独立于 member/CLI credential；
- list 只展示持久 inventory facts；
- remote revoke 只承诺服务端 certificate authority 已撤销，不声称远端文件已删除；
- local cleanup、response loss、stale credential rejection 和 fresh re-enrollment 有直接证据。

## 18. Node 10：Private reply failure

### 用户结果

私人回复无法形成时，准确成员在原消息旁看到 Failed 或 Unknown，并可以明确 `Try again`；失败不会伪装成 Carry transcript 消息或 Needs You Work。

### 关闭证据

- 成员能区分仍可恢复、明确 Failed 与无法确认 outcome 的私人回复；
- 自动恢复有明确上限，旧 claim/worker 不能晚到提交；
- `Try again` 幂等，新 member message 不与旧失败恢复竞争；
- 失败不伪装成 Carry 消息，也不进入共享 Work 或 Needs You；
- 持久字段、expiry 推进者和 recovery transaction 只在 Node 10 操作链路研究与合同冻结后确定。

## 19. Node 11：Responsibility authority

### 用户结果

成员可以转交负责人、Pause、Close 或 Reopen Work，旧执行立即失去提交权。

这些事实优先直接属于 Work，不建立 delegation 或 lifecycle engine。

### 关闭证据

- 开放 Work 唯一负责人；
- 竞争 transfer 一个 winner；
- Pause/Close fence 旧 Run；
- Reopen 不复活旧 authority。

## 20. Node 12：Future continuation

### 用户结果

成员可以约定一个未来时间让 Work 继续；Pause/Close 后旧约定不执行。

第一条实现优先是 Work 的一个明确 `continue_at` 和 generation。只有多个独立时间约定、recurrence 与 occurrence lifecycle 被真实 journey 要求时才建立 Timer identity。

### 关闭证据

- 时间与时区可检查；
- 同一继续条件只触发一次；
- Pause/Close fence 旧条件；
- 不依赖原生 Agent Session。

## 21. Node 13：First connected conversation

### 用户结果

成员可以从一个已授权渠道与 Carry 或 Work 交流；重复 callback 不复制消息，outbound response loss 保持 Unknown。

从第一条真实 provider journey 推导最小 native identity、target binding 和 outbound outcome。不要预建通用 Connector registry、InboundMessage、Event branch 或第二 provider framework。

## 22. Node 14：Second channel parity

### 用户结果

第二个渠道完成相同产品 journey，同时保留自己的 actor、thread、幂等和 error protocol。

只有两个 concrete adapters 已经存在后，才提升它们真正共享的 delivery semantics；不统一 native payload 或 rich content。

## 23. Node 15：First third-party capability

### 用户结果

Pi 与 Codex 可以安全使用同一份固定只读能力。

节点进入时只选择一个真实 fixture 和一种 transport。没有长期 bytes 前不建 Artifact；没有独立安装 lifecycle 前不建 Plugin owner；没有 credential-bearing invocation 前不建 broker 或 tool registry。

## 24. Node 16：First external Action

### 用户结果

Carry 提出一个准确外部写操作，由正确成员批准，唯一 worker 执行，响应丢失保持 Unknown。

Action identity 必须由独立授权与后果 lifecycle 赚得。只实现一个 typed command，不建立 generic command JSON、universal approval engine 或 provider registry。

## 25. Node 17：V1 closure

### 用户结果

一个团队可以从公开产品入口把 Work 交给 Carry，跨进程和机器持续推进，并安全使用已经被前序真实旅程赚得的能力。

### 关闭

- 从空数据库执行全部 migrations；
- `make check`；
- release binaries/Web assets 可重建；
- 关键 live canary；
- Web accessibility；
- 删除 stale experiment、script、dependency、generated artifact 和本文件；
- 全仓三门 review，无 blocker。

## 26. 条件 promotion

以下不占固定 Node，也不预建 owner：

- Event：第一条没有明确 Conversation/Work target 的授权生产来源；
- Artifact：第一份必须长期保存的 immutable bytes；
- 多个并行执行者：一个 executor 无法自然完成的真实 journey；
- Agent API：原生 Agent 或 bridge 需要直接调用服务端能力；
- native Session optimization：fresh execution 的成本或时延被生产证据证明不可接受。

每次 promotion 都重新走研究、概念准入、Node contract 和关闭证据。

## 27. Node 关闭记录

关闭记录只进入 PR/Issue：

```text
User journey:
Changed owners:
Evidence:
Commands:
Review:
Commit and push:
Deleted:
Deferred:
Residual risk:
```

不把 transcript、review 原文、临时研究或证据 archive 加入仓库。

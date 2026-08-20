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

### 普通 Node 关闭

一名最相关的 fresh-context reviewer 只检查当前 journey、主要失败路径和明显冗余。修 blocker 后最多一次窄确认。

### Milestone 或用户明确要求的全仓架构切割

并行进行三门独立审查：

1. 逻辑、并发和可执行证据；
2. 架构、产品哲学和 AI-native；
3. 命名、文件、类型、函数和删除质量。

三门只报告 blocker 和高价值删除项。父进程综合后修改；每个 blocker 最多一次窄确认，不开启第四轮全面 review。

检查与规定 review 无 blocker后，检查完整 diff、排除无关文件，创建一个 Node-scoped commit 并 push。等待 CI 通过后停止，不自动进入下一 Node。

## 6. Milestones

```text
M0 Foundation       Nodes 0–1         complete
M1 Native core      Nodes 2–4         complete
M2 Responsibility   Nodes 5–7
M3 Connections      Nodes 8–11
M4 V1 closure       Node 12
```

Node 不是 package 或 migration 批次，而是用户结果。默认一到三个工作日；第三天仍不能关闭时应删减或拆分。

## 7. 当前切割：Pre-Node5 polish

M1 与 post-close correction 已完成。进入 Node 5 前只做一次不增加产品能力的维护切割，使当前行为、文件职责、测试导航和 canonical 文档一致。

### 用户结果

一次私人 Reply adapter 失败不会停止 Carry 继续处理其他责任，也不会写入回复或共享 Work。除此之外，M1 用户旅程不变。

### 必须完成

1. Reply 生成在 commit 前失败时保留未提交 claim，Host 继续服务；claim、renew、commit 的 authority 或基础设施错误仍然 fail closed。
2. 并发 Machine 竞争同一 Work 时仍只有一个 Run winner；准确 unresolved-Run 唯一冲突对 loser 表达为无可领取 Work，不泄漏 PostgreSQL constraint error。
3. Machine certificate、server TLS 与 `pki init` 文件职责和名称准确，不把 PKI 表现成额外产品概念。
4. E2E 以行为命名，共享 process fixture 有明确 harness owner，不再依赖某个历史 Node 文件。
5. `docs/product.md` 明确区分当前 M1 合同与未来方向；其余 canonical 文档只保留各自拥有的规则，删除重复解释。
6. Conversation Web pagination/reconciliation 的纯函数与异步 effect 清楚可测，不增加状态框架。
7. 仅在官方已有稳定 Node 24 runtime generation 时刷新 pinned GitHub Actions；不为外部 cache 故障增加 workaround。

### 关闭证据

- Host behavior test 证明 Reply adapter failure 不终止 worker、不 commit，并且 Work 继续推进；
- 并发 claim integration test 证明一个 winner，loser 只得到无可领取 Work；
- 现有 Machine/source/fence/lease 与 Run/Attempt/fence/lease 证据保持通过；
- E2E、Web 与文档导航不再使用 Node 名称表达当前行为；
- public protocol、schema、migration 和六个 owner 不变；
- `make check`、race、一次聚焦 review、普通 commit、push 与 CI 成功。

### 明确不做

Node 5、schema/API/owner 变更、Work/Conversation 通用 claim、execution engine、Go OpenAPI generator、Pi/Codex registry、migration rewrite、全局 Web state/router/query framework，以及只为静态扫描分数改写正确代码。

## 8. 已完成基线：M0 与 M1

详细 Node 关闭记录属于 Git/PR 历史；当前稳定行为由 Product、Architecture、协议和测试定义。

- **Node 0 — Foundation/Machine enrollment:** 发布 `carry-server` 与 `carry`；成员以 User identity enrollment/revoke Machine；Host 只使用独立 Machine mTLS；private key 与 pending enrollment identity 先以 `0600` 保存；Runtime diagnosis 只在本地。
- **Node 1 — First durable Work:** CLI/Web 可幂等创建、读取和补充 Work；PostgreSQL 保持消息顺序与重启后连续性；Web 用 HttpOnly Browser Session，不保存长期 bearer；内部 sequence/CAS 不进入 User API。
- **Node 2 — Native execution parity:** Pi/Codex 保留原生 adapter，只共享 Carry-owned Execute/Reply 合同；strict output 只有经 Machine fenced commit 才能修改 owner facts；核心不保存 provider/runtime/model/session。
- **Node 3 — Host interruption recovery:** expired Attempt 永不复活；新 Machine/Attempt 增加 fence 并取得相同 fixed Run range；旧 Machine renew/commit/finish 均失败；Failed/Unknown 只由成员幂等 `Try again` 开启 fresh Run。
- **Node 4 — Private Conversation:** 每个 `(Space, member)` 一个私人 Conversation、一个 outstanding turn；User API 与 Agent context 有界；exact Machine claim/fence/lease 才能读 fixed context；reply-once commit 可原子创建至多一份 Work，但 Work 不保存私人 source、digest 或 transcript；Browser 只保存随机 request key并 fail-closed sign-out。

M1 基线明确不包含 native Session recovery、Agent credential/API、provider routing、Work Offer、queued private turns、retention lifecycle 或 Work/Conversation target union。

## 13. Node 5：结果检查与 Needs You

### 用户结果

成员可以检查 Carry 的重要结果，并只看到必须由自己处理的事项。

### 进入原则

先尝试让结果属于 Work 当前内容。只有独立结果需要可引用 revision 和接受/修改/撤回 lifecycle 时才提升 Result identity。Needs You 始终是查询，不建立 package 或 Attention 表。

### 关闭证据

- review 绑定准确内容版本；
- 普通进度和内部恢复不进入 Needs You；
- 接受阶段结果不自动关闭仍有后续责任的 Work。

## 14. Node 6：Responsibility authority

### 用户结果

成员可以转交负责人、Pause、Close 或 Reopen Work，旧执行立即失去提交权。

这些事实优先直接属于 Work，不建立 delegation 或 lifecycle engine。

### 关闭证据

- 开放 Work 唯一负责人；
- 竞争 transfer 一个 winner；
- Pause/Close fence 旧 Run；
- Reopen 不复活旧 authority。

## 15. Node 7：Future continuation

### 用户结果

成员可以约定一个未来时间让 Work 继续；Pause/Close 后旧约定不执行。

第一条实现优先是 Work 的一个明确 `continue_at` 和 generation。只有多个独立时间约定、recurrence 与 occurrence lifecycle 被真实 journey 要求时才建立 Timer identity。

### 关闭证据

- 时间与时区可检查；
- 同一继续条件只触发一次；
- Pause/Close fence 旧条件；
- 不依赖原生 Agent Session。

## 16. Node 8：First connected conversation

### 用户结果

成员可以从一个已授权渠道与 Carry 或 Work 交流；重复 callback 不复制消息，outbound response loss 保持 Unknown。

从第一条真实 provider journey 推导最小 native identity、target binding 和 outbound outcome。不要预建通用 Connector registry、InboundMessage、Event branch 或第二 provider framework。

## 17. Node 9：Second channel parity

### 用户结果

第二个渠道完成相同产品 journey，同时保留自己的 actor、thread、幂等和 error protocol。

只有两个 concrete adapters 已经存在后，才提升它们真正共享的 delivery semantics；不统一 native payload 或 rich content。

## 18. Node 10：First third-party capability

### 用户结果

Pi 与 Codex 可以安全使用同一份固定只读能力。

节点进入时只选择一个真实 fixture 和一种 transport。没有长期 bytes 前不建 Artifact；没有独立安装 lifecycle 前不建 Plugin owner；没有 credential-bearing invocation 前不建 broker 或 tool registry。

## 19. Node 11：First external Action

### 用户结果

Carry 提出一个准确外部写操作，由正确成员批准，唯一 worker 执行，响应丢失保持 Unknown。

Action identity 必须由独立授权与后果 lifecycle 赚得。只实现一个 typed command，不建立 generic command JSON、universal approval engine 或 provider registry。

## 20. Node 12：V1 closure

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

## 21. 条件 promotion

以下不占固定 Node，也不预建 owner：

- Event：第一条没有明确 Conversation/Work target 的授权生产来源；
- Artifact：第一份必须长期保存的 immutable bytes；
- 多个并行执行者：一个 executor 无法自然完成的真实 journey；
- Agent API：原生 Agent 或 bridge 需要直接调用服务端能力；
- native Session optimization：fresh execution 的成本或时延被生产证据证明不可接受。

每次 promotion 都重新走研究、概念准入、Node contract 和关闭证据。

## 22. Node 关闭记录

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

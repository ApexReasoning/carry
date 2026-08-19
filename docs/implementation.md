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
- review 不能为未来便利扩大已经冻结的关闭证据。

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
M1 Native core      Nodes 2–4         active
M2 Responsibility   Nodes 5–7
M3 Connections      Nodes 8–11
M4 V1 closure       Node 12
```

Node 不是 package 或 migration 批次，而是用户结果。默认一到三个工作日；第三天仍不能关闭时应删减或拆分。

## 7. 当前简化切割

Node 3 recovery 进入实现后出现了约 1,800 行未提交扩张：native evidence、Resume/Discard、额外 Host API、server retention 和 adapter lifecycle。用户旅程只要求“Host 挂了以后 Work 能继续”，这些概念没有被赚得。

因此在继续 Node 3 前执行一次全仓 simplification cut，并按 Milestone 标准做三门审查。

### 用户结果

已有成员、Work、Machine、Pi/Codex 旅程保持可用；内部路径变短，普通成员和 public API 不再承担执行实现概念。

### 允许修改

- 五份 canonical 文档；
- Identity/Space/Work/Run/Machine 当前 schema 与 migrations；
- PostgreSQL queries、Store 和真实并发测试；
- User/Host server routes；
- Host worker 与 CLI composition；
- User OpenAPI、generated Web client、Web/CLI tests；
- Node 0–2 public-process E2E；
- 删除 obsolete files。

### 必须完成

1. 删除 RuntimeObservation persistence、report/status API 与 CLI status；
2. Host 启动时本地选择一个 concrete executor，不向服务端报告 Runtime；
3. Machine claim 直接创建 Run + Attempt，删除 background coordinator 与 pending Run；
4. Claim 直接返回 Work context，删除 Agent API audience；
5. 删除 Agent credential 与 writer token；Machine mTLS + exact Attempt/fence/lease 是当前 authority；
6. current understanding 直接属于 Work，删除 revision rows；
7. goal 不再重复为 tagged Agent input；执行上下文只传 goal、previous understanding 和 new messages；
8. User API 隐藏 sequence/version，改为简单 `has_unapplied_input` 表达；
9. 删除重复 `carry version`，保留 `--version`；
10. 保留 Machine enrollment/start/revoke，删除没有 admission consumer 的 status/report surface；
11. 恢复前不实现 native Session evidence、Resume、Discard 或 adapter archive lifecycle。

### 成功证据

- current schema 只有当前 owner 的表；
- claim/recovery/commit 是清楚的 PostgreSQL transactions；
- 并发 claim 一个 winner；
- stale/expired Attempt 不能 renew、commit、finish；
- Work 执行期间的新消息不会被旧 Run 覆盖；
- Pi/Codex 各自完成相同 conformance journey；
- CLI、Web、server 重启和 public-process E2E 通过；
- `make check` 通过；
- 三门独立审查无 blocker。

### 明确不做

- 新产品功能；
- 原生 Session recovery；
- Agent direct tools；
- Result/Question/Timer/Conversation/Channel/Action/Artifact 实现；
- provider routing 或 registry；
- migration compatibility branch；
- 修改代码迎合把 TypeScript 路径错误交给 `go test` 的外部 runner。

## 8. Node 0：Foundation 与 Machine enrollment — complete

### 用户结果

两个二进制可运行；成员可以为 Space enrollment Machine，并用独立 Machine mTLS 启动 Host。

### 简化后的合同

- Identity 证明成员；
- Space 判断 enrollment/revocation 权限；
- Machine 保存 durable execution identity；
- Host 只在本地 Diagnose executor；
- 不保存 Runtime observation 或 status projection。

### 保留证据

- enrollment response loss 复用同一 idempotency identity；
- private key 与 pending identity 在请求前以 `0600` 保存；
- Host 不使用成员 token；
- revoked Machine 不能继续执行；
- 两个二进制和边界检查通过。

## 9. Node 1：First durable Work — complete

### 用户结果

成员通过 CLI 或最薄 Web 创建、查看并补充一份重启后仍存在的 Work。

### 合同

Work 拥有 goal、owner、messages 与 current understanding。内部 sequence 只用于 PostgreSQL ordering/CAS，不进入 User API。

### 保留证据

- create/message 幂等；
- 并发消息获得连续顺序；
- 开放 Work 恰有一个负责人；
- Web 不保存 JavaScript 可读长期 bearer；
- unsafe cross-origin write 被拒绝；
- PostgreSQL 重启后事实不变。

## 10. Node 2：Native execution parity — complete after simplification

### 用户结果

Pi 与 Codex 都可以在同一合同下推进 Work；Work 不按 provider、Runtime、Git 或内容分类。

### 简化后的主流程

```text
Machine claim
→ PostgreSQL 创建 active Run + Attempt
→ Claim 返回 immutable Work context
→ one concrete adapter Execute
→ strict understanding/next_step decode
→ Machine fenced Commit 更新 Work
```

Host 启动时选择一个可用 executor。claim 后不自动 fallback。Pi/Codex 保留各自 native protocol，只共享 Diagnose、Execute 和 conformance behavior。

### 保留证据

- strict model output 才能形成 candidate update；
- 只有 PostgreSQL fenced Commit 才修改 Work；
- Codex terminal 无法证明时保持 Unknown；
- current Attempt 不请求 repository capability；
- core/schema/claim 没有 provider/runtime/model/session 字段；
- Pi/Codex live canary 继续有效。

## 11. Node 3：Runtime recovery

### 用户结果

Host 失败后新的 Attempt 从持久 Work 安全继续，旧 Host 不能晚到提交。

### 主流程

```text
active Attempt lease expires
→ next Machine claim locks the Run
→ old Attempt becomes expired
→ fence increases
→ new Attempt receives the same fixed Work context
→ fresh Execute
→ only new fence may commit
```

### 实现顺序

1. expired lease 不能 renew；
2. claim 原子 recovery rotation；
3. concurrent recovery 单 winner；
4. late renew/commit/finish rejection；
5. public-process Host interruption E2E。

### 明确不做

- native Session evidence、resume、archive 或 cleanup protocol；
- 接管运行中的 turn；
- Agent credential rotation；
- provider checkpoint/event abstraction；
- failed/unknown Run 自动重试；
- claim 后切换 adapter。

### 关闭证据

- recovery 总是增加 fence；
- expired lease 永远不能复活；
- 旧 Host 三种 mutation 全部被拒绝；
- 新 Attempt 从 Work context fresh Execute；
- failed/unknown 保持 terminal；
- 没有 provider/runtime/session state 进入 core。

## 12. Node 4：Private Conversation

### 用户结果

成员可以私聊 Carry；普通问题得到回复，明确委托幂等创建 Work，共享成员看不到私人原文。

### 进入原则

只有这条 journey 证明独立隐私、参与者和 message lifecycle 后才建立 Conversation owner。不要建立通用 Message package或 Work Offer。

### 关闭证据

- 同一 execution retry 不产生第二条回复；
- 同一 source message 返回同一 Work；
- Agent 不能伪造 actor、owner 或权限；
- Work 查询无法读取私人原文。

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

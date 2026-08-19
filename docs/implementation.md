# Carry 第一版实施计划

## 1. 目标

这份计划用于把 Carry 从空仓库快速建设成可以真实使用的第一版。

它不是长期 roadmap，也不是把未来功能全部列出来的愿望清单。第一版关闭后，本文件应删除；后续工作回到 Issue、PR 和少量仍然有效的 canonical 文档。

实施必须同时满足：

- 快：每个 Node 默认在一到三个工作日内形成可演示结果；
- 小：实现步骤只完成一条完整行为，不再把每一步变成评审节点；
- 真：通过公开产品入口和真实 PostgreSQL 证明；
- 简：不为未来 provider、状态或 UI 预建结构；
- AI-native：Agent 解释目标和开放内容，系统只固化权限、物理事实和外部后果；
- 可删：每个节点结束时删除被替代的代码、实验、脚本和文档。

## 2. 不复制现成实现

已有系统只能提供两类证据：

1. 已经证明必要的不变量，例如 lease/fence、Unknown、原子接纳和隔离数据库测试；
2. 已经证明失败的设计，例如 provider 泄漏、Git 中心、长期兼容层、重复 owner 和文档堆积。

禁止直接复制：

- package tree；
- migration；
- HTTP API；
- SQL query；
- generated code；
- compatibility branch；
- Web route；
- CI 和 scripts 结构。

如果借鉴一个不变量，必须先从 Carry 当前用户旅程重新推导，再在新 owner 中从零实现和测试。

## 3. Node 与 Milestone

计划只使用两层：

### Node

Node 是一条可以在一到三个工作日内完成的纵向用户旅程。本文编号的 0–12 都是 Node。

Node 内部可以有若干实现步骤，但这些步骤不是新的计划层级，也不分别启动多轮 review。

### Milestone

Milestone 只用于在几个相关 Node 完成后做一次较深的整体审查：

```text
M0 Foundation       Nodes 0–1
M1 Native core      Nodes 2–4
M2 Responsibility   Nodes 5–7
M3 Connections      Nodes 8–11
M4 V1 closure       Node 12
```

Milestone 不引入新的实现范围，也不创建单独计划文件。

不再建立 Stage、Cluster、Phase、Track 等更多层级。

## 4. 时间和停止规则

默认节奏：

- Node 调研与设计：半个工作日以内；
- 一个实现步骤：数小时，结束时必须可测试；
- 一个 Node：一到三个工作日；
- 第一个工作日结束前：必须出现可运行的纵向骨架；
- 第三个工作日仍不能关闭：停止扩写，把剩余范围拆成新的 Node。

以下情况立即停止当前实现并重新切小，不继续堆代码：

- 用户旅程无法用一句话说明；
- 新增 package 没有当前消费者；
- review 已经进入第三轮；
- 同一 blocker 修补两次仍未收敛；
- 为了通过 review 开始增加通用 registry、批次、角色、workflow 或兼容层；
- 一个实现步骤同时改变多个无关 owner；
- 需要在 Prompt 中反复提醒 Agent 才能保证权限或物理正确性。

外部服务不可用可以暂停节点，但不以等待为理由继续扩大范围。

## 5. 每个 Node 的进入流程

进入 Node 后，先完成以下六步，再写生产代码。

### 5.1 写清用户旅程

只用一句话描述节点结束后用户能完成什么。

例如：

> 成员通过 CLI 创建一份有明确负责人、重启后仍存在的 Work。

如果一句话里出现多个“并且”，节点通常太大。

### 5.2 提出 module 假设

只列这条旅程可能触及的 owner，不立即创建目录。

每个候选 module 回答：

- 它拥有哪个事实；
- 当前消费者是谁；
- 必须保护哪个不变量；
- 相邻 owner 为什么不能直接完成；
- 删除后旅程会失去什么。

无法回答的候选 module 从设计中删除。

Node 方案同时列出本次允许新增或修改的目录。实现若需要一个未列出的 owner 或根目录，先暂停并更新方案与关闭证据，不能边写边扩大结构。目录约束来自 `architecture.md` 和 `repository.md`；不为约束而创建空 package。

### 5.3 调研至少五个相关系统，并考古历史实现

每个 Node 都选择至少五个真正相关的产品或开源实现。五个计数项都必须有与当前问题直接相关的官方文档、官方协议或指定源码 revision；二手分析只能补充，不能计数。

除此之外，每个 Node 开始时必须检查用户指定的历史实现中与当前旅程对应的代码。历史实现不计入五个外部系统，也不是模板；它只用于识别已经证明有效的局部做法、曾经造成膨胀或错误的结构，以及当前 Node 必须保留的回归证据。Node 0 和 Node 1 在 M0 关闭前补做这项考古。

调研规则：

- OpenHands 与 Multica 是 Agent/Host/Work 节点的默认对照；
- 其他对象按问题选择，不机械复用同一名单；
- 比较当前决策问题，不做通用 feature checklist；
- 分开记录 observed fact、inference 和 recommendation；
- 记录源码 revision 或访问日期；
- 如果一个对象没有回答当前问题，替换它，不凑数量；
- 调研结论写入节点 Issue/PR，不为每个对象创建仓库文档；
- 历史实现考古必须引用准确文件和 symbol，分别记录可保留做法、必须拒绝的失败模式和 Carry 从当前旅程重新推导的结论；
- 不从历史实现复制 package tree、schema、API、migration、generated code、兼容层或 Web route。

调研的目的不是找一个项目照抄，而是回答：

- 大家为什么需要这个边界；
- 哪些复杂度来自他们自己的产品，而不属于 Carry；
- 是否存在更自然的原生 Agent 路径；
- 哪个最小不变量被多个独立实现共同证明。

### 5.4 写节点设计

节点设计只包含：

- 用户旅程；
- 当前事实 owner；
- 一个主流程图；
- 必须原子的事务；
- 失败和 Unknown；
- 公开协议变化；
- 实现顺序；
- 明确不做；
- 验收命令。

不提前列全部 struct、表字段、文件名和未来扩展点。只有即将实现的第一个实现步骤需要精确到类型和 SQL。

### 5.5 做两次反思

#### 简洁反思

- 是否可以删除一个 package、状态、表或中间层？
- 是否只因为两个实现语法相似就建立抽象？
- 是否为第二个尚不存在的消费者提前设计？
- 是否把实现细节变成用户或 Agent 必须理解的概念？
- 是否留下了明确不做的范围？

#### AI-native 反思

- 这件事应该由原生 Agent 根据目标和上下文判断，还是必须由系统硬编码？
- 系统是否只类型化权限、时间、因果、物理执行和外部后果？
- 是否错误地增加 Plan、Step、Role、Router、Workflow 或工具选择状态机？
- Pi 与 Codex 是否都能在同一个 Work 合同下自然工作？
- 是否错误地把 Git、软件、内容或 provider 变成 Work 分类，而不是由当前 Attempt 获得窄 capability？
- Prompt 是否被用来代替真实 capability 或事务？

发现过度设计时先删设计，不带着问题进入开发。

### 5.6 在方案中确定关闭证据

调研和方案确定后、开发开始前，立即写清：

- 一个成功旅程；
- 一个主要失败旅程；
- 一个权限或并发证明；
- 需要运行的 focused tests；
- 本节点不包含什么。

这组关闭证据与方案一起进入开发。方案发生实质变化时，先更新关闭证据再继续写代码；不能在开发结束后由 review 不断扩大条件。新增要求只有在发现 blocker、数据风险或权限漏洞时才进入当前 Node。

## 6. Node 开发规则

Node 内每个实现步骤按照相同顺序：

```text
读取当前 owner
→ 写一个失败或合同测试
→ 实现最小完整行为
→ 运行 focused checks
→ 删除被替代代码
→ 合入当前 Node 的可运行纵向路径
```

实现步骤必须尽快汇入同一条纵向路径。禁止连续创建 schema、Store、Service、HTTP、Web 五个半成品，最后才第一次运行用户旅程。

可以先做最薄路径，再在同一 Node 内逐步增加错误、并发和 Web 表达。实现步骤只做自检和 focused checks，不分别启动 reviewer。

## 7. Review 预算

### 实现步骤

只做作者自检、formatter、LSP 和 focused tests，不单独启动 reviewer。

### Node 关闭

Node 达到启动时确定的关闭证据后，只做一次 focused review：

- 一名最相关的 reviewer；
- 只检查当前用户旅程、改动 owner 和主要失败路径；
- 只报告 blocker 与明显冗余；
- blocker 修复后由同一 reviewer 做一次窄确认；
- 不启动三门 review，不做全仓扫描。

可选建议记录到后续 Issue，不阻塞当前 Node。关闭检查和规定 review 无 blocker后，检查完整 diff、排除无关文件，以当前 Node 为单位创建普通 commit 并 push 当前分支；不得 force push、改写历史或顺带提交无关工作。提交与 push 完成后停止，等待用户反馈，不自动进入下一 Node。

### Milestone 关闭

一个 Milestone 内的 Nodes 全部完成后，才进行一次三门并行审查：

1. 逻辑和可执行证据；
2. 架构、产品哲学和 AI-native；
3. 命名、文件、类型、函数和删除质量。

三门各自只报告 blocker 和高价值删除项。修复后只做一次针对 blocker 的确认；如果仍有结构问题，拆分或撤回错误设计，不开启新一轮全面 review。

### 第一版关闭

只在 Node 12 做一次全仓审查、完整测试、浏览器旅程、生成检查和陈旧内容清理。

## 8. 状态汇报

每个 Node 只使用：

```text
Designing
Building
Reviewing
Complete
Split
```

汇报保持简短：

- 已完成；
- 当前实现步骤；
- 下一步；
- blocker；
- 是否仍在时间预算内。

不维护百分比、复杂燃尽表或多层状态看板。

## 9. 第一版节点总览

| 节点 | 用户结果 | 默认时间 |
| --- | --- | --- |
| 0. Foundation 与 Host enrollment | 两个二进制可运行，成员可以注册并启动 Host | 1–2 天 |
| 1. First durable Work | 成员可以创建、查看和补充一份持久 Work | 1–2 天 |
| 2. Native execution parity | Pi 与 Codex 都可以通过同一合同推进 Work | 1–2 天 |
| 3. Runtime recovery | Host 失败后新 attempt 可以安全接力 | 1–2 天 |
| 4. Private Conversation | 私聊可以回答问题或直接形成 Work，且不泄漏原文 | 1–2 天 |
| 5. Result 与 Needs You | 成员可以验收结果并只看到必须处理的事项 | 1 天 |
| 6. Responsibility authority | 负责人转交和生命周期变化会正确 fence 旧执行 | 1–2 天 |
| 7. Future continuation | Work 可以在明确约定的未来时间继续 | 1–2 天 |
| 8. Lark 与 Delivery | Lark 会话可以安全接收和回复，Unknown 保持真实 | 1–2 天 |
| 9. Slack parity | Slack 通过同一产品合同而不复制 Lark 结构 | 1–2 天 |
| 10. Read-only Plugin | 同一 Plugin 通过一种真实 transport 被 Pi/Codex 使用 | 1–2 天 |
| 11. Action | 一个真实写操作可被提出、批准、执行和观察 | 2–3 天 |
| 12. V1 closure | 完整第一版旅程、发布物和仓库清理通过 | 1–2 天 |

时间是拆分信号，不是要求牺牲正确性的 deadline。

## 10. Node 0：Foundation 与 Host enrollment

### 用户结果

成员可以运行 `carry`，连接 `carry-server`，为 Space 注册当前机器，并通过 `carry host start` 报告本机可用的 Pi/Codex Runtime。

### 进入时评估的候选 modules

- Identity：当前成员是谁；
- Space：Membership 与 Machine enrollment 权限；
- Host：Machine identity、证书和 Runtime report；
- Server/PostgreSQL：协议和持久化 adapter。

候选不是预建目录清单。调研后如果两个事实可以由一个 owner 清楚表达，就合并。

### 至少调研

- Multica CLI 与 daemon；
- OpenHands runtime；
- GitHub Actions self-hosted runner enrollment；
- Tailscale node enrollment；
- Codex CLI；
- Claude Code remote/managed execution。

### 实现顺序

1. 按 `architecture.md` 与 `repository.md` 建立可编译的 Go module、`cmd/carry-server`、`cmd/carry` 和当前所需 `internal` package；不创建空未来目录；
2. 建立 Makefile、最小 CI 和只表达稳定禁止方向的 boundary check；
3. `carry-server` config/health 与第一条 migration；
4. 最小成员认证和 Space bootstrap；
5. `carry host enroll` 签发独立 Machine identity；
6. `carry host start/status` 与 Pi/Codex availability report。

### 明确不做

- Run queue；
- Agent registry；
- 任意 Runtime plugin 系统；
- Host 自动更新；
- 通用 capability catalog；
- production deployment framework。

### 关闭证据

- `carry-server` 与 `carry` 构建；
- 实际目录只包含 Node 0 允许的 owner，没有空占位 package；
- boundary check 拒绝 domain 反向导入 adapter，并把 Cobra 限制在 `internal/cli/`；
- enrollment 需要真实成员权限，Machine 私钥和 pending idempotency identity 在网络请求前以 `0600` 留在本机；
- enrollment response loss 后复用同一 key 与 idempotency identity，不创建新的未知 Machine；
- Host 启动后不再使用成员 token，服务端重启后仍保持同一 Machine identity；
- 撤销 Machine 后不能继续报告或领取未来工作；
- 本地和 CI 的 `make check-*` 路径一致。

## 11. Node 1：First durable Work

### 用户结果

成员可以通过 CLI 或最薄 Web 界面创建、查看并补充一份有明确目标和负责人的 Work；服务重启后事实仍然存在。

### 进入时评估的候选 modules

- Work：目标、负责人、Message 和输入序号；
- Space：创建权限；
- Identity：Web 使用的短期 opaque browser session，不复制 Space 或 Work authority；
- User API、CLI、Web：三个 adapter，不新增领域 owner。

### 至少调研

- Linear issues 与 Agents；
- Multica task/project；
- OpenHands task/session；
- Devin task；
- GitHub Issues；
- Plane work item。

### 实现顺序

1. Work create transaction 与首任负责人；
2. Work load/list；
3. Work Message 与连续 `input_seq`；
4. CLI create/show/message；
5. Web 以一次性 member-token exchange 建立 HttpOnly browser session；
6. 在第一条 Web journey 中建立 Web toolchain，并完成最薄 create/show。

### 明确不做

- WorkKind；
- Work Offer；
- Plan/Step；
- Run；
- Result；
- Timer；
- Git repository 字段；
- 通用 event log；
- JWT、OAuth/OIDC、CORS 和 production Web embedding；
- React Router、client cache/store 和前端权限裁决。

### 关闭证据

- 并发 create/message 具有准确幂等和序号；
- 开放 Work 恰有一个负责人；
- 权限由成员与 Space 决定，不由文本、cookie claim 或 Web 状态决定；
- Web 不保存 JavaScript 可读的长期 bearer，unsafe cross-origin 写入被拒绝；
- PostgreSQL focused tests 实际运行，服务端重启后 Work、owner、Message 和序号不变。

## 12. Node 2：Native execution parity

### 用户结果

成员要求 Carry 推进一份 Work，Pi 与 Codex 都可以领取并更新同一 Work 当前理解。当前 Attempt 不请求 repository capability；这不是 Work 类型，admission、continuity 和 execution state 不得按 Git、软件、内容、provider 或 Runtime 分类。

### 进入时评估的候选 modules

- Run：subject、attempt、lease 和 fence；
- Host：通用 claim、进程 supervision 和两个具体 adapter worker；
- Work：单 coordinator、writer token 和 revision commit；
- Agent API：准确 Run capability；
- `agent/pi` 与 `agent/codex`：各自拥有原生进程协议。

Node 2 进入研究选择 Pi 0.84.2 documented RPC 与 Codex 0.148 app-server 同时直接实现。ACP v1、`pi-acp` 与 `codex-acp` 已完成对照，但不作为共同 Host boundary：它们没有消除终态、隔离和错误语义差异，还会增加额外 executable 与版本矩阵。两个 native adapter 出现后才提升 Host 已经真实消费的 `Execute` 与 `Diagnose` 语义；启动、原生消息循环、取消、关闭和清理由具体 adapter 拥有。Work、Run、PostgreSQL、Server 与通用 claim 不导入或保存 adapter identity。Host 为每个当前可用的具体 adapter 显式启动 worker，它们竞争同一通用 queue；claim 后不自动 fallback，也不把 provider Session、进程或模型当作 Work 连续性。

### 至少调研

- Pi Session 与 extension/runtime 合同；
- OpenAI Codex CLI/app-server；
- OpenHands agent/runtime；
- Multica daemon/runtime；
- Claude Code；
- Trigger.dev Run/Attempt。

### 实现顺序

1. Work 未处理输入扫描与唯一 coordinator Run；
2. Run 通用 claim、attempt fence 和 late commit rejection；
3. Agent API context/read/revision commit；
4. 直接打通隔离的 Pi RPC 与 Codex app-server 执行，并严格解析 `understanding` 与 `next_step`；
5. 从两个已运行 adapter 提升 Host 实际消费的 `Execute`/`Diagnose` 合同和 conformance suite；
6. Host 为两个 adapter 运行相同 worker loop，Work 当前理解可被成员读取；
7. 用短时 live canary 证明两个真实 Agent；Codex 缺失 `turn/completed` 时只用 `thread/read` 核对准确 turn，无法证明完成则保持 Unknown。

### 明确不做

- provider/runtime-based admission、claim eligibility 或 Work routing；
- provider registry、catalog、factory 或自动 fallback；
- ACP client、ACP wrapper 依赖或共同 Agent wire protocol；
- model profile；
- recovery/resume framework；
- Git checkout；
- gVisor 默认化；
- Child Run；
- Plan/Step/Coordinator role。

### 关闭证据

- Pi 与 Codex 分别完成同一条 Work journey 并通过共同 conformance suite；
- adapter 的 settled、idle、进程退出或模型文本不能直接提交；只有严格结构验证和当前 fenced PostgreSQL revision commit 才算成功；
- Codex terminal evidence 缺失时有界结束并保持 Unknown，不因已有 agent message 猜测成功；
- core、PostgreSQL、Server 和通用 claim 没有 Pi/Codex/provider/runtime 分支或字段；
- 同一 Work 只有一个 coordinator 和 writer；
- stale writer 与 late attempt commit 被拒绝；
- 当前 Attempt 不请求 repository capability，且该事实没有进入 Work schema、admission 或 lifecycle。

## 13. Node 3：Runtime recovery

### 用户结果

Host 失败后新的 attempt 可以从持久 Work 安全接力，旧 Host 不能晚到提交；有可靠原生 Session 证据时可以恢复，没有时从 Work 重新开始。

### 进入时评估的候选 modules

- Run recovery claim 与 attempt rotation；
- Pi 与 Codex 各自的 opaque Session evidence；
- Host-owned Runtime contract 中被两个 adapter 真正消费的 Resume 能力。

### 至少调研

- Pi resume/session；
- Codex resume/app-server；
- OpenHands runtime recovery；
- Multica daemon reconnect；
- Trigger.dev Run/Attempt；
- Temporal Activity timeout/retry。

### 实现顺序

1. 唯一 recovery claim 与旧 attempt 终结；
2. fence 与短期 credential rotation；
3. 两个 adapter 各自验证原生 Session 证据并 Resume；
4. 无可靠 Session 时从持久 Work 建立新 attempt；
5. 分区旧 Host late-commit rejection journey。

### 明确不做

- 大一统 provider event union；
- 跨 Runtime Session 解释；
- Runtime registry；
- 自动 fallback；
- provider checkpoint 抽象；
- Child Run。

### 关闭证据

- recovery 总是轮换 attempt fence 与 credential；
- 分区旧 Host 的晚到提交被拒绝；
- Pi/Codex Session 只由各自 adapter 解释；
- 无法恢复 Session 时从持久 Work 继续而不伪造记忆。

## 14. Node 4：Private Conversation

### 用户结果

成员可以私聊 Carry；普通问题得到回复，明确委托直接创建 Work，共享 Work 读者看不到私人原文。

### 进入时评估的候选 modules

- Conversation：参与者、Message、顺序和 privacy；
- Run：Conversation response subject；
- Work：从准确 source message 创建；
- Delivery 暂不需要，原生 Web/CLI 回复直接读取 Conversation。

### 至少调研

- Multica Mika；
- OpenHands conversation/task creation；
- ChatGPT task/delegation；
- Claude conversation/project；
- Linear Agent delegation；
- Devin session/task handoff。

### 实现顺序

1. 私人 Conversation Message 与幂等顺序；
2. Conversation response Run；
3. 一次 accepted reply across recovery；
4. 明确委托创建 Work 的原子事务；
5. Web/CLI 私聊与 Work 来源隐私测试。

### 明确不做

- 自动把语义相关消息搬入 Work；
- Work Offer entity；
- 群聊；
- Lark/Slack；
- 通用 Message package；
- 长期 Agent memory entity。

### 关闭证据

- 同一 Run 重试不产生第二条回复；
- 同一 source message 重试返回同一 Work；
- Agent 不能伪造 actor、owner 或额外权限；
- Work 查询无法读取私人原文。

## 15. Node 5：Result 与 Needs You

### 用户结果

成员可以验收一个 Result，并在 Needs You 中只看到自己必须处理的事项。

### 进入时评估的候选 modules

Result、review 和 Work Question 默认属于 Work。Needs You 只是查询，不成为 owner。

### 至少调研

- Linear Agent activity/results；
- Multica task review；
- GitHub pull request review；
- LangGraph human interrupt；
- Devin deliverable review；
- OpenHands task confirmation。

### 实现顺序

1. Result propose；
2. accept/revision requested/withdraw；
3. Work Question 的准确目标成员和 disposition；
4. Needs You 聚合 Result review 与 Work Question；
5. CLI/Web 验收 journey。

### 明确不做

- `result` 或 `needsyou` package；
- 通用 approval engine；
- 所有通知聚合；
- Result 接受自动关闭 Work；
- 内部恢复进入 Needs You。

### 关闭证据

- review 绑定准确 Result revision；
- Needs You 只包含目标成员必须决定的事实；
- 普通进度和 Agent retry 不出现；
- Result 接受后仍有后续责任的 Work 保持 Open。

## 16. Node 6：Responsibility authority

### 用户结果

成员可以转交负责人、暂停、关闭或 Reopen Work，所有旧执行立即失去提交权。

### 进入时评估的候选 modules

这些事实默认属于 Work：owner tenure/transfer、Open/Paused/Closed/Reopen 和 authority version。Run 只消费 fence 结果。

### 至少调研

- Linear issue ownership；
- GitHub assignee/permission changes；
- Multica task/autopilot pause；
- OpenHands task pause/cancel；
- Kubernetes generation/fencing；
- Temporal cancellation semantics。

### 实现顺序

1. owner transfer proposal/accept 与唯一任期；
2. Pause 与 writer/Run fence；
3. Close 与未开始执行的取消；
4. Reopen 与新 authority version；
5. 旧 Host、Agent API 和 writer 的并发拒绝测试。

### 明确不做

- 通用 delegation package；
- 自动选负责人；
- 通用 lifecycle engine；
- 把等待或阻塞增加为 Work 状态；
- 取消已经可能发生的外部事实。

### 关闭证据

- 开放 Work 始终只有一名当前负责人；
- 竞争 transfer 只有一个 winner；
- Pause/Close 后旧 Run 不能提交；
- Reopen 不复活旧 authority。

## 17. Node 7：Future continuation

### 用户结果

成员可以约定一个未来时间，Carry 到时继续当前 Work；暂停或关闭后旧约定不再执行。

### 进入时评估的候选 modules

Timer 与 firing 默认属于 Work。调研必须先选择一个真实一次性或周期 journey，再决定第一版是否同时需要两者。

### 至少调研

- Temporal durable timers；
- Trigger.dev schedules/waits；
- DBOS scheduled workflows；
- Linear Loops；
- Multica Autopilot schedules；
- GitHub Actions scheduled workflows。

### 实现顺序

1. 成员明确提出未来时间时直接创建结构化 Timer 并由 Carry 复述；只有 Carry 主动建议或时间表达存在实质歧义时才再次确认；
2. 一次性 Timer 与唯一 firing；
3. firing 获得 Work `input_seq` 并触发 coordinator；
4. Pause/Close/Reopen 的 generation fence；
5. 如果选定 journey 明确需要，再加入最小周期和 missed-window policy。

### 明确不做

- `timer`、`scheduler` 或 `automation` package；
- cron UI；
- workflow DSL；
- 预建复杂 calendar recurrence；
- Timer 作为 Event。

### 关闭证据

- 同一 occurrence 只 firing 一次；
- 时间和时区可被成员检查；
- Pause/Close 后旧 generation 不 firing；
- 恢复后由持久 Work 继续，不依赖原 Session。

## 18. Node 8：Lark 与 Delivery

### 用户结果

成员可以从一个已授权 Lark 会话与 Carry/Work 交流；普通回复回到原 thread，重复 callback 不复制消息，响应丢失显示 Unknown。

### 进入时评估的候选 modules

- `connector/lark`：签名、native identity、payload 和 API；
- Delivery：EndpointLink 与 outbound state；
- Conversation/Work：消息内容最终 owner。

### 至少调研

- Lark bot/webhook 官方协议；
- Slack Events API 与 message delivery；
- Chatwoot conversation/channel model；
- Linear Slack integration；
- GitHub App webhook/idempotency；
- Discord interaction acknowledgement。

### 实现顺序

1. EndpointLink 与撤销；
2. Lark signature、actor mapping 和原子入站；
3. Conversation/Work 的唯一消息归属；
4. Lark outbound Delivery；
5. response loss 与 Unknown；
6. 最小 Lark connection 设置。

### 明确不做

- Slack；
- Messaging.Channel；
- 持久 InboundMessage；
- 通用 Connector registry；
- 通用 webhook；
- rich-content DSL；
- 主动发送到新受众。

### 关闭证据

- callback 成功前事务已经提交；
- native actor 不自动获得 Membership；
- 同一 native ID/different content fail closed；
- 普通回复是 Delivery，不是 Action；
- EndpointLink 撤销后没有新入站或出站；
- Sending 超时进入 Unknown，不盲目重试。

## 19. Node 9：Slack parity

### 用户结果

成员可以通过 Slack 完成与 Lark 相同的产品旅程，同时保留 Slack 自己的 thread、actor 和幂等语义。

### 进入时评估的候选 modules

- `connector/slack`：只拥有 Slack 原生协议；
- Delivery：复用已经被 Lark 证明的跨渠道合同；
- 共享 conformance：证明产品语义，不统一 native payload。

### 至少调研

- Slack Events API；
- Lark callback/message API；
- Discord interactions；
- Matrix application services；
- Chatwoot channels；
- Linear Slack integration。

### 实现顺序

1. Slack installation 与 actor mapping；
2. Slack inbound 原子事务；
3. Slack outbound Delivery；
4. Slack response loss；
5. Lark/Slack Delivery conformance；
6. 最小 Slack connection 设置。

### 明确不做

- 重构成通用 Connector framework；
- 统一 Lark/Slack rich content；
- provider registry；
- Teams/Discord 预留 adapter；
- 主动新受众发送。

### 关闭证据

- 两个 provider 共享产品 Delivery 语义；
- native protocol 仍由各自 connector 拥有；
- Slack callback replay 不复制消息；
- Slack response loss 保持 Unknown；
- 新增 Slack 没有迫使 Lark 通过 provider switch 工作。

## 20. Node 10：Read-only Plugin

### 用户结果

Space 安装一个固定版本 Plugin 后，Pi 与 Codex 都能读取同一 Skill，并通过一种真实 transport 安全调用一个明确只读的 MCP tool。

### 进入时评估的候选 modules

- Plugin installation：digest、enablement、permission 和 PLUGIN_DATA；
- Artifact：保存安装后必须可重复取得的 immutable Plugin package bytes；
- Agent adapters：各自加载同一 prepared descriptor；
- 当前 fixture 所需的唯一 MCP transport owner。

节点调研时选择 Remote MCP 或 stdio MCP 之一。另一个 transport 不在本节点预建。

### 至少调研

- Agent Plugins 1.0 specification；
- Pi Skills/extensions；
- Codex Skills/MCP；
- Claude Code plugins/hooks/MCP；
- OpenHands MCP/tool integration；
- Multica skills/runtime integration。

### 实现顺序

1. 选择一个真实 fixture 和一种 transport；
2. manifest/path validation；
3. 保存 immutable Plugin package bytes，并用 digest 建立安装记录；
4. Pi/Codex Skill parity；
5. 一个 read-only MCP call 的逐次 permission mediation；
6. disable/revoke 后拒绝新调用。

### 明确不做

- 第二种 MCP transport；
- marketplace；
- auto-update；
- Plugin 自带 Secret；
- 长期 credential env；
- tool annotation 直接授予只读；
- 通用 tool registry；
- 写入型 tool。

### 关闭证据

- 相同 Plugin digest 在 Pi/Codex 表现一致；
- 安装后的 package bytes 可以由 digest 重取，不依赖原始来源继续可用；
- path escape 被拒绝；
- Plugin 内容不能扩大权限；
- disable 后旧 Run 不能开始新调用；
- credential 不进入 Agent prompt、package 或日志。

## 21. Node 11：Action

### 用户结果

Carry 可以提出一个准确的真实外部写操作，由正确成员批准，唯一 worker 执行，并在响应丢失时保持 Unknown。

### 进入时评估的候选 modules

- Action：proposal、decision、submit 与 outcome；
- command owner：一个具体 Plugin 或 Connector；
- Work：来源和可见结果；
- worker：与 Node 10 已选 transport 对应的 concrete Action executor。

只实现一个真实 typed command。第二个 provider 不是本节点前提。

### 至少调研

- GitHub Agentic Workflows safe outputs；
- Claude Code permission model；
- OpenHands confirmation/security；
- Temporal Activity retry semantics；
- Restate idempotency/awakeable patterns；
- Stripe idempotency 与 reconciliation；
- Agent Plugins MCP annotations。

### 实现顺序

1. 选择一条非破坏性 canary typed command；
2. proposal 与 immutable command digest；
3. member authorize/decline；
4. unique submit claim；
5. provider call 与 Succeeded/Failed/Unknown；
6. read-only observation；
7. Work/Needs You/Web 表达；
8. response-loss live canary。

### 明确不做

- generic command JSON；
- provider registry；
- universal approval workflow；
- operator 猜测终态；
- Unknown 自动重试；
- 多 provider 泛化；
- 支付或生产删除作为第一条 canary。

### 关闭证据

- Agent 不能选择 credential、审批人或 scope；
- I/O 前授权和 command 已持久；
- 同一 Action 只有一个 submit winner；
- response loss 保持 Unknown；
- read-only evidence 才能收敛；
- 原 Agent Session 结束后已授权 Action 仍可安全执行。

## 22. Node 12：V1 closure

### 用户结果

一个团队可以通过 Web、CLI、Lark 或 Slack 把 Work 交给 Carry，由 Pi/Codex 和一个 Plugin 持续推进，并安全处理一个外部 Action。

### 进入时评估的候选 modules

本节点原则上不增加产品 owner。发现缺口时优先修正现有边界；新增 package 必须证明此前节点无法完成真实旅程。

### 至少调研

- Multica self-host/release shape；
- OpenHands deployment；
- Caddy release simplicity；
- Gitea modular monolith；
- PocketBase single-binary operations；
- GitHub artifact provenance。

### 实现顺序

1. 从空数据库执行全部 migration；
2. 如果已经存在真实部署的 V1 candidate，再验证其 upgrade；没有已承诺版本时不制造兼容工作；
3. 完整 product journey，其中当前 Attempt 不请求 repository capability；
4. Pi/Codex、Lark/Slack、Plugin/Action canary；
5. Web accessibility 和关键浏览器路径；
6. release binaries/image/Web assets；
7. 删除 stale experiment、script、target、dependency、generated artifact 和文档；
8. 全仓三门 review；如有 blocker，最多一次窄 follow-up。

### 明确不做

- 新产品功能；
- Event owner，除非第一条生产来源已经被明确选择；
- marketplace；
- multi-region；
- workflow builder；
- speculative performance framework；
- 为未发布版本保留 compatibility。

### 关闭证据

- `make check` 通过；
- 所有 PostgreSQL test 实际运行；
- 两个二进制和 Web release 可重建；
- 关键 live canary 有外部证据；
- 三门 review 无 blocker；
- 仓库中没有无消费者代码和历史脚手架。

## 23. 条件节点：Event

Event 不在固定 V1 顺序中。

只有当团队选择第一条真实、没有明确 Conversation/Work 目标的生产来源时，才按同一流程建立 Event 节点：先调研至少五个相关系统，再确定 source identity、privacy、零/一/多 Work admission 和 retention。

在此之前：

- 不创建 `event` package；
- 不预建 routing Run；
- 不建立通用 webhook；
- Connector 只处理明确支持的消息和原生证据。

## 24. 节点关闭模板

每个 Node 最终只保留一份简短关闭记录，写在 PR 或 Issue：

```text
User journey:
Changed owners:
Evidence:
Commands:
Focused review:
Milestone review (only when applicable):
Commit and push:
Deleted:
Deferred:
Residual risk:
```

不把 transcript、完整 diff、所有 reviewer 原文和临时研究报告复制进仓库。

## 25. 计划完成条件

第一版关闭后：

1. 将仍然有效的事实更新到 product、architecture、code-style 或 repository；
2. 未解决工作创建普通 Issue；
3. 删除本文件；
4. 不建立第二份长期 roadmap 继续保存已经完成的节点。

实施计划的价值是帮助快速建设，不是成为新的永久架构层。

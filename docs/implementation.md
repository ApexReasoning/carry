# Carry 第一版替换路线

## 状态

节点 0–12 是旧合同下的技术证据。旧的节点 13–19 作废，不得实施。本文件是节点 12 之后唯一的活动路线，拥有节点路线、研究程序、评审协议和证据标准。

Roadmap reset 已由 Issue #1、commit `f2a10bc` 与 CI `32504005646` 关闭；节点 13 已由 Issue #2、commit `663123b` 与 CI `32525708982` 关闭；节点 14 corrective 与 pre-Node15 全项目检查已由 Issues #3/#5、commit `9a283bd` 与 CI `32554850043` 关闭。对该 commit 的复核以 B1′、OAuth actor split 与可信代理来源回退为窄范围重开 Issue #5；这条 follow-up 完成提交、push 与 CI 前不得写节点 15 production。之后节点 15 从简单旅程重新冻结并恢复有界研究。

## 1. 路线规则

### 只用七个 owner

Identity、Space、Agent、Conversation、Work、Machine、Run。计划项、产出项、需要人的事项和继续时间是 Work 的事实；在线、最近活跃和 Inbox 是查询。没有 Assignment、Coordinate、Plan/Step/Artifact owner、Session owner、参与者 owner、通知 owner、调度 owner、workflow 引擎或 provider registry。

### 人与 Agent 是两套表面

```text
人    → Web → Carry Server
人    → carry setup（浅层 operator 接入）→ Carry Server
Agent → carry 本机接口 → 本机 Host → Carry Server
```

Work 只能由 Agent 创建；人用的登录、聊天、Work 管理 CLI 命令作废。

### Server 不推送

记录目标 Agent，Host 拉取并 claim，PostgreSQL 裁决 winner。目标不可用是用户可见状态，不是回退。

### 一个写者

一个人或一个 agent 拥有工作树。子任务不重新设计路线，不启动子 agent，不扩大被批准的范围。

### Issue 拥有节点 artifact

每个节点的旅程冻结、研究、证据、canary 和设计冻结写在这个节点的 GitHub Issue 里。仓库不新增计划、评审、审计或证据文档。

### 门顺序

旅程冻结（§2）→ 问题主导的研究（§3）→ `carry.supervisor` 研究审计（§3.7）→ 精确设计冻结（§4）→ `carry.supervisor` 设计审计 → 实施 → 三份新上下文关闭评审（§6）→ `carry.supervisor` diff 审计 → `make check` → 一次节点提交 → push → CI 终态。跳过任何一步是阻断。

## 2. 旅程冻结

写生产代码前，节点在自己的 Issue 里冻结这十个字段。产品评审者可以在任何研究开始前否决它。

```text
1  触发者与位置        谁、在哪个界面或哪个进程发起
2  当前用户相信什么    这个节点开始前，用户界面上成立的事实
3  这个节点改变的一件事 用一句话说明用户之后能做什么、之前不能
4  授权               谁被授权、凭什么事实被授权
5  权威与唯一 winner   哪个约束裁决冲突
6  失败与恢复          至少包含：目标 Agent 不可用、进程丢失、响应丢失（Unknown）
7  用户如何看到结果    准确的界面位置和文案含义
8  被替代并删除的路径   界面、路由、schema、查询、命令、测试、文档
9  关闭证据            可直接观察的事实，不是命令返回 0
10 明确不做            本节点被诱惑但拒绝的相邻能力
```

字段 3 出现"并且"通常说明这是两个节点。字段 8 为空必须写明为什么这次没有可删除的东西。

## 3. 问题主导的研究程序

研究的目的是改变或验证冻结的旅程，不是列举产品。

### 3.1 冻结问题与反证问题

每个节点写下一句可判定的**冻结问题**（答案会改变设计），和一句会推翻当前倾向方案的**反证问题**。没有反证问题的研究不算通过。

### 3.2 来源相关性矩阵

来源按五个维度打分（0/1），不按流行度、star 数或"大家都这么做"选择。

| 维度 | 通过条件 |
| --- | --- |
| 交互属性同构 | 它解决的是冻结问题的同一个交互属性，而不是同一个行业 |
| 第一方 | 规范文本、官方源码，或本机可执行并可复现的官方 help / 输出 |
| 暴露失败 | 该来源明确说明或可观察到失败、拒绝、超时或未知结果 |
| 覆盖权限或并发 | 它对身份、授权、幂等、顺序或并发有明确立场 |
| 可反证 | 它能提供反对当前倾向方案的证据 |

规则：通常至少五个来源通过，且各覆盖一个**不同的**交互属性；同一属性的第二个来源只有在结论冲突时才保留。若冻结问题本身不足五个独立交互属性，Issue 必须列出全部属性并说明为什么继续凑来源只会制造重复证据，由 `carry.supervisor` 决定是否足够。只有"流行""主流""竞品都有"的来源直接丢弃；多数票不是结论——写下每个来源各自的约束，再判断 Carry 是否有同一约束。

### 3.3 Loop 考古

`/Users/zane/Dev/loop` 只读。每个节点在研究开始前写下准确考古目标：

```text
问题：      本节点冻结问题的哪一部分 Loop 曾经真的遇到
读取范围：  准确的目录或文件路径（不超过冻结问题需要的范围）
要找的事实：哪一个 owner、哪一次失败、哪一条约束
不看：      package 树、schema、API、route 的形状
```

只提取"哪条约束真的保护了权限、并发或用户损失"和"哪部分只是当年的包对称"。永不复制它的 package 树、schema、API 或路由。考古结论进入同一张证据表，来源列写准确路径。

### 3.4 证据行格式

每条证据一行，八列缺一不可：

| 列 | 内容 | 规则 |
| --- | --- | --- |
| 观察事实 | 直接看到的行为或文本 | 只写看到的，不写解读 |
| 准确出处 | 文件:行、符号名、规范小节，或本次执行的命令与版本 | 必须可被另一个人重放 |
| 适用性 | 它对应 Carry 的哪个 owner / 哪条约束 | 指名 owner |
| 失败模式 | 该做法在什么情况下会坏 | 不允许空 |
| 推断 | 从观察到的事实推出的结论 | 与第一列物理分开 |
| Carry 建议 | 采用、部分采用还是拒绝 | 必须有一个动词 |
| 删除影响 | 采用后当前树里哪条路径必须删除 | 没有就写"无" |
| 不确定性 | 还不知道什么、如何在实施中验证 | 不允许空 |

前两列是事实，后面是判断。评审者可以只否决判断而保留事实。

### 3.5 真实 canary

在真实进程或真实浏览器中，用真实凭据执行一次被改路径，并观察到用户可见事实发生变化。不算 canary：单元测试全绿；mock 返回预设值；只断言 HTTP 状态码；预置数据下的界面截图；只在 CI 日志里出现的成功。

凭据不可得时写下受阻 canary：准确的缺失凭据、被阻断的步骤、凭据到位后要执行的准确命令。

### 3.6 研究禁止

不按 star 数、下载量或"市场共识"选来源；不把某个产品的默认参数当作 Carry 的默认值；不从任何仓库复制 package 树、schema、API 或路由；不把没有第一方出处的记忆写成观察事实。

### 3.7 `carry.supervisor` 研究审计

`carry.supervisor`（Claude Opus 5，只读）是第 3、5、8 步的连续性门，不替代 §6 的三份独立评审。

输入只有四项：十字段旅程冻结；冻结问题与反证问题；完整八列证据表；当前倾向方案的一段话摘要。

输出只有四项：阻断项（编号 + 违反了哪条冻结约束 + 出处）；证据缺口（哪一列是判断冒充了事实）；被这份研究要求删除但尚未列出的路径；一句话结论。存在阻断项时设计冻结不得开始。

## 4. 精确设计冻结

研究审计通过后，节点在 Issue 里写下：

```text
owner 与 package     只列本节点真的会改的
准确文件预算         新增手写文件逐个列出；修改文件逐个列出；生成文件写目录
删除范围             准确路径，含测试、路由、schema、UI 和文档
线性流程             一段 text 图，从用户动作到用户看到结果
事务阶段             begin → 锁读 → 拒绝终态/重放 → 校验权限 → 写转移 → 记录提交 → commit
证据清单             §2 字段 9 的可观察事实逐条对应到一个测试或一次 canary
命令                 本节点用来验证的准确命令
不做                 §2 字段 10 的重述
```

预算是约束不是建议：研究可以从预算里**删**文件；要**加**一个 package、migration、公开路由、凭据受众或文件家族，必须先回到 §2 重开合同并取得用户明确决定。本文件的 §7 只写到 package 与责任；准确文件名、migration、字段名和命令语法在 Issue 的设计冻结里才存在。

## 5. 面向 Agent 的接口：冻结的原则与待研究项

`docs/code-style.md` §11 引用本节。这里冻结**原则**，不冻结形状。

已冻结的原则：不按内容分裂命令；机器可读的结构化输入输出，stdout 只承载协议；可发现性稳定但不是运行时动作注册表；错误类别稳定互斥（本地失败、Host 拒绝、Server 拒绝、Unknown 永不合并）；没有自然语言动作入口或按名字派发的通用表面；调用方内容永不携带 Host 或 Server 已拥有的身份与权限事实；"当前 Work 上下文中"与"无 Work 的本机创建"各有自己的权限校验。

**未冻结、必须由节点研究决定**：命令数量与动词名；名词层级；传输（本机 socket、环境变量、文件描述符或其他）；结构化输入的字段名；操作集合是否封闭以及由谁拥有；退出码取值；幂等键是否存在、由谁拥有、如何推导；本机凭证的形状与有效期。

冻结这套形状的是节点 18；节点 16 只负责提供 Pi 与 Codex 的第一方进程与会话证据。在冻结语法与文件前，节点 18 必须用第一方证据比较至少这三种候选，并各自跑一次真实或明确受阻的 canary：

1. 资源导向的命令表面（一个入口 + flag，机器格式与退出码承载语义）；
2. 结构化 stdio 会话（长连接、逐条对象、Host 侧持有上下文）；
3. 具体 adapter 的原生工具注入（由 Pi / Codex 自身的工具机制暴露能力）。

Pi 与 Codex 的第一方证据（可执行的官方 help、源码或规范文本）是这三项的判定依据；没有第一方证据的记忆不得进入证据表。

## 6. 节点关闭三评审协议

### 6.1 规则

- 三位评审者并行、新上下文、只读，互不通信；
- 三位收到完全相同的输入包（§6.2）；
- 只报阻断项和高价值删除项，每条必须带 `path:line` 和证据；
- 不报风格偏好、不报未来设想、不扩大范围；
- 一个写者修复；只有提出该项的评审者做窄范围复核；
- 只有当修复跨越多个 gate 时才三位全部重跑；
- 里程碑级累积评审不替代任何一个节点的关闭评审。

### 6.2 输入包（三位完全一致）

```text
1  Issue 中的十字段旅程冻结 + 精确设计冻结（原文）
2  本节点完整累积 diff（不是最后一次提交）
3  canonical 文档中被引用条款的原文摘录
4  证据索引：每条关闭证据 → 对应测试或 canary 的准确路径
5  排除项：生成代码目录、vendored 代码、与本节点无关的用户改动路径
```

### 6.3 Gate 1 — 产品、旅程、逻辑与直接证据

- 旅程是否完整：从用户动作到用户看到结果，没有断点、没有 TODO、没有只在测试里存在的分支；
- 是否有直接执行证明：真实进程或真实浏览器观察到用户可见事实改变；命令返回 0 不算；
- 界面是否说了真话：两位负责人、目标 Agent 状态、进展与产出与实际事实一致；
- 失败与恢复是否覆盖旅程冻结字段 6：目标 Agent 不可用、进程丢失、响应丢失；
- 本节点是否只做了它冻结的那一件事。

### 6.4 Gate 2 — 权限、并发、隐私、Unknown 与 AI-native

- 每个被改事实是否有唯一 owner，且与 `docs/architecture.md` §2 一致；
- PostgreSQL 是否真的裁决权威与并发：唯一 winner、幂等、lease 过期、迟到 fence 被拒；
- 不同 actor 的创建与写入是否走各自的 handler 与事务，权限来源没有被合并；
- 隐私边界：私人内容、来源关系、transcript 是否被挡在共享 Work 之外；
- 因果：一次执行的结果是否可以追到发起它的执行与成员；
- Unknown 是否被独立表达，且没有被自动重放或猜成成败；
- 时间：数据库时间是否拥有顺序、过期与继续；有没有进程内时钟悄悄授予权限；
- 责任固定、路径自由：系统是否只裁决事实与权限，没有替 Agent 规定语义步骤，没有偷偷长出 workflow 引擎或 Server 推送。

### 6.5 Gate 3 — 美学、package/文件、删除与测试质量

- 条件：有没有 `docs/code-style.md` B1/B2（含被 helper 藏起来的长条件）；
- 命令与字面量：有没有 B3/B4；
- 函数与事务：有没有 B5 混合责任、B6 匿名事务、B8 只转发的抽象；
- package 与文件：新 package 是否对应事实 owner、具体进程 adapter，或不拥有产品策略的明确 composition/transport 边界；文件是否以一个内聚行为命名（B10），有没有新的中央清单或垃圾桶目录；
- 删除：被替代的路径是否在同一个节点删除（B11），有没有两条活着的真相；
- 测试：有没有 B9 仪式测试；每个测试是否说明了哪个用户可见事实证明了它；
- 名字是否使用产品与 owner 语言。

### 6.6 评审任务模板

三位评审者收到同一段文字，只有 `GATE` 一行不同。

```text
角色：Carry 节点关闭评审者（只读，新上下文，不与其他评审者通信）
GATE：<1 产品与直接证据 | 2 权限并发隐私与 AI-native | 3 美学、package 与删除>

输入包：
  1 旅程冻结与设计冻结：<Issue 链接或原文>
  2 完整累积 diff：<命令或路径>
  3 canonical 条款摘录：<原文>
  4 证据索引：<关闭证据 → 测试/canary 路径>
  5 排除项：<生成代码目录、vendored、无关用户改动路径>

你要做的：
  按本 gate 的检查清单（docs/implementation.md §6.3/§6.4/§6.5）逐条核对。
  只允许读取。不修改任何文件，不运行会写入的命令，不启动子 agent。

你要输出的（除此之外不要输出别的）：
  阻断项：
    - [B<n> 或 gate 条目] path:line — 违反了什么 — 证据（引用 diff 行或命令输出）— 最小修法
  高价值删除：
    - path — 为什么现在没有消费者 / 合同 / 有效证据
  结论：一行，"可以关闭" 或 "不可关闭：<阻断项编号>"

不要输出：风格偏好、未来设想、范围之外的重构建议、对已排除路径的意见。
```

### 6.7 `carry.supervisor` diff 审计

三份评审的阻断项全部清空后，把 §6.2 的输入包 1、2、3 加上三份评审结论交给 `carry.supervisor`，它输出与 §3.7 相同的四项格式。通过后跑 `make check`，一次节点范围提交，push，等 CI 终态。

## 7. 节点路线

| 里程碑 | 节点 | 产品证明 |
| --- | --- | --- |
| 进入产品 | 13–14 | 任何新用户能登录、进入或被邀请进入一个 Space |
| Agent 与对话 | 15–16 | 一台 Host 接入，Agent 有持久身份，用户能与选定的 Agent 持续对话 |
| Work 由 Agent 建立 | 17–18 | 三条创建路径都产生两位负责人，且都经过 Agent |
| Work 真的被推进 | 19–21 | 一个 Work 有进展、协作、计划与产出，界面说真话 |
| 验收与生命周期 | 22 | 人类负责人能验收产出并暂停、关闭和重开 Work |
| 跨渠道与时间 | 23–25 | Inbox、邮件、飞书与继续时间不破坏 Work 真相 |
| 里程碑门 | — | 一条真实的非代码旅程端到端成立，无预置数据 |
| 有后果与可运营 | 26–30 | 外部效果、干净安装、Agent 交接、成员离场与 Space 结束都被证明 |

两条精度规则：**删除在本路线中已经有唯一 Node owner**，完整文件/目录直接列路径，局部行为列 `path::symbol`、测试名或当前行段；Issue 研究只能证明某项无需删除并把它从预算删去，不能把它移到另一 Node 或用新 catch-all 扩大。**新增只写到 package 与责任**，准确文件名、migration、路由与字段名在 Issue 的设计冻结里才写死。所有历史 migration 服从 `docs/repository.md` 的全局前向规则。

### 节点 13 — 选择或创建 Space

**能力**：登录后从自己已属于的 Space 中选一个，或用一个显示名新建一个并获得全局唯一 slug。

**package 责任**：`space`（创建、显示名、slug 归一化与建议）；`server`（Space 选择与创建 transport）；`postgres`（唯一约束与查询）；Web 新增 Space 选择/创建 feature。

**删除**：完整删除 `apps/web/app/features/user-session/first-space.tsx`、`first-space-creation.ts`；删除 `apps/web/app/features/user-session/use-user-session.ts` 的 `"first-space"` phase/`createFirstSpace` 分支与 `routeUser` 当前行 91–106 的 zero-Membership invitation/first-Space routing（保留并改为所有 Membership 数量都先检查 pending invitations）、`apps/web/app/features/user-session/invitation-inbox.tsx` 当前行 158–160 的 `user.spaces.length === 0` first-Space skip 文案、`apps/web/app/carry-app.tsx` 的 `session.phase === "first-space"` 分支与当前行 142–164 的 conditional header picker/static single-Space display；删除 `apps/web/app/features/works/use-work-board.ts` 当前行 37–45 的 single-Space auto-entry，使选择只由 `/s/{slug}` URL 决定；删除 `internal/server/space_creation_api.go::{FirstSpace,spaceCreationAPI.createFirst}`、`internal/server/routes.go::{NewUserSpaceRoutes,NewUserSpaceRoutesWithInvitations}` 的 first-Space 参数/`POST /spaces` createFirst 绑定；删除 `internal/space/creation.go::{ErrInvalidSpaceCreation,ErrAlreadyHasSpace,FirstSpacePersistence,FirstSpace,NewFirstSpace,CreateFirstRequest,FirstSpace.Create,CreateFirstCommand,firstSpaceRequestDigest}` 及 `internal/server/space_creation_api.go::createFirst` 的旧 generic-invalid/already-has-Space error mapping；用普通 Space 创建替换 `internal/postgres/space_creation.go::{CreateFirstSpace,restoreCreatedSpace}` 与 `internal/postgres/queries/space_creation.sql::{LockSpaceCreator,LoadCreatedSpaceByRequest,HasActiveMembership,SetInitialDisplayName,CreateFirstSpace,CreateFirstSpaceMembership}`；替换 `protocol/user/v1/openapi.yaml::createFirstSpace`、`apps/web/app/carry-api.ts::{createFirstSpaceRequest import,createFirstSpace}`；删除 `cmd/carry-server/main.go::run` 中 `space.NewFirstSpace`/`NewUserSpaceRoutesWithInvitations(firstSpace, …)` composition，重写 `cmd/carry-server/external_login_integration_test.go::createFirstSpace` 与 `composeExternalLoginTestAPI` 当前行 749–752、772 的 FirstSpace-only composition；重写 `internal/space/creation_test.go::TestFirstSpaceOwnsIdentityNormalizationAndDigest`、`internal/server/email_login_api_test.go::{TestFirstSpaceUsesAuthenticatedUserAndNarrowAuthority,TestFirstSpaceRejectsTransitionalBearerBeforeSpaceBehavior}`、`internal/postgres/email_login_integration_test.go::TestFirstSpaceCreationIsAtomicIdempotentAndSingleWinner`、`internal/server/api_test.go` 当前行 283、391–393 的 FirstSpace stubs、`internal/server/email_login_api_test.go` 当前行 200–244、324–329 的 FirstSpace composition/stubs、`internal/server/space_invitation_api_test.go` 当前行 27、88、153–155 的 `NewUserSpaceRoutesWithInvitations` first-Space consumer/stub、`apps/web/app/features/user-session/user-session.test.tsx` 的 `"first-Space unknown replays exactly then routes a concurrent other Space"` 用例、`apps/web/app/carry-app.test.tsx` 当前行 213–284 的 first-Space journey、`apps/web/e2e/user-session.spec.ts` 当前行 109、192、197–211 的 first-Space/concurrent-other-Space journeys，以及 `apps/web/e2e/first-durable-work.spec.ts` 当前行 19–24 的 first-Space 表单段。删除邀请接受中此后不可达的姓名输入/写入：`apps/web/app/features/user-session/invitation-inbox.tsx` 当前行 32、211–219、233–237 的 name state/form/gate；`internal/space/invitation.go::{Invitations.Accept,AcceptInvitationCommand.DisplayName}` 的 display-name digest/input；`internal/postgres/space_invitation.go::AcceptInvitation` 的 name validation/`SetInvitationAcceptedUserName` 调用与 `internal/postgres/queries/space_invitation.sql::SetInvitationAcceptedUserName`；`internal/server/space_invitation_api.go::spaceInvitationAPI.accept` 的 display-name body mapping；`protocol/user/v1/openapi.yaml::acceptSpaceInvitation` 的 display-name request field；`apps/web/app/carry-api.ts::acceptInvitation` 与邀请 UI/tests 的 display-name parameter/fixtures。删除 Identity 可空旧路径：`internal/postgres/queries/email_login.sql::CreateEmailUser`、`external_login.sql::CreateExternalLoginUser` 的 NULL display name，改为 deterministic fallback；`internal/server/user_api.go::userAPI.me` 当前行 44–52 的 nullable conversion；`protocol/user/v1/openapi.yaml::User.display_name` nullable schema及生成 Web 可空处理。历史 migration `0009_email_identity_first_space.sql` 不改写；新前向 migration 回填 fallback、恢复非空并添加 slug live schema。

**生成目录**：`internal/postgres/dbsqlc/`、`apps/web/app/generated/`。

**线性流程**：登录 → Space 选择页（已属 Space 列表 + 新建）→ 提交显示名 → 归一化 slug → 冲突时就地给出带后缀建议 → 进入该 Space。

**研究问题**：显示名到 slug 的归一化如何既保证全局唯一，又保证用户能读出、输入并稳定复制 `/s/{slug}`？**反证问题**：哪种真实显示名（大小写、Unicode、同形字、混合文字、超长）会让归一化产生用户无法理解或无法稳定重开的 slug？

**权限 / 失败 / 直接证据**：只有成员看得到自己的 Space；并发同名创建由唯一索引决定唯一 winner，落败者看到未预留建议而不是错误页；零/一/多个 Membership 都停在显式选择页，未知与非成员 URL 结果相同；无预置数据的新账号从登录走到 Space 页面。

**命令**：`go test ./internal/space/...`；`make test-db`；`pnpm --dir apps/web test`；`make check-product`。

**不做**：onboarding 表单、顶层 slug 保留名表、slug 改名/别名/重定向、Space 删除、成员管理界面、Space 设置的其余部分。

### 节点 14 — 邀请链接的登录与接受

**能力**：拿到 `/invitations/{invitation_id}` 的人完成认证后，只有准确受邀 Email 的当前 owner 能看到并显式接受邀请；该 ID 本身不授权，也不在认证前披露元数据。

**package 责任**：`space` 继续拥有邀请身份、收件 Email、Space、邀请人、两项授予、签发/重发/撤销/接受、终态与准确 owner 投影；`identity` 只在短期 login transaction 中携带一个语法有效、无 FK 且不解释的 invitation UUID 续接值；`postgres` 裁决终态 winner、数据库时间、重放与 Membership；`server` 只解析续接并映射到 Space-owned 准确路径；Web 保留 Email-owned 通用 inbox 作为 fallback。

**删除/替换**：删除 `Invitations.destinationURL` 与固定 `/invitations` 构造、`ExternalOrigin.InvitationsURL`、Web 把 `/invitations` 当作邮件准确入口的分支、会丢失准确意图的 history rewrites、通用 no-match/first-Space recovery 和固定通用链接断言。保留认证后的 `/invitations` inbox、email-owner list、delivery observation 与现有 Identity method 管理。

**线性流程**：manager 签发并看到/复制由当前 origin 与 invitation ID 派生的准确链接 → 收件人打开链接 → 认证前只看到三种登录方式 → Email 验证留在当前页，Google/GitHub 的 login transaction 只携带 UUID → 有效成功/失败/重放回准确路径，失去 cookie/state 绑定则回 root 并要求重开链接 → 登录后 owner-only read → non-owner/unknown 统一 unavailable，恢复只取决于 viewer 自己是否已有 Email method → 准确 owner 看 pending/accepted/revoked/expired → pending 时可用 `Not now` 只在当前 browser session 暂缓自动优先级并进入 chooser，显式 inbox/准确链接始终重新打开且不改变 authority → pending 且 Email proof 不足 10 分钟时在页内证明 → 显式 Accept → 完整导航到 `/` chooser。

**事务与隐私**：`external_login_transactions.invitation_id` nullable UUID、无 FK，并约束只能用于 `purpose='login'`。未认证 OAuth start 只接受有界且字段准确的 form；Server 从可信代理规则推导来源，PostgreSQL 按 global→source 顺序取 advisory lock、删除过期 transaction、裁决 live source/global cap，再创建 transaction，超限返回 429。Accept 在任何 Membership 写入前完成 session identity/method/revocation、准确 Email owner、邀请 identity 与终态的无时间授权；最后可能等待的 Membership insert 后只取一次 `clock_timestamp()`，同一个值校验 session/proof/expiry、写 accepted timestamp 与 SQL predicate。Revoke 在 manager 与邀请锁后同样只取一次 clock。owner/scope 成立后 expired→410、revoked→410、accepted→409；wrong User、unknown、cross-owner/cross-Space 始终统一 404。页面使用 `no-referrer`，认证前没有 preview。

**直接证据**：准确 URL 与 persisted-ID issue replay；login-only migration/no-FK；OAuth success/failure/replay/cookie binding、有界/准确 form、来源透传、并发 source cap 与过期行回收；targeted pending/terminal/current/former owner projection与所有 non-owner 组合统一；non-owner Accept 零 Membership 后果；Accept/Revoke 两连接等待跨越 wall expiry 后拒绝；Accept-vs-Revoke 单 winner；页面内 Email proof、Unknown reload、manager copy/same resend URL、通用 inbox fallback、无 pre-auth preview/no-referrer 与 public-process 准确链接旅程。live Google/GitHub 凭据缺失只作为 residual 报告，按 Issue #3 v2 不阻断。

**命令**：`go test ./internal/space/... ./internal/identity/... ./internal/server/...`；`./scripts/test-db ./internal/postgres/...`；`./scripts/test-db ./cmd/carry-server/...`；`mise exec node@24.19.0 -- pnpm --dir apps/web test`；`mise exec node@24.19.0 -- pnpm --dir apps/web typecheck`；`mise exec node@24.19.0 -- make check`。

**不做**：第二 locator/token/capability owner、FK、generic returnTo/open redirect、storage continuation、pre-auth preview、provider-profile-email authority、auto-accept、multi-email、OAuth parallel-tab redesign、角色矩阵/批量/域/配额邀请、Agent/Host/Work 改动、静态 hosting 产品或通用 router 重写。

### 节点 15 — Host 接入与持久 Agent 身份

**能力**：用户运行 `carry setup`、在浏览器确认这台机器后，在 Web 上看到这台 Host 及其上每个 Agent 的持久身份与派生在场。

**package 责任**：新的 `agent` owner（ID、Space、Space 内唯一归一化名字、确定性头像、人类 owner、Host 绑定、Active/Removed、创建时间；新身份的人类 owner 取自浏览器批准成员，Space 成员可选择所有 Active Agent）；`machine`（浏览器批准的接入、批准成员、可验证来源、进程在场）；`host`（发现本机具体 Agent 并上报，内容不提供 owner）；`host/pi`、`host/codex`（由 `internal/agent/pi`、`internal/agent/codex` 迁入）；`cli`（浅层 setup 表面）；`server`（Host 接入与 Agent inventory transport，不拥有身份/批准策略）；`postgres`；Web Agent 清单 feature。

**删除**：`internal/agent/pi/`、`internal/agent/codex/` 旧路径（迁移后同一节点删除）；`internal/cli/host/start.go::selectExecutor` 与 `internal/host/worker.go::Worker.Executor` 所表达的 Host 生命周期内固定单一 executor 组合，改为每个持久 Agent 的具体 adapter 组合；被 `carry setup` 取代的 `internal/cli/host/connect.go`、`internal/cli/host/connect_test.go::TestConnectPersistsFreshProofBeforeNetworkAndRecoversExactPending`、`internal/cli/host/command.go::NewCommand` 当前行 26 的 `connect` 注册、`connect_test.go::TestHostCommandExposesOnlyConnectStartAndDisconnect` 当前行 20 的 `"connect"` 断言，以及 `apps/web/app/features/user-session/machine-connect.tsx` 当前行 103 的 `carry host connect` 文案、`machine-connect.test.tsx` 当前行 54、85、`apps/web/e2e/machine-connection.spec.ts` 当前行 35；`e2e/harness_test.go` 当前行 274、279 的 shared `carry host connect` driver；`e2e/machine_connection_test.go::TestMemberConnectsRunsDisconnectsAndBrowserRevokesMachine` 的 connect/start/browser-approve 段（当前行 54–92、111–124、139–164）；`internal/host/native_execution_live_test.go` 当前行 10–11 的旧 `internal/agent/pi|codex` imports。`start.go` 的长驻 Host 入口保留并重构，`internal/host/execution.go` 的旧 understanding contract 由节点 19 删除，`internal/cli/host/disconnect.go` 的撤销旅程由节点 28 删除/替换。人用 CLI 登录、凭据与 Work 家族整体由节点 18 删除。

**生成目录**：`internal/postgres/dbsqlc/`、`apps/web/app/generated/`。

**线性流程**：具有连接 Host 权限的成员在 Web 设置选择 Add Host → 机器上运行 `carry setup` → 该成员在浏览器核对并确认这台机器 → Host 上报它暴露的具体 Agent → 新 Agent 的人类 owner 取自该批准成员 → Web 显示 Host、Agent 与 owner 清单。

**研究问题**：一台 Host 上的具体 Agent 如何被发现并绑定成一个持久身份，并把新身份的人类 owner 只取自准确的浏览器批准成员，使 Host 掉线与重复发现都不改变已有身份？**反证问题**：在哪种真实情况下（重装、克隆机器、同机多进程、改名、第二名成员重新接入）同一路径会产生重复身份、冒充另一个 Agent、改写已有 owner，或复活 Removed 身份？

**权限 / 失败 / 直接证据**：只有具有连接 Host 权限的批准成员能成为新 Agent 的人类 owner；setup shell 与发现内容不能提供 owner；Host 不提交名字，PostgreSQL 分配不可变 Machine ordinal 并自动产生 Space-unique 的 `Pi`/`Codex` 配对名字，不存在冲突建议协议；两次 `setup` 不产生重复 Agent；第二名成员重新接入时已有 Agent 的 owner 不变而只对真正的新身份使用当前批准者；Removed 不被发现复活；杀掉 Host 后清单仍显示 Agent 且标为不在线；未确认的接入不授予任何权限；真实机器上完成一次 setup 并在浏览器看到清单。

**命令**：`go test ./internal/agent/... ./internal/machine/... ./internal/host/...`；`make test-db`；`pnpm --dir apps/web test`；`make check-product`。

**不做**：per-Agent owner 选择界面、完整 Host/Agent 移除确认、受影响 Work 交接与离场 UX（节点 28；节点 15 仍须让当前 Machine revoke 原子地把绑定的 Active Agent 置为 Removed）、模型选择界面（节点 16 判定）、Agent 与 Work 的关系、任何 provider 目录。

### 节点 16 — 选定 Agent 的对话与可恢复会话

**能力**：用户在一个输入框里选择当前可访问的 Agent 对话，Conversation 在第一条消息后固定该 Agent，并在 Host 重启后仍可继续。

**package 责任**：`conversation`（固定的目标 Agent 与可选模型、消息与受众）；`run`（目标 Agent、claim、lease、fence）；`host`（拉取、本机私有的 provider 续接句柄持久化）；`host/pi`、`host/codex`；`server`（Conversation member transport 与 Agent/Host claim/commit transport，不拥有消息/claim 策略）；Web 对话 feature。

**删除**：`internal/postgres/conversation_reply.go::{ClaimConversationReply,RenewConversationReply,lockActiveReplyMachine,fixedReplyContextRange,loadFixedReplyContext}` 与 `CommitConversationReply` 当前行 124–180、186–304 的 Machine-target claim/reply 权威，明确保留当前行 181–185 的 delegation call 和 `createDelegatedWork`（306–328）到节点 17；完整替换 `internal/server/machine_conversation_api.go` 与 `internal/server/machine_conversation_api_test.go` 的 Machine-target claim/renew/commit transport；`apps/web/app/features/conversation/conversation-panel.tsx::{ConversationPanelProps,ConversationPanel}` 与 `use-conversation.ts::useConversation` 中仅按 Space、未固定 Agent 的入口及其 `conversation-panel.test.tsx` 断言；`internal/postgres/conversation_reply_integration_test.go` 中 `TestConversationReplyConcurrentClaimHasOneWinner`、`TestConversationReplyContextIsFixedAcrossRecoveryAndBounded`、`TestConversationReplyRenewAndFirstCommitRejectLostAuthority`、`TestRevokedMachineCannotClaimPrivateConversationReply`、`TestConversationReplyConcurrentCommitAndCompletedReplayAreReplyOnce`；`internal/postgres/space_member_removal_integration_test.go::TestRemovalCompletionPreventsConversationReplyClaimRenewAndFirstCommit` 的 `claim`/`renew` subtests 与 stale reply-authority assertions（当前行 513–559、568–569、576–599，排除 Node 29 的 removal call 570–575 与 Node 17 的 `first commit` delegation subtest）；`internal/host/worker_test.go` 当前行 128–268 的 private-reply claim/renew/cancellation/fairness 断言；以及 `e2e/private_conversation_test.go::TestMemberTalksPrivatelyAndDelegatesSharedWork` 的 Host claim/reply 段与 `assertPrivateConversationEvidence` 的 Conversation-only 断言（当前行 29–200）。delegated Work 创建与隐私断言（当前行 201–308）保留到节点 17 替换。`internal/host/conversation_reply.go` 的 reply prompt/parser 与测试保留，Node 17 只替换其中 delegation-specific 部分。`internal/postgres/conversation.go` 的消息持久化不由这条删除预算误删。

**生成目录**：`internal/postgres/dbsqlc/`、`apps/web/app/generated/`。

**线性流程**：用户选择 Agent 并发送第一条消息 → Conversation 固定该 Agent → 拥有该 Agent 的 Host 拉取并 claim → 该 Agent 回复 → 用户稍后回来继续 → Host 用本机句柄或有界历史继续同一个 Conversation。

**研究问题**：Pi 与 Codex 各自的会话续接句柄有什么第一方语义（有效期、失效表现、能否跨进程重建），Host 应保存哪一个最小事实来恢复？**反证问题**：哪种真实失效会让"继续"静默产生一个失忆的新会话，而界面仍然显示连续？

**权限 / 失败 / 直接证据**：Conversation 固定后换 Agent 只能新建；目标 Agent 离线时界面显式说明且不回退到别的 Agent；删除本机句柄后仍能继续对话且用户可见内容不丢；并发两个 Host 拉取同一条消息只有一个 winner；真实 Pi 与 Codex 各跑一次。

**命令**：`go test ./internal/conversation/... ./internal/run/... ./internal/host/...`；`make test-db`；`pnpm --dir apps/web test`；`make check-product`。

**不做**：公开 Session API、通用 Session owner、Work 创建、多 Agent 协作。

### 节点 17 — Web 经 Agent 创建带两位负责人的 Work

**能力**：用户在对话里交办，或用 `Create with <Agent>` 表单提交，由 Agent 创建一个有人类负责人和 Agent 负责人的 Work。

**package 责任**：`conversation`（对话消息与表单提交形成的准确 target-Agent 请求、等待与恢复）；`run`（目标 Agent 的 pull/claim/lease/fence）；`host`（执行目标 Agent 并把当前 Run 的接下结果送回 Agent transport）；`work`（只接受当前 Agent Run 授权的创建事务与两位负责人）；`agent`（准确目标必须属于当前 Space 且为 Active）；`server`（成员请求 transport 与独立的 Agent 创建 transport，均不拥有创建策略）；Web Work 请求、等待与列表 feature。

**删除**：替换 `apps/web/app/features/works/create-work-form.tsx::{CreateWorkFormProps.onCreate,CreateWorkForm}` 的无 Agent direct-create 提交；`internal/server/work_api.go::{WorkCommands.CreateWork,createWorkRequest,workAPI.create}` 与 `workAPI.mount` 的 `POST /spaces/{spaceID}/works` 直接创建入口；`internal/work/work.go::{CreateCommand.CreatorUserID,CreateDigest}` 与 `internal/postgres/work.go::CreateWork` 的 member-authored 创建权限；`protocol/user/v1/openapi.yaml::createWork`、`apps/web/app/carry-api.ts::{createWork import,createWork}` 与 `apps/web/app/features/works/use-work-board.ts::useWorkBoard` 当前 direct-create 分支（当前行 119–143）；`internal/server/work_api_test.go::{TestCreateWorkTakesOwnerFromAuthenticatedMember,TestCreateWorkRejectsCallerNominatedOwner}`、`internal/postgres/work_integration_test.go::TestConcurrentWorkCreationReturnsOneDurableWork`、`internal/server/api_test.go::unavailableWorkCommands.CreateWork`、`internal/server/work_api_test.go::recordingWorkCommands.CreateWork`、`internal/postgres/migration_integration_test.go` 当前行 78–81 的 direct member fixture；`internal/postgres/conversation_reply.go::CommitConversationReply` 当前行 181–185 的 direct delegation call 与 `createDelegatedWork`（306–328）；`internal/conversation/conversation.go::{ReplyCandidate.DelegationGoal,CommitReplyResult.CreatedWorkID}` 与 `NormalizeReplyCandidate` 当前行 128–150 的 delegation-goal/digest 分支（保留 reply candidate 本身）；`internal/host/conversation_reply.go` 的 `ConversationReplyOutputSchema.delegation_goal`、`conversationReplyInstruction` delegation 规则、`ParseConversationReply` 的 `DelegationGoal` 分支，以及 `conversation_reply_test.go::{TestConversationReplyPromptContainsOnlyOrderedUntrustedContent,TestParseConversationReplyRequiresExactNullOrGoalOutput,TestConversationReplyOutputSchemaRequiresOnlyBothFields}`、`internal/conversation/conversation_test.go` 当前行 51–68、`internal/host/native_execution_conformance_test.go` 当前行 52、71、89、`e2e/testdata/privateconversationpi/main.go` 当前行 39、73–75 的 delegation output 断言/fixture；`internal/postgres/conversation_reply_integration_test.go::TestConversationDelegationCreatesOneSharedWorkWithoutPrivateSource`；`internal/postgres/space_member_removal_integration_test.go::TestConversationReplyCommitBeforeRemovalIsRetainedAndTransferred` 的 delegation commit/Work assertions（当前行 450–461、483–493、501–510）与 `TestRemovalCompletionPreventsConversationReplyClaimRenewAndFirstCommit` 的 `first commit` delegation subtest；`internal/host/worker_test.go::TestWorkerCommitsPrivateReplyAndDelegationCandidate`；`internal/postgres/run_integration_test.go::TestIdempotentWorkCreateReplayReturnsCurrentFacts`；`e2e/private_conversation_test.go::assertPrivateConversationEvidence` 的 delegated Work 创建、单例、隐私与 Run 断言（当前行 201–308）；`e2e/durable_work_test.go::TestBrowserCreatesDurableWorkWithoutStoringBearer` 的直接 Browser create 旅程；`apps/web/app/carry-app.test.tsx` 的 `"reuses the same Work identity after a create response is lost"`、`"reuses a pending Work identity after remount"` direct-create 用例，以及 `apps/web/e2e/first-durable-work.spec.ts` 当前行 36–44 的 direct-create 段。`work-input.ts` 的通用文本校验保留并复用于 Agent-mediated 请求。

**生成目录**：`internal/postgres/dbsqlc/`、`apps/web/app/generated/`。

**线性流程**：用户在对话里交办或在表单里选定 Agent 并提交 → Conversation 持久化准确成员、目标 Agent 与结构化请求，页面显示等待谁 → 拥有目标 Agent 的 Host 拉取并 claim → Agent 接下后用当前 Run 权威调用独立创建 transport → Work 事务从请求与 Run 推导两位负责人并为同一请求只创建一次 → 页面由同一请求恢复并显示 Work；Agent 要求澄清或拒绝时显示准确结果而不创建 Work。

**研究问题**：Conversation 如何拥有两条 Web 入口的最小 target-Agent 请求，使进程/响应丢失后仍可恢复，而 Work 只在 Agent 当前执行明确接下时出现？**反证问题**：哪种失败或重放会让 Server 直接创建、主动推送、产生幽灵 Work、同一请求创建两份，或让用户以为已建立但 Agent 从未接下？

**权限 / 失败 / 直接证据**：成员 transport 只能写请求；Agent 创建 transport 必须证明准确 target Agent、当前 Run/attempt/lease/fence 和原请求成员仍是当前 Membership；Work 只接收成员明确交办的结构化目标，不保存可回读的私人消息来源关系；正文不能改变任何一位负责人；未选择 Agent 不能提交；Agent 离线或进程被杀时请求仍在且没有 Work；响应丢失后刷新恢复同一结果；请求成员 Membership 与成员移除使用冲突锁：创建先提交则移除事务承接该 Work，移除先提交则创建明确拒绝；并发/重放最多一份 Work；对话路径的人类负责人正是发出被接受消息的成员；真实浏览器各跑一次对话、表单、进程丢失与响应丢失。

**命令**：`go test ./internal/conversation/... ./internal/run/... ./internal/host/... ./internal/work/...`；`make test-db`；`pnpm --dir apps/web test`；`make check-product`。

**不做**：节点 18 才冻结的 Agent-facing `carry` 本机接口与无 Work create-only 路径；本节点只复用节点 16 的 target-Agent Host claim/commit 纵向链路。也不做计划与产出（节点 21）或协作（节点 20）。

### 节点 18 — 本机 Agent 的仅创建 Work 路径

**能力**：机器上已注册的 Agent 通过 `carry` 新建一个 Work，人类负责人是该 Agent 的人类 owner。

**package 责任**：`cli`（Agent 本机表面）；`host`（把调用归属到唯一一个已注册 Agent 并转发）；`machine`；`work`（常驻 create-only 授权校验与独立创建事务）；`server`（只提供独立 Agent transport handler）。

**删除**：完整删除作废的人用 CLI 垂直路径：`internal/identity/cli_login.go`、`cli_login_test.go`；`internal/cli/work/`、`internal/cli/userapi/`、`internal/cli/login/`、`internal/cli/credentialfile/`；`internal/server/cli_login_api.go`、`cli_login_api_test.go`、`internal/server/routes.go` 当前行 13–23 的 `CLICredentialAuthenticator` dependency 与 81–99 的全部 `/cli-logins`、`/cli-credentials`、`/identity/cli-credentials` 绑定；`internal/server/user_auth.go::{CLICredentialAuthenticator,userAuthenticator.cli,userAuthenticator.authenticate}` 当前 CLI bearer branch（当前行 15–17、87–93）与 `browser_session_api_test.go::{TestMachineRouteRejectsUserCredentialsEvenWithValidCertificate,memberSurfaceTestAPI}` 的 CLI-token dependency；`internal/postgres/cli_login.go`、`internal/postgres/queries/cli_login.sql`、`internal/postgres/cli_login_integration_test.go`；`protocol/user/v1/openapi.yaml::{beginCliLogin,pollCliLogin,cancelCliLogin,lookupCliLogin,approveCliLogin,denyCliLogin,listCliCredentials,revokeCliCredential,revokeCurrentCliCredential}` 与 `apps/web/app/generated/` 中对应生成 symbols（通过 regenerate 删除）；`apps/web/app/features/user-session/cli-login.tsx`、`cli-login.test.tsx`、`cli-credential-settings.tsx`、`cli-credential-settings.test.tsx` 与 `apps/web/app/carry-app.tsx` 当前行 4–5、121–123、203、229 的 CLI import/route/settings 入口；`cmd/carry-server/main.go::run` 当前行 209、230–235 的 CLI login/auth composition 与 `external_login_integration_test.go` 当前行 753–757、761–763 的 CLI composition/stubs；`internal/server/api_test.go` 当前行 41、74、98、113–116、197–204、264–272、287、296 的 CLI authenticator/testCLIBearer/shared composition 及 `browser_session_api_test.go` 当前行 23、122–126、214，`conversation_api_test.go` 当前行 35、67、93、122、153、181，`external_login_api_test.go` 当前行 75、227、232，`machine_connection_api_test.go` 当前行 57、127，`work_api_test.go` 当前行 95、132、160、171、179、196、208、231、258、279、296、316，`space_invitation_api_test.go` 当前行 62 的 CLI bearer consumers/helper dependencies；`cmd/carry-server/member_removal_integration_test.go` 当前行 66–70、112–120、154–163、198–211、239–246 的 CLI bearer/database fixture；`internal/cli/root.go::newRoot` 的 login/work imports 与 command registrations、`internal/cli/root_test.go::{TestRunBuildsFreshCommandTree,TestRunRejectsUnknownAndRemovedVersionCommands}` 的人用 command 断言；`e2e/durable_work_test.go::TestMemberCreatesMessagesAndReloadsDurableWork` 的人用 Work CLI 旅程；`apps/web/e2e/cli-login.spec.ts`；`e2e/native_execution_test.go::TestOwnerReviewsResultProducedThroughNativeExecution` 当前行 17、54–84、139–163 的 `credentialfile`/人用 Work CLI 依赖，保留其 Host execution 主体给节点 19。已存在 migration 的数据保留或前向删除方式由本节点研究和生产数据库门决定；不改写已依赖的历史。

**生成目录**：`internal/postgres/dbsqlc/`、`apps/web/app/generated/`。

**线性流程**：本机 Agent 调用 `carry` → Host 把调用归属到唯一一个已注册 Agent → Host 携带当前 Machine 与 Agent 认证事实调用独立 transport → Work owner 的事务锁读并校验该 Space 的常驻 create-only 权限与 Agent 人类 owner Membership → Work 出现在 Web 上，两位负责人明确。

**研究问题**：本机调用如何被归属到唯一一个已注册 Agent，并且只获得"新建 Work"这一项权限？（在此冻结 §5 的接口形状比较结论。）**反证问题**：同机上的另一个进程、另一个 Agent 或一个被复制的凭证，在什么条件下能用同一路径创建 Work 或读到已存在的 Work？

**权限 / 失败 / 直接证据**：无法归属到唯一 Agent 的调用被拒绝且错误类别与其他失败不合并；创建事务锁读并证明 Agent Active、Agent 的人类 owner 仍是同一 Space 的当前成员；owner Membership 已撤销是独立且确定的拒绝类别；真实 PostgreSQL 并发测试覆盖 local create 先提交则移除承接、移除先提交则 local create 拒绝；用同一路径尝试列出或修改已存在的 Work 被拒绝；移除 Host 后同一调用立即失去权限；真实 Pi 或 Codex 在本机跑通一次创建。

**命令**：`go test ./internal/cli/... ./internal/host/... ./internal/work/...`；`make test-db`；`pnpm --dir apps/web test`；`go test ./e2e/...`；`make check-product`。

**不做**：读取或修改已存在 Work 的通用命令、按内容分裂的命令、自然语言动作入口、把外部渠道动作交给 Agent CLI。

### 节点 19 — 一个 Agent 负责人推进 Work、评论与有界实时进展

**能力**：Agent 负责人真的推进一个 Work，用户看到它当前在做什么，并能评论。

**package 责任**：`work`（进展、评论、用户可见输出）；`run`（目标 Agent 的 claim、attempt、lease、fence）；`host`（绑定当前 Work/Run 并转发）；`host/pi`、`host/codex`（把旧 understanding 输出改为统一用户可见进展输入）；`cli`（复用节点 18 的同一结构化 Work-context 输入字段，不新增进展专用命令）；`server`（有界实时流 transport）；Web Work 详情 feature。

**删除**：历史 `internal/postgres/migrations/0005_simplify_execution.sql::runs_unresolved_work_idx` 在当前数据库里所表达的"一个 Work 只能有一个未解决 Run"live 约束（历史文件不改写，以前向 migration 替换）；完整删除 `internal/host/execution.go` 的 understanding/next_step contract；删除 `internal/work/work.go::{Work.Understanding,Work.NextStep}`；删除 `internal/run/run.go::{MaxUnderstandingBytes,MaxNextStepBytes,Claim.CurrentUnderstanding,Claim.CurrentNextStep,Claim.BaseUnderstandingVersion,CommitCommand,ValidateUnderstandingUpdate}`、`internal/postgres/run.go::CommitWorkUnderstanding`、`internal/postgres/queries/run.sql::{CommitCurrentUnderstanding,CreateWorkResultCheck}` 及 claim projection 中 understanding/next_step 字段、`internal/server/machine_api.go::{commitUnderstandingRequest,machineAPI.commitUnderstanding}` 与 `internal/server/routes.go` 当前行 287–289 的 Machine claim/renew/understanding mounts；重写并保留 `MachineRuns.FinishUnresolvedAttempt`、`finishAttemptRequest`、`machineAPI.finishAttempt` 与 `routes.go` 当前行 290 的 `/outcome` mount，使失败/Unknown 只接受准确 target Agent/attempt/fence，不把它当成被删除路径；删除 `internal/server/work_api.go::{workWire.Understanding,workWire.NextStep,workToWire}` 的 understanding/next_step wire mapping（当前行 65–66、323）及 `internal/server/work_api_test.go::TestWorkDetailBindsCurrentReviewIdentityToCurrentContent` 当前行 226、237 的 understanding/next_step fixture/response 断言（其余 review 部分仍归节点 22）；`internal/cli/host/machine_http.go::machineHTTP.Commit`、`internal/host/worker.go` 的 `UnderstandingUpdate` commit 分支；删除 `internal/postgres/work.go::{workFromCreateRow,workFromIdempotencyRow,workFromLoadRow}` 当前行 396、408、422 的 understanding/next_step row mappings；删除 `internal/postgres/queries/work.sql` 当前行 43–44、62–63、218–219、251 的 understanding/next_step 投影与 lock fields，明确保留 review projections（当前行 65–78、107–143、162–198、227–242、256–289；223–226 的 needs_retry 仍由 Work 失败恢复拥有）到节点 22；删除 `apps/web/app/features/works/work-detail.tsx` 当前行 122–146 的 understanding/next_step 投影、`protocol/user/v1/openapi.yaml` 当前行 2239–2270 的 understanding/next_step schemas，明确保留 `internal/work/work.go::{AcceptReviewCommand,ReviewContentDigest,ReviewAcceptanceDigest}`、`internal/postgres/work.go::AcceptWorkReview` 与 `internal/postgres/queries/work.sql::{FindWorkReviewAcceptanceByIdempotency,LockWorkResultCheck,AcceptWorkResultCheck}` 到节点 22。按 Machine 所在 Space 领取任意 Work 的路径：`internal/postgres/run.go::ClaimRun`、`internal/postgres/queries/run.sql`、`internal/server/machine_api.go`、`internal/cli/host/machine_http.go`、`internal/host/worker.go::tryRun` 的 hand-written Machine HTTP 操作，以及 `internal/server/api_test.go::{TestMachineClaimReturnsCompleteWorkContextWithoutSecondCredential,TestMachineCommitBindsCertificateIdentity,TestMachineMutationRejectsMalformedAuthorityPath,recordingMachineRuns.CommitWorkUnderstanding}` 当前行 26–110、466–469；`internal/postgres/run_integration_test.go::{TestConcurrentClaimCreatesOneRunAttemptWithFixedMessages,TestBoundedRunInputsContinueWithoutOmission,TestCommitUpdatesWorkDirectlyAndLeavesLateMessageForNextRun,TestFailedAndUnknownRunsRequireExplicitRetry,TestConcurrentWorkRetryHasOneWinner,TestRetryIdempotencyCannotAuthorizeALaterTerminalRun,TestRevokedMembershipCannotRequestWorkRetry,TestExpiredAttemptRecoversOnceAndRejectsOldAuthority,TestAttemptAuthorityCannotCrossLeaseExpiryWhileWaitingForLock,TestMachineRevocationRejectsClaimAndCommit}`、`internal/host/worker_test.go` 当前行 16–96、270–325 的 Work execution 断言与 347–452 的 `recordingExecutor`/`recordingRunClient` UnderstandingUpdate shared stubs（保留并适配 `recordingExecutor.Reply` 当前行 379–394 的 Conversation 桩）；`internal/host/native_execution_conformance_test.go::TestNativeExecutorsShareOneUnderstandingContract` 当前行 16、19、41–42、73、91；迁移后的 `host/pi` `pi_test.go` 当前行 32、44 与 `host/codex` `codex_test.go` 当前行 26、50、65、176、186、212、239、246；`e2e/harness_test.go` 当前行 700 的 shared Pi understanding stub；`internal/cli/host/start_test.go` 当前行 71–72 的 `diagnosticExecutor` UnderstandingUpdate stub；`e2e/testdata/privateconversationpi/main.go` 当前行 42–44、77–84 的 `strictUnderstanding` fixture；以及 `e2e/host_recovery_test.go::{TestInterruptedHostWorkContinuesWithNewAttempt,waitForWorkUnderstanding}`；完整删除 `internal/host/execution_test.go`；删除 `internal/host/native_execution_live_test.go` 当前行 19、41 的 legacy understanding contract（Agent import 路径由节点 15 改）；`internal/run/run_test.go` 当前行 9–28 的 `ValidateUnderstandingUpdate`/`MaxNextStepBytes` tests；`internal/cli/host/http_test.go` 当前行 209、228–229、435、447–448 的 understanding claim/commit assertions；`apps/web/app/carry-app.test.tsx` 当前行 372、375、576–577、1176、1180、`apps/web/app/features/works/use-work-board.test.tsx` 当前行 275–276、`work-detail.test.tsx` 当前行 21–22 的 understanding/next_step fixtures；`e2e/native_execution_test.go::TestOwnerReviewsResultProducedThroughNativeExecution` 中 Machine/Space-wide claim 与 legacy understanding 执行断言（当前行 85–138；Node 18 独占 17、54–84、139–163，review acceptance 段 165–225 保留到节点 22）。历史 migration `0005` 不改写；本节点用前向 migration 删除/替换 live index。它们由准确 target-Agent pull/claim 取代；Conversation claim 仍只由节点 16 拥有。旧 result-check / review acceptance 由节点 22 替换。

**生成目录**：`internal/postgres/dbsqlc/`、`apps/web/app/generated/`。

**线性流程**：Work 创建后 → 拥有目标 Agent 的 Host 拉取并 claim → Agent 提交用户可见进展 → Work 页面显示当前活动与有界实时流 → 用户评论 → 评论送到 Agent 负责人 → Agent 据此调整并继续。

**研究问题**：有界实时进展要保存哪些事实，才能让用户理解当前活动而不持久化完整 tool trace 或模型思维？**反证问题**：哪种真实 Agent 输出会让"有界"丢掉用户判断所必需的事实？

**权限 / 失败 / 直接证据**：过期 lease 的迟到写入在 SQL 里被拒绝；进程被杀后 Work 保持 Open 并显示可理解的状态；响应丢失记为 Unknown 且不自动重放；评论一定到达 Agent 负责人；真实浏览器观察一次完整推进。

**命令**：`go test ./internal/cli/... ./internal/host/... ./internal/run/... ./internal/work/...`；`make test-db`；`pnpm --dir apps/web test`；`make check-product`。

**不做**：多 Agent 协作（节点 20）、计划项与产出项（节点 21）、生命周期（节点 22）、Inbox（节点 23）。

### 节点 20 — 额外 Agent 会话与协作

**能力**：Agent 负责人在有用且可用时请另一个 Agent 参与同一个 Work，顺序或并行推进。

**package 责任**：`run`（目标 Agent、因果来源、扇出上限）；`work`（参与 Agent 的查询视图与协作请求校验）；`cli`（复用节点 18 冻结的同一结构化 Work-context 输入，不新增协作专用命令）；`host`（绑定当前 Work/Run 上下文并转发、拉取与 claim）；`server`（Agent transport 与查询 transport，不拥有协作/因果策略）；Web 参与者与并行活动展示。

**删除**：无。只有一个 Agent 的 Work 仍是有效产品旅程；节点 19 已独占旧 Machine/Space-wide Work claim 的删除。本节点新增明确的多 Agent 行为与测试，不删除或重复认领节点 17–19 的单 Agent 证据。

**线性流程**：Agent 负责人通过同一 `carry` Work-context 结构化输入请求协作 → 本机 Host 绑定当前 Agent/Run 权威 → Work/Run owner 记录目标 Agent 与因果来源 → 拥有该 Agent 的 Host 拉取并 claim → 两个 Agent 的活动同时出现在 Work 页面 → 负责人不变。

**研究问题**：如何在不引入 Assignment owner 的前提下表达"这次执行来自哪一次执行、哪位成员"并限制扩散深度与并发度？**反证问题**：什么情况下两个并发协作者会同时认为自己拥有 Work 持久真相的写权限？

**权限 / 失败 / 直接证据**：并发写入只有一个 winner；协作者的权限不超过发起者，也不越出 Work 边界；扇出上限由数据库强制；一个协作者失败不改变负责人；只有一个 Agent 时同一条旅程仍然成立；真实两个 Agent 各跑一次。

**命令**：`go test ./internal/cli/... ./internal/host/... ./internal/run/... ./internal/work/...`；`make test-db`；`pnpm --dir apps/web test`；`go test ./e2e/...`；`make check-product`。

**不做**：Server 推送、任务分派界面、按名字回退到其他 Agent、协作策略配置。

### 节点 21 — 计划项与产出项

**能力**：Work 显示一份由 Agent 负责人维护的有序计划，以及可以指向计划项的产出。

**package 责任**：`work`（有序计划项、产出项、二者关联及当前写者校验）；`run`（当前 Run 权威）；`cli`（复用节点 18 的同一结构化 Work-context 输入字段，不新增计划/产出命令）；`host`（绑定当前 Agent/Run 并转发）；`server`（Agent transport 的 wire-shape 校验与成员查询 transport）；Web Work 详情。

**删除**：无。旧 understanding / next_step 由节点 19 删除，旧 result-check / review acceptance 由节点 22 删除；本节点只在新的 Work 表示上增加计划项与产出项，不保留第三份阶段或结果真相。

**线性流程**：Agent 负责人通过同一 `carry` Work-context 结构化输入提交计划项与产出项 → Host 绑定当前 Agent/Run → Server transport 只校验 wire shape 并委托 → Work/Run owner 事务校验受众、当前写者/Run 与 expected version → Work 页面按顺序显示计划与产出及其关联。

**研究问题**：有序计划项在被频繁重排和替换时，如何保持顺序与关联稳定且仍然只是展示真相？**反证问题**：哪个字段或界面元素会诱使服务端把计划当成调度真相，或让 Agent 以为提交计划就会被执行？

**权限 / 失败 / 直接证据**：服务端从不执行计划项；并发重排只有一个 winner；产出项在其计划项被删除后仍可解释；界面在计划为空时仍然说真话；真实浏览器观察一次计划更新。

**命令**：`go test ./internal/cli/... ./internal/host/... ./internal/run/... ./internal/work/...`；`make test-db`；`pnpm --dir apps/web test`；`make check-product`。

**不做**：Plan/Step/Artifact owner、workflow 引擎、依赖图、进度百分比推算。

### 节点 22 — 产出验收与 Work 生命周期

**能力**：Work 的人类负责人可以验收准确的产出版本，在 Open 与 Paused 之间暂停/恢复，关闭 Work，并在需要时显式重开；这些动作不会把 Unknown 猜成结果，也不会复活旧执行权威。

**package 责任**：`work`（产出版本的验收事实、Open/Paused/Closed 生命周期与暂停/恢复/关闭/重开规则）；`run`（暂停、恢复、关闭和重开后的 claim/lease/fence 边界）；`postgres`（当前版本与并发 single winner）；`server`（成员 transport）；Web Work 详情。

**删除**：删除旧 result-check / review acceptance 真相：`internal/work/work.go::{ErrReviewNotCurrent,AcceptReviewCommand,Work.NeedsReview,Work.ReviewID,Summary.NeedsReview,ReviewContentDigest,ReviewAcceptanceDigest}`；`internal/postgres/work.go::AcceptWorkReview`；`internal/postgres/queries/work.sql::{FindWorkReviewAcceptanceByIdempotency,LockWorkResultCheck,AcceptWorkResultCheck}`（当前行 256–289）及 `FindWorkByCreateIdempotency`/`ListNewestWorks`/`ListWorksBefore`/`LoadWork` 的 `needs_review`/`review_id` 投影与 needs-you review filters（当前行 65–78、107–143、162–198、227–242）；`internal/server/work_api.go::{WorkCommands.AcceptWorkReview,workSummaryWire.NeedsReview,workWire.NeedsReview,workWire.ReviewID,workAPI.acceptReview}` 与 `/reviews/{reviewID}/accept` mount；`protocol/user/v1/openapi.yaml::acceptWorkReview` 与当前行 2243、2276–2280、2301、2330 的 review fields；`apps/web/app/features/works/work-list.tsx` 当前行 83–84、`work-page.ts` 当前行 20、`apps/web/app/carry-app.tsx` 当前行 309 的 needs-review projections；`apps/web/app/features/works/work-detail.tsx::{WorkDetailProps.onAcceptReview,WorkDetail}` 的 result-review 段（当前行 92–120）、`work-pending.ts::{MutationCommand.accept-review,pendingReviewIdentity}`、`use-work-board.ts::useWorkBoard` 当前 review-accept 分支（当前行 167–201）；完整替换 `internal/postgres/work_review_integration_test.go`；重写 `internal/postgres/space_member_removal_integration_test.go::TestWorkReviewAcceptanceAndRemovalHaveOneValidOrder` 的 review-specific side（当前行 368–371、373–384、398–400）；删除 `internal/server/work_api_test.go::TestNeedsYouQueryIsExplicitAndOwnerScoped`、`internal/server/api_test.go::unavailableWorkCommands.AcceptWorkReview`、 `internal/server/work_api_test.go::{TestWorkDetailBindsCurrentReviewIdentityToCurrentContent,TestAcceptWorkReviewUsesAuthenticatedOwnerAndIdempotency,recordingWorkCommands.AcceptWorkReview}`、`apps/web/app/features/works/work-detail.test.tsx` 的 `"lets only the responsible member accept the exact current result"`、`use-work-board.test.tsx` 的 `"reuses the exact acceptance identity when a lost request did not commit"` 与 `"accepts the exact Needs You result and reconciles a lost response"`、`work-pending.test.ts` 的 `"binds a pending review identity to the exact review without storing content"`、`work-list.test.tsx` 当前行 18 与 `apps/web/app/carry-app.test.tsx` 当前行 1179 的 review fixture，以及 `apps/web/e2e/result-review.spec.ts`，以及 `e2e/native_execution_test.go::TestOwnerReviewsResultProducedThroughNativeExecution` 的 review acceptance 段（当前行 165–225；Node 18 独占 member-request auth helper 139–163）。历史 migration 不改写，前向 migration 负责替换仍存的约束。

**生成目录**：`internal/postgres/dbsqlc/`、`apps/web/app/generated/`。

**线性流程**：人类负责人打开 Work → 验收一个准确产出版本，Work 可继续 Open → 选择暂停时新的 claim 和旧提交失去权威 → 从 Paused 显式恢复时只从当前持久事实产生新的执行机会 → 选择关闭时未来继续停止且页面保留全部事实 → 显式重开时保留两位负责人并产生另一份新执行机会；恢复与重开都不复活旧 Run、lease、批准、schedule 或 provider session。

**研究问题**：如何让产出验收与 Work 关闭分别表达"这个产出被接受"和"这份责任不再继续"，同时使暂停、恢复、关闭、重开在并发执行与 Unknown 外部动作存在时仍然说真话？**反证问题**：哪种真实旅程会因把验收等同关闭、或因重开复活旧授权而产生错误外部后果？

**权限 / 失败 / 直接证据**：只有当前人类负责人能验收、暂停、恢复、关闭和重开；验收绑定准确 output version，之后的修订不继承接受；关闭不把 Unknown 变成 Failed 或 Succeeded；暂停/关闭后旧写入在 SQL 中失败；并发恢复、关闭与重开各只有一个 winner；Agent 不可用时人仍可暂停或关闭；真实浏览器观察验收后保持 Open、Paused 恢复后新 Run 且旧 fence 仍失败、关闭后不再 claim、Closed 重开后新 Run 且旧 provider 权威不复活。

**命令**：`go test ./internal/work/... ./internal/run/...`；`make test-db`；`pnpm --dir apps/web test`；`make check-product`。

**不做**：Result owner、结果状态机、自动关闭、关闭审批流、恢复旧 provider session、从产出内容推导验收。

### 节点 23 — Inbox 与邮件投递

**能力**：Agent 负责人提出的需要人的事项出现在 Inbox，并在用户连接邮件渠道后自动投递。

**package 责任**：`work`（需要人的事项及其投递提交事实，一个 Work 可同时有多项）；`run`（当前 Agent 负责人执行权威）；`cli`（复用节点 18 的同一结构化 Work-context 输入字段，不新增通知命令）；`host`（绑定当前 Agent/Run 并转发）；`identity` 或 `space`（渠道连接归属，由研究确定）；`postgres`（投递幂等与 single winner）；`server`（Agent transport、Inbox transport 与投递 worker 组合，不拥有事项/投递策略）；邮件 outbound adapter；Web Inbox feature。

**删除**：无。节点 22 已删除旧 `needs_review` 单项表示；当前 Node 12 树没有多项 needs-human Inbox 或通知产品路径，本节点第一次增加该查询与投递旅程。

**线性流程**：Agent 负责人通过同一 `carry` Work-context 输入提出事项 → Host 绑定当前 Agent/Run → Work owner 校验并记录 → 事项进入 Inbox → 已连接邮件渠道时自动投递 → 记录 Succeeded/Failed/Unknown → 用户在产品内回应 → 事项解决并回到 Work。

**研究问题**：投递提交如何做到幂等，并把 Unknown 与失败记录得既可诊断又不污染 Inbox？**反证问题**：哪种投递失败模式会让用户以为事项已被处理，或让同一事项被重复送达？

**权限 / 失败 / 直接证据**：投递失败不改变 Inbox 状态；重复投递提交不产生第二封邮件；从邮件回到产品仍需登录与权限；私人内容不出现在邮件正文；真实邮箱收到一次。

**命令**：`go test ./internal/cli/... ./internal/host/... ./internal/run/... ./internal/work/... ./internal/server/...`；`make test-db`；`pnpm --dir apps/web test`；`make check-product`。

**不做**：逐条投递审批、通知 owner、活动流、摘要邮件、渠道内直接回复。

### 节点 24 — 飞书投递

**能力**：同样的事项在连接飞书后自动投递到飞书。

**package 责任**：`work`（复用同一份事项与投递提交事实）；`postgres`（复用投递幂等与 single winner）；飞书 outbound adapter；`server`（只组合同一 worker）；Web 渠道设置。

**删除**：无。节点 23 的设计冻结必须已经把投递事实放在 Work 并保持 adapter-neutral；若节点 24 需要先删除邮件专用产品分支，说明节点 23 未完成，不能用本节点补救。

**线性流程**：用户连接飞书 → 新事项自动投递 → 记录结果 → Work 与 Inbox 不因渠道而改变。

**研究问题**：飞书的第一方投递语义（幂等、重试、失败可见性）与邮件有何不同，需要哪一个额外事实？**反证问题**：哪种飞书侧失败会被误当作成功？

**权限 / 失败 / 直接证据**：两个渠道共用同一份事项事实；一个渠道失败不影响另一个；真实飞书租户必须收到一次。受阻 canary 只能记录研究阻塞，不能满足节点关闭证据。

**命令**：`go test ./internal/work/... ./internal/server/...`；`make test-db`；`pnpm --dir apps/web test`；`make check-product`；执行 Node 24 Issue 冻结的真实飞书租户 canary 命令并观察投递。

**不做**：机器人对话式操作、渠道内授权、第三个渠道。

### 节点 25 — Agent 设定的一次性与周期继续

**能力**：Agent 负责人为一个 Work 设定一次未来继续或按天/按周的周期继续，用户能看到并在需要时暂停或取消。

**package 责任**：`work`（继续时间事实、当前 Agent 负责人校验、到期资格与唯一触发语义）；`run`（当前 Agent 执行权威）；`cli`（复用节点 18 的同一结构化 Work-context 输入字段，不新增 schedule 命令）；`host`（绑定当前 Agent/Run 并转发）；`postgres`（数据库时间与 single winner）；`server`（Agent transport 与到期 worker 组合，不拥有继续策略）；Web Work 详情的只读显示与暂停/取消。

**删除**：无。当前 Node 12 树没有定时产品路径；本节点第一次增加 Work-owned continuation，不预设临时代码供未来清理。

**线性流程**：Agent 负责人通过同一 `carry` Work-context 输入设定继续 → Host 绑定当前 Agent/Run → Work owner 校验并记录 → 数据库时间到期 → 唯一 winner 触发一次新的执行 → 结果与下次时间显示在 Work 页面 → 关闭 Work 停止未来继续。

**研究问题**：周期推进如何由数据库时间裁决唯一 winner，并在时区与夏令时边界上既不跳过也不重复？**反证问题**：哪种时区、夏令时或长时间停机组合会产生跳过、重复或雪崩式补偿？

**权限 / 失败 / 直接证据**：并发到期只有一个 winner；停机恢复后不重放堆积的过期时间点；关闭 Work 后不再触发；用户看到的下次时间与数据库一致。

**命令**：`go test ./internal/cli/... ./internal/host/... ./internal/run/... ./internal/work/... ./internal/server/...`；`make test-db`；`pnpm --dir apps/web test`；`make check-product`。

**不做**：通用人类日程编辑器、为未使用的人类编辑保留 API、调度 owner、cron 表达式。

### 里程碑门 — 真实的非代码端到端旅程

不是节点，是一次不可跳过的检验：在没有预置数据的环境里，一个新用户注册、建 Space、接入一台 Host、与一个 Agent 对话、产生一个非代码 Work、看到协作与计划、通过 Inbox 与邮件回应、经历一次继续，并关闭它。任何环节需要人工修数据库、手动补事实或解释"正常情况下应该"，都算未通过，缺口回到对应节点。

### 节点 26 — GitHub 仓库授权与 code-to-PR

**能力**：人类负责人显式授权一个仓库后，Work 能产生一次真实的、有后果的外部改动直到 PR。

**package 责任**：`work`（外部提交请求、当前 Agent 执行权威与人类批准）；`cli`（复用节点 18 的同一结构化 Work-context 输入字段，不新增 GitHub 动作命令）；`host`（绑定当前 Agent/Run 并转发请求）；`identity` 或 `space`（仓库授权归属，由研究确定）；GitHub outbound adapter；`server`（成员授权/批准 transport、Agent transport 与 outbound worker 组合，不拥有外部授权策略）；Web 授权与结果展示。

**删除**：无。当前 Node 12 树没有 GitHub repository authorization 或 code-to-PR 生产路径；本节点新增独立授权与具体 adapter，不把 GitHub 登录复用成仓库权限。

**线性流程**：人类负责人授权仓库 → Agent 通过同一 `carry` Work-context 输入提出外部改动请求 → Host 绑定当前 Agent/Run → Work owner 记录准确请求 → 人类负责人批准 → Work 事务消费当前批准并记录准确提交事实 → Server 组合的 GitHub adapter 在事务外执行并记录 Succeeded/Failed/Unknown → PR 链接与检查结果显示在 Work 上。

**研究问题**：一次外部提交如何绑定真实成员、准确目标、准确参数和当前批准，并在超时后可判定地区分成功、失败与 Unknown？**反证问题**：哪种真实 GitHub 失败或重放会造成重复 PR 或重复副作用？

**权限 / 失败 / 直接证据**：GitHub 登录本身不授予任何仓库权限；批准过期后不能执行；重放不产生第二个 PR；Unknown 不被自动重试；真实仓库产生一次 PR。

**命令**：`go test ./internal/cli/... ./internal/host/... ./internal/work/... ./internal/server/...`；`make test-db`；`pnpm --dir apps/web test`；`make check-product`。

**不做**：仓库托管、CI 编排、按 Git 分类 Work、代码审查产品。

### 节点 27 — 干净安装、发布与升级

**能力**：从干净环境部署一次 Carry Server 与一台 Host，并完成一次带 migration 的升级和一次备份恢复演练。

**package 责任**：release pipeline 与 `.github/workflows/`；`scripts`；`compose.yaml`；`cmd/carry-server` 与 `cmd/carry` 的版本注入。

**删除**：无预设删除。当前 `Makefile` 与 CI 都有消费者；本节点研究若证明某个准确 target 或 job 已无消费者，必须在 Issue 设计冻结中列出路径后删除，不能使用"过时本地假设"作 catch-all。

**线性流程**：干净环境 → 部署 → 冒烟一次真实旅程 → 升级到新版本并跑 migration → 从备份恢复一次 → 同一条旅程仍然成立。

**研究问题**：哪一条最小的发布与升级链路能同时证明 migration 前向兼容与备份可恢复，并用容量 canary 判断 OAuth start 当前 global advisory lock 与每次全局过期清理的串行天花板是否仍满足真实负载？**反证问题**：哪种升级顺序会让运行中的 Host 与新 Server 的协议不兼容而无人察觉，或哪种 start 流量会让当前全局串行 admission 在正确限流之前先成为可用性瓶颈？

**权限 / 失败 / 直接证据**：升级期间已接入的 Host 行为可预测；备份恢复后 Work 真相不丢；发布产物有 checksum 与来源记录。

**命令**：`make check`；release workflow 的一次真实运行。

**不做**：多区域、蓝绿、自动扩缩、可观测性平台、离场（节点 28–30）。

### 节点 28 — Host / Agent 下线与 Work 交接

**能力**：Host 意外掉线或被紧急撤销、Agent 被移除时，Work 不丢失、不自动换负责人；人类负责人能把未来责任安全移交给另一名可用 Agent。

**package 责任**：`machine`（掉线、立即撤销与认证失效）；`agent`（Removed 生命周期与历史保留）；`work`（当前 Agent 负责人、交接事实、未来继续暂停）；`run`（旧 claim / lease / fence 失效与新负责人 Run）；`server`（Host/Agent revocation、Work owner transfer 与查询 transport，不拥有撤销/转移策略）；Web Host 设置、Work 详情与 Inbox。

**删除**：`internal/cli/host/disconnect.go`、`internal/cli/host/disconnect_test.go::TestDisconnectRetainsExactCredentialWhenServerIsUnreachable`、`internal/cli/host/connect_test.go::TestDisconnectLocalOnlyErasesAllLocalMaterialWithHonestWarning`、`internal/cli/host/command.go::NewCommand` 当前行 28 的 `disconnect` 注册与 `connect_test.go::TestHostCommandExposesOnlyConnectStartAndDisconnect` 当前行 20 的 `"disconnect"` 断言，以及 `e2e/machine_connection_test.go::TestMemberConnectsRunsDisconnectsAndBrowserRevokesMachine` 的 disconnect/revoke/local-only 段（当前行 93–110、125–136、165–168）的"撤销 Machine 后立即删除本地凭据"单步旅程。它们由先撤销未来权威、展示受影响 Work/Agent、保留 Unknown 与可恢复本地状态的下线旅程取代。

**生成目录**：`internal/postgres/dbsqlc/`、`apps/web/app/generated/`。

**计划移除流程**：设置页请求移除 → 只读列出每个受影响的 Open/Paused Work 及其人类负责人 → 当前操作者可打开自己负责的 Work 逐份转移，不能替别人选择 → 撤销 Host / Agent → 未转移的 Work 保持旧 Agent 负责人、派生为 owner unavailable，并直接进入各自人类负责人的 Inbox → 历史仍可读。

**紧急与恢复流程**：立即撤销 Host / Agent → 新认证和新 claim 被拒绝，旧执行失去 Server 提交权，未来继续暂停 → Work 保持 Open，owner unavailable 直接进入当前人类负责人的 Inbox → 该负责人选择替代 Agent → PostgreSQL 原子转移当前 owner 并拒绝并发 loser → 替代 Agent 可用并 claim 后从持久 Work 事实启动新 Run、发布接手摘要并继续。

**研究问题**：如何在安全撤销不等待交接的前提下，原子地改变一份 Work 的 Agent 负责人、撤销旧 Run 的提交权、暂停未来继续，并给新 Agent 最小且完整的持久交接上下文？**反证问题**：旧 Host 在转移同时恢复、两个标签页选择不同替代 Agent、替代 Agent 在提交后立即掉线、已有外部提交结果 Unknown，或移除者不是受影响 Work 的人类负责人时，哪条路径会产生双 owner、迟到写入、越权转移、错误重放或隐藏的无人负责状态？

**权限 / 失败 / 直接证据**：只有当前人类负责人能转移；Host/Agent 移除权限不能改变 Work owner；替代 Agent 必须同 Space、Active 且在转移提交时可用，但接手摘要不是事务完成条件；紧急撤销不被移交失败阻塞；被撤销 Host 绑定的 Agent 转为 Removed；并发转移只有一个 winner；旧证书、Run、lease 与 fence 不能再提交；未来继续保持暂停直到新 Agent 明确恢复；替代 Agent 随后掉线时同一 Work 再次进入 owner unavailable；Unknown 仍为 Unknown；旧 Agent 保留为历史参与者且不向新 Agent 泄露私人 Conversation 或 provider session；真实浏览器完成一次意外掉线恢复、一次紧急撤销后转移、一次无权移除者尝试转移和一次替代 Agent 立即掉线。

**命令**：`go test ./internal/agent/... ./internal/machine/... ./internal/work/... ./internal/run/...`；`make test-db`；`pnpm --dir apps/web test`；`make check-product`。

**不做**：Host/Agent 移除页批量改变 Work owner、自动挑选替代 Agent、Agent 自行改变 owner、迁移 provider session、跨 Work 原子批处理、角色权限系统、通用 Assignment 或审计日志产品。

### 节点 29 — 成员离场与依赖责任收回

**能力**：成员可以在清空自己的责任后自愿离场；具有成员管理权限的人也能立即移除不合作成员，同时让其 Active Agent 和 Open/Paused Work 都落到有效状态。

**package 责任**：`space`（两项窄 Membership 权限、自愿离场、强制移除与一次性裁决）；`agent`（人类 owner 转移、Removed）；`work`（人类负责人转移与承接原因）；`identity`（未来访问撤销）；`postgres`（Membership/Agent/Work 与 Web-request/local create 串行化）；`server`（Membership、Agent owner 与 Work owner transfer transport，不拥有移除/承接策略）；Web 设置、Work 与 Inbox。

**删除**：删除 `internal/space/member_removal.go::{ErrRemovalSuccessorRequired,ErrRemovalSuccessorUnexpected,ErrRemovalSuccessorInvalid,RemoveMemberRequest.SuccessorUserID,RemoveMemberCommand.SuccessorUserID,NewRemoveMemberCommand}` 中 caller-selected successor 输入/校验；删除 `internal/postgres/membership.go::RemoveSpaceMember` 当前行 151–191 的 successor Membership 校验、required/unexpected 分支、`TransferRemovedMemberOpenWorks` 调用与 revoke payload，以及 replay successor 比较；删除 `internal/postgres/queries/membership.sql::{LoadMemberRemovalReplay.removal_successor_user_id,LockOpenWorksOwnedByMember,TransferRemovedMemberOpenWorks,RevokeSpaceMembership.successor_user_id}` 当前行 42–47、64–89；删除 `internal/server/space_invitation_api.go` 当前行 77–85 的 successor wire/command mapping、327–328 的 successor error mapping，与 `space_invitation_api_test.go` 当前行 78–120 的断言；删除 `protocol/user/v1/openapi.yaml` 当前行 1363–1374 的 successor 字段与 1378 的 transferred-Work response 语义并重新生成 Web client；删除 `apps/web/app/carry-api.ts` 当前行 396–406、`apps/web/app/features/user-session/member-settings.tsx` 当前行 24–28、55、211–231、237、289、323–359 的 third-party successor state/UI/transport，以及 `member-removal.test.tsx` 当前行 52–142；重写 `internal/space/member_removal_test.go::{TestNewRemoveMemberCommandBindsExactRemovalFacts,TestNewRemoveMemberCommandRejectsInvalidFacts}`；重写 `internal/postgres/space_member_removal_integration_test.go::{TestRemoveSpaceMemberWorklessTransferReplayAndRetention,TestRemoveSpaceMemberTransfersAllOpenWorkAtomically,TestRemoveSpaceMemberRejectsInvalidSuccessorAndFinalAuthorities,TestRemoveSpaceMemberSelfReplayAndConcurrentRemovalSafety,TestWorkCreationCanFinishWhileRemovalWaitsForMembership,removalCommand,assertRemovalState}` 的 successor/transfer 断言；重写 `TestWorkReviewAcceptanceAndRemovalHaveOneValidOrder` 的 removal/successor side（当前行 372、385–397、401–402）与 `TestConversationReplyCommitBeforeRemovalIsRetainedAndTransferred` 的 removal/successor side（当前行 462–481、494–500），以及 `TestRemovalCompletionPreventsConversationReplyClaimRenewAndFirstCommit` 的 removal call（当前行 570–575）。历史 migration `0013_member_removal.sql` 不改写；live schema/约束只用新前向 migration 替换。

**生成目录**：`internal/postgres/dbsqlc/`、`apps/web/app/generated/`。

**自愿流程**：成员请求离场 → 系统列出本人负责的 Open/Paused Work 与 Active Agent → 本人用普通规则逐份转移/关闭 Work，并转移/移除 Agent → 再次提交 → 数据库确认两类剩余均为零 → 撤销 Membership。

**强制流程**：具有成员管理权限的人选择目标 → 确认页只读列出目标 Open/Paused Work、Active Agent 与最后持有的窄权限，并说明执行者将承接 Work → 提交 → PostgreSQL 按固定锁顺序使目标 Agent Removed、把目标 Open/Paused Work 人类负责人改为执行者并记录原因；目标若是连接 Host 权限最后持有人则该项转给执行者；随后撤销 Membership → 承接 Work 进入执行者 Inbox → 执行者再按普通规则处理。

**研究问题**：Membership、Agent human owner、Work human owner 与 Web-request/local Agent 创建如何在一个权威锁顺序下串行化，使自愿离场可恢复、强制移除不等待目标、又永不产生无效负责人？**反证问题**：移除锁定期间的新 Work 创建、并发两次移除、移除重放、目标正在转移 owner 或执行者自身同时失去 Membership 时，哪条交错会制造无效 owner、双重承接或死锁？

**权限 / 失败 / 直接证据**：只有当前 owner 能普通转移自己的 Work/Agent；只有成员管理者能强制移除；强制移除者不能选择第三方且必须亲自承接现有 Work；目标 Active Agent 在同一事务转 Removed；Web-request 与 local create 都锁读 prospective human owner Membership，移除之后返回独立确定性拒绝；并发移除只有一个 winner 且重放不重复改 owner；自愿离场若会消灭任一窄权限的最后持有人则被阻塞，直到当前持有人把该项授予另一名 Active 成员；强制移除目标若是连接 Host 权限最后持有人则执行者原子承接该项；成员管理权限不会消失，因为执行者已持有；真实浏览器完成一次最后持有人恢复、一次被剩余责任阻塞的自愿离场和一次不合作成员强制移除；数据库并发 canary 证明没有 Open/Paused Work 或 Active Agent 指向已撤销成员。

**命令**：`go test ./internal/space/... ./internal/agent/... ./internal/work/... ./internal/identity/...`；`make test-db`；`pnpm --dir apps/web test`；`make check-product`。

**不做**：第三方 successor 选择器、orphan/pending owner 状态、组织角色层级、通用权限系统、Space 结束（节点 30）。

### 节点 30 — Space 结束

**能力**：具有成员管理权限的人可以在看清仍存在的 Work、Host、Agent、渠道和保留后果后结束一个 Space，未来访问与执行被撤销，但产品不谎称远端副本已删除。

**package 责任**：`space`（Space 生命周期与成员未来访问撤销）；`machine`、`agent`（未来认证/执行失效与历史保留）；`work`（只读检查是否仍有 Open/Paused Work，不改变 lifecycle）；`identity`（浏览器选择中移除已结束 Space）；`server`（Space-end preflight/commit 与历史查询 transport，不拥有结束/保留策略）；Web 设置。

**删除**：无。当前 Node 12 树没有 Space 结束生产路径；准确的前向数据与协议预算由本节点研究和生产数据库门冻结。

**线性流程**：设置页请求结束 Space → 展示 Open/Paused Work、连接 Host、Active Agent、渠道、Unknown 外部动作与保留说明 → 只要仍有 Open/Paused Work 就拒绝并链接到各自人类负责人处理 → 全部 Work 已 Closed 后再次确认 → 撤销未来 Membership、Machine/Agent 权威与继续时间，但不改写任何 Work lifecycle → Space 从普通选择中消失 → 授权历史仍可读取到冻结的保留边界。

**研究问题**：Space 结束在第一版究竟保留哪些最小历史、保留多久、谁还能读取，以及如何在要求各自 owner 先关闭 Work、又不承诺远端擦除的情况下撤销所有未来权威？**反证问题**：哪个真实合规、恢复或误操作场景会使立即硬删除、永久保留或不可恢复的结束确认不可接受？

**权限 / 失败 / 直接证据**：只有成员管理者能发起；任何 Open/Paused Work 都阻塞结束且管理者不能代替其 human owner 关闭；存在 Unknown 外部动作时界面说真话；并发 Host claim、Work 写入与 Space 结束由 PostgreSQL 拒绝迟到者；结束不改变 Work lifecycle、不声称远端进程/副本消失；真实浏览器先观察有 Open/Paused Work 的拒绝，再由 owner 关闭后结束，并证明后续登录、claim、schedule 和本机创建均失权。

**命令**：`go test ./internal/space/... ./internal/machine/... ./internal/agent/... ./internal/work/...`；`make test-db`；`pnpm --dir apps/web test`；`make check-product`。

**不做**：数据导出产品、租户迁移、远端擦除保证、复杂保留策略编辑器。

## 8. 仍需研究才能关闭的决策

下列问题在它们的节点写代码前必须用第一方证据关闭。无证据地决定其中任何一条是阻断。

| 门 | 问题 | 守护的节点 | 关闭它的证据 |
| --- | --- | --- | --- |
| G1 | slug 归一化、`/s/` 命名空间与冲突建议规则 | 13 | 真实显示名样本 + 数据库唯一性行为 |
| G2 | 邀请意图跨登录方式与设备的存活方式 | 14 | 三种登录的第一方回跳行为 + 一次跨设备 canary |
| G3 | 具体 Agent 的发现、身份防冒充与人类 owner 稳定性 | 15 | Pi/Codex 第一方进程事实 + 重装/克隆 + 两名成员先后接入同一 Host + Removed 再发现观察 |
| G4 | provider 会话续接句柄的语义与失效表现 | 16 | Pi/Codex 官方文档或源码 + 本机可复现执行 |
| G5 | Agent 本机接口形状、归属与权限校验（§5 的三种候选比较） | 18 | 三种候选各一次真实或明确受阻 canary |
| G6 | 有界实时进展要持久化的最小事实 | 19 | 真实 Agent 输出样本 + 用户可理解性检验 |
| G7 | 协作因果与扩散上限的表示 | 20 | 真实多 Agent 旅程 + 并发写入观察 |
| G8 | 投递幂等与 Unknown 的记录方式 | 23、24 | 邮件与飞书的第一方投递语义 |
| G9 | 周期继续的时区与夏令时边界 | 25 | 数据库时间行为 + 边界日期观察 |
| G10 | 外部提交的授权绑定与幂等 | 26 | GitHub 第一方 API 语义 + 一次真实 PR |
| G11 | Agent owner 转移、旧提交权失效、未来继续暂停、替代 Agent 随即掉线与 Unknown 交接的原子边界 | 28 | 当前 PostgreSQL/Run 权威考古 + 掉线、紧急撤销、并发转移、越权转移和替代 Agent 提交后掉线 canary |
| G12 | 成员移除与 Agent human owner 失效的并发负责人边界 | 17、18、29 | 当前 Membership/Agent/Work 事务考古 + 并发 Web-request/local create、负责人转移与移除 canary |
| G13 | Space 结束的保留、读取与未来权威撤销边界 | 30 | 当前 Space/Machine/Agent/Work 权威考古 + 有 Work、Host、渠道与 Unknown 的结束 canary |

以下事实已由用户决定，不再重开：七个 owner；Work 的两位负责人与"只能经 Agent 创建"；Server 不推送；Agent 身份与在场分离；无 onboarding 表单与无首个 Space 特例；slug 全局唯一且第一版不可改；邀请一次性、有期限、可撤销；渠道连接后自动投递；计划只是展示真相；里程碑门必须是真实非代码旅程。

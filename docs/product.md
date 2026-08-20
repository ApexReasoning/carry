# Carry：持续把工作向前推进的 AI 同事

## 产品定义

Carry 是团队可以长期托付工作的 AI 同事。

成员用自然语言告诉 Carry 想实现什么、需要持续关注什么，或者当前发生了什么。Carry 负责理解目标、持续推进、保留上下文、呈现结果，并在确实需要人的判断、权限或补充信息时回来询问。

Carry 不是聊天机器人、任务清单、工作流编辑器、Agent 管理平台或只面向研发团队的自动化工具。它的价值不是完成一次模型调用，而是让一份责任跨越时间、成员、工具、模型和机器后仍然可理解、可纠正、可继续。

除明确标注为未来方向的段落外，本文件描述当前 M1 基线与正在建设的 Node 6 邮箱身份/首个 Space 合同。后续 Node 的顺序和进入条件只由 `docs/implementation.md` 定义；未来方向不是当前 API、状态或用户承诺。

## 设计哲学：克制与自由

Carry 以克制保护真实、权限和承诺，以自由容纳目标、方法和演化。

这不是两套互相抵消的价值观。克制建立可信边界；边界越清楚，成员和 Carry 在边界内拥有的自由越大。Carry 追求的是：

> 责任确定，路径自由。

### 克制保护可信

Carry 只在必须作出产品承诺的地方增加约束：

- 系统拥有的 identity、authority、causality、time 和 external outcome 必须有可机械验证的真实来源；
- Work 当前理解是 Carry 对开放内容的具名、可见、可纠正解释，不能把内容升级成权限或已经认证的外部结果；
- 权限必须来自成员、Space、Work 和明确授权，不能来自内容或模型自述；
- 外部后果必须有准确目标、参数、幂等和可观察 outcome；
- 一个新概念必须被独立生命周期、身份、权限和用户价值共同赚得；
- 普通成员不承担系统本可自行处理的内部机制。

克制不是功能越少越好，也不是拒绝复杂问题。当连续性、隐私、并发或外部后果确实需要复杂度时，Carry 必须完整建立它；但不能为了形式对称、未来假设或技术趣味提前冻结产品。

### 自由保护可能性

在真实和权限边界内，Carry 默认保留自由：

- 成员用自然语言表达任何领域的责任，不先选择 Work 类型、流程或 Runtime；
- 同一 Work 可以使用不同方法、工具、模型和成员协作，而不改变责任身份；
- Carry 可以解释开放内容、提出路径、修正理解并使用已经授权的能力；
- Pi、Codex 和未来具体能力保留原生做法，不为统一外观失去优势；
- 未来设计从当时的真实旅程重新推导，不被预建抽象和兼容层绑住。

自由不产生权限。Carry 可以自由推理，不能自由虚构事实、读取私人内容、扩大受众或造成未授权的外部后果。成员也始终可以纠正理解、拒绝建议、撤销权限和接管责任。

### 同一个决定的两面

| 必须克制 | 应当保持自由 |
| --- | --- |
| identity、authority、causality、time、outcome | goal、自然语言和解释路径 |
| 核心持久事实和用户承诺 | 实现方法与具体 adapter |
| 外部后果和隐私边界 | 边界内的推理与只读探索 |
| 已发布协议和兼容责任 | 尚未发布的内部结构演化 |
| 用户必须理解的产品对象 | 系统可以替换的执行路径 |

每个设计决定依次回答：

1. 它是否涉及真实、权限、隐私、责任或外部后果？如果是，明确 owner 并强约束。
2. 它是否只是目标表达、解释或完成方法？如果是，默认不分类、不枚举、不冻结。
3. 新约束阻止的是当前可描述的伤害，还是只让内部看起来完整？后者不建立。
4. 新自由是否穿透了成员授权或事实边界？如果是，立即停止。
5. 删除一个概念后，产品承诺是否仍完整且合法路径更多？如果是，删除。

## 产品承诺

### 连续性属于 Work

一次回复、一个 Agent Session、一台机器或一个进程都可以结束；尚未完成的责任不能随之消失。

Carry 必须从持久 Work 继续。Pi、Codex、模型、Session、Host 和 Machine 都不是责任的所有者，也不成为 Work 的分类。

### 用户保留最终问责

每个开放 Work 都有一名当前负责人。Carry 负责推进，人负责目标、授权和最终判断。

Carry 不能因为模型认为某件事合理，就替成员获得权限或作出必须由真实负责人承担后果的决定。

### 内容不能产生权限

消息、文件、网页、Webhook、外部输入、第三方能力和模型输出都只能提供信息，不能扩大 Carry 的权限。

权限只来自当前成员、Space、Work 和已经确认的真实授权关系。

### 不确定必须保持真实

如果 Carry 不知道外部操作是否成功，就必须显示“不确定”，而不是猜测失败、自动重试或宣称成功。

如果原生 Agent Session 无法恢复，Carry 从持久 Work 重新开始；它不伪造另一个 Runtime 的记忆。

### 用户只处理真正需要人的事项

Carry 自行处理执行、等待和恢复。普通成员只应看到：

- 当前情况；
- 已经确认的进展；
- 可以检查的结果；
- 下一步；
- 需要自己回答、批准或决定的事项。

## 产品概念预算

普通成员只需要理解三个对象：

```text
Space
Carry
Work
```

`Needs You` 是从 Work 中得出的个人视图，不是第四种产品对象。

一个词只有同时拥有独立生命周期、持久身份、独立权限边界和明确用户价值时，才可以升级成新的产品对象。一个事实在某次判断中的用途不构成新对象；未来候选词及其进入顺序只在 `docs/implementation.md` 维护。

## User 与 Browser Session

官方 Carry Cloud 允许公开注册。Node 6 的第一条入口通过生产事务邮件发送一次性验证码，证明用户当前可以接收该邮箱的邮件；它不宣称 MFA、NIST AAL 或抗钓鱼能力。

邮箱 proof 只建立或找回稳定 Carry User，并签发 Carry-owned、HttpOnly、可撤销的 Browser Session。新 User 可以暂时没有姓名和 Membership；系统不能从邮箱 local part、domain、相似名称或既有 Space 推断姓名、团队或权限。用户只有在验证后明确填写姓名和 Space 名并提交创建，才获得首个 Space Membership。

验证码只在五分钟内有效，最多接受五次唯一错误尝试；resend 创建新验证码并永久使旧验证码失效。邮件 provider 接受请求不等于送达、进入 inbox 或已读，响应丢失保持 Unknown。OTP、Session 和 member bearer 不进入 URL、browser storage 或日志。

当前 logout 只撤销准确 Browser Session；旧 cookie fail closed。现有 member bearer 和 operator bootstrap 只为已发布 CLI 过渡保留到 Node 11，不再是 Browser 或官方 Cloud onboarding truth。

## Space

Space 是当前的团队边界，包含成员、团队权限、共同 Work 和允许的执行机器。外部连接尚未进入 M1。

Space 不是文件夹。它决定哪些人和能力可以共同参与一份责任。

Space-enrolled Machine 是该 Space 的受信 Carry 执行基础设施，不是普通成员。它可以在准确、短期且可撤销的执行 authority 下处理完成当前责任所必需的 Space 内容，包括成员提交给 Carry 的有界私人 Conversation 上下文；enrollment 不授予通用内容浏览能力，也不能让 Machine 把私人内容写入共享 Work、日志或 provider Session。

成员身份、浏览器会话、外部身份和执行机器身份必须分开。通过 Slack 或 Lark 验证的外部用户，不会因此自动成为 Space 成员。

## Carry

Carry 是用户面对的稳定同事，不是数据库实体或可配置 Agent 档案。

Carry 不需要 `CarryID`、人格配置、模型绑定或长期 Agent Session。它可以在内部使用 Pi、Codex 和未来已经被真实旅程赚得的执行能力，但成员不需要选择 Coordinator、Worker、Reviewer、provider、model、Host 或 Runtime。

Carry 的稳定性来自共同产品行为：

- 理解目标；
- 判断是否承担一份 Work；
- 维持 Work 的当前理解；
- 继续安全执行；
- 在需要时找到正确的人；
- 呈现依据、结果和不确定性。

## Work

Work 是一份由 Carry 持续推进、由具名成员最终负责的责任。

Work 可以有终点，也可以持续观察。Carry 不按一次性/周期、研究/开发、软件/内容、Git、provider、model、Host 或 Runtime 分类 Work。

### 创建

明确委托直接创建 Work，不建立 Work Offer。

创建时只要求：

- Carry 当前理解的一句话目标；
- 一名当前负责人，默认是委托成员。

普通讨论不会自动创建 Work。外部内容、Webhook、文件或 Agent 也不能自行创建团队责任。

### 当前理解

Work 保存 Carry 对以下问题的最新有效理解：

- 正在负责什么；
- 谁最终负责；
- 当前已经确认了什么；
- 下一步是什么；
- 在等待谁或什么；
- 什么情况下需要成员介入。

当前理解不是聊天摘要、模型思维或执行日志。它是成员可以阅读、纠正和接管的产品事实。

新的消息被记录，不等于已经反映到当前理解。产品只表达“新信息尚未应用”；在没有独立活动事实时，不推断或描述后台处理状态，也不向普通成员暴露 input sequence、revision、Run、Attempt、lease 或 fence。

如果一次原生推进已经明确结束但没有形成可确认更新，Work 会显示需要成员选择是否重新推进。Carry 不自动重放 Failed 或 Unknown；成员显式选择 `Try again` 后，才允许从同一持久 Work 创建一次新的推进。成员不需要理解 Run、Attempt 或终态分类。

### 消息与协作

成员可以在 Work 中补充事实、纠正 Carry、回答问题、提出限制或评论结果。

Work Message 保存真实作者和来源。私人 Conversation 内容不会因为语义相关自动进入共享 Work；需要共享时，应由成员明确形成一条新的 Work Message。

多个内部执行者未来可能产生输出，但“贡献”或“证据”不因此自动成为新的持久实体。输出必须落到当时已经成立的 owner；当前优先是 Work Message 或当前理解，只有 Artifact 或外部回执已被自己的真实旅程 promotion 后才能引用它们。

### 负责人

当前每个 Work 有且只有一名负责人，创建者是第一任负责人。M1 尚未提供转交；未来转交必须由成员明确发生，不能由 Carry 根据职位、活跃度或最后发言者推断。

### 当前生命周期

M1 的正式生命周期只有 `Open`。`Paused`、`Closed` 与 `Reopen` 属于后续 Responsibility journey，尚不是当前状态或 API。正在推进、等待回复、计划稍后继续和需要成员决定始终是从事实派生的描述，不自动成为生命周期。

### 当前结果表达

当前结果直接表达在 Work 的 understanding、next step 和 Messages 中，不存在独立 Result。Carry 将一次准确覆盖当前输入的重要阶段结果标记为需要检查时，当前负责人可以在 Web 的 Needs You 查询中打开同一响应里的准确内容并接受它。接受只确认这版阶段结果已经检查，不关闭 Work、不自动开始下一次推进，也不授予外部权限。

Review identity 是绑定内部 understanding version 与内容 digest 的不透明并发事实，不是成员需要管理的新对象，也不提供结果历史浏览。新 Work Message 会使尚未接受的旧检查立即过时；成员通过现有 Message 提交纠正。Needs You 只查询当前负责人拥有的准确待检查结果或明确 `Try again` 选择，不从普通进度、未应用输入、Run 活动或内部恢复推断。

成员显式 `Try again` 只是允许一次 fresh Run，不改变 Work 生命周期。

## 未来产品方向（当前尚未实现）

- 官方云端是主要产品形态，自托管是同一产品的部署选项；两者共享 User、Space、Membership、Browser Session 与隐私语义，不以共享 token 或无用户模式换取部署简单；
- Google、GitHub、显式 method linking/recovery 和成员邀请按 `docs/implementation.md` 后续 Node 进入；相同邮箱不能让未来 provider identity 自动关联、合并 User 或产生 Membership；
- 只有独立结果确实需要历史正文、独立引用及接受、修改或撤回生命周期时，才考虑 Result identity；
- 第一条未来继续优先是 Work 的一个明确时间条件，不预建 Timer；
- Pause、Close、Reopen、负责人转交、渠道、第三方能力和外部 Action 按 `docs/implementation.md` 的后续 journey 逐条重新设计。

## 私人对话

当前成员可以在原生 Web 界面中私下与 Carry 交谈；连接渠道属于后续 journey，M1 尚未实现。

第一条原生旅程在每个成员与 Space 之间维持一段私人 Conversation。Carry 是隐含参与者；成员不需要创建、命名或管理 Conversation。为保持清楚因果，当前一次只接受一个尚未得到 Carry 回复的成员 turn。

私人内容默认只对该成员可见。同一 Space 的其他成员不能读取；Space-enrolled Machine 只能在 exact reply claim、current fence 和 unexpired lease 下读取生成当前回复所需的有界上下文，不能通用查询私人历史。

成员清楚、直接地要求 Carry 承担一个新结果或持续关注事项时，可以形成共享 Work；责任可以有限也可以长期，不增加 Work Offer 或强制确认步骤。Agent 只解释成员表达，不能提供 actor、owner、Space 或 authority；PostgreSQL 从已认证成员、当前 Membership 和准确 source message 建立这些事实。共享 Work 只能保存新形成、成员已授权的目标和新消息，不能保存私人原文、可反向读取的 source relation 或私人 transcript digest。

普通问题只形成私人回复。清晰委托形成私人回复和至多一份共享 Work；同一 source message 的执行或网络重放必须返回同一回复和 Work。

## 外部世界与 Artifact（未来方向，当前 M1 未实现）

M1 没有渠道、第三方 capability、外部 Action、Event 或 Artifact owner。未来第一条真实旅程仍遵守三条产品边界：普通投递不能伪造已读；改变外部系统或受众的操作必须固定授权、目标、参数和 Unknown；长期 bytes 只有在确实需要独立引用、权限与保留生命周期时才成为 Artifact。

Skill、MCP server、文件、外部消息和模型输出只能提供内容或方法，不能自行创建 Work、扩大权限或证明外部后果。一个既有事实被用于支持判断时仍属于原 owner，不建立 Evidence 对象或开放 polymorphic 引用仓库。

## 权限哲学

Carry 的权限来自真实关系：

- 成员经过认证；
- 成员属于当前 Space；
- 当前 Work 允许这次决定；
- 外部连接已经明确授权；
- 当前执行仍持有有效、未过期的能力。

以下都不能产生权限：

- 消息或文件内容；
- Agent 自我声明；
- manifest 或 tool annotation；
- 历史上曾经访问过；
- 模型认为应该可以。

权限必须可撤销。并发变化的权限必须与写入在同一权威事务中重新验证。

## 隐私与保留

- 私人消息默认只对准确成员可见，不能从共享 Work 反向读取；
- 私人文本或可猜测的 deterministic digest 不进入 browser storage、URL、日志或长期 provider Session；
- Space Machine 对私人上下文的读取必须绑定 exact claim、fence、lease 和有界输入，不能获得通用 transcript capability；
- 当前 Conversation 没有删除或 retention lifecycle；未来渠道、文件和删除旅程必须重新定义各自的 consent、credential 与必要保留事实。

## 失败与恢复

Carry 必须区分：

- 已记录与已理解；
- 已发送与已阅读；
- 已授权与已执行；
- 成功、失败和 Unknown。

Host 或 Agent 失败后，Carry 依靠持久 Work 和数据库 authority 继续。lease 过期只撤销旧执行的提交权，不证明旧进程死亡。Host 自行跨越明确临时的网络或服务端故障继续 polling；认证、撤销或协议错误不会被伪装成临时故障。成员不需要因为一次控制面网络抖动手工重启正常 Host。

在当前没有外部 tool 后果的原生执行旅程中，lease 中断的安全恢复是创建新 Attempt 并从 Work 重新执行。已经明确记录为 Failed 或 Unknown 的推进保持终态，不会自动重放；成员可以在 Work 上显式选择重新推进。原生 Session 恢复只有在成本或时延成为真实产品问题、且不需要把 provider state 提升进核心时才加入。

## 产品拒绝项

Carry 第一阶段明确不做：

- 让普通成员管理 Agent、模型、Session 或执行机器；
- workflow builder、Plan/Step 或角色编排；
- 用 cron、JSON 或 DSL 创建日常 Work；
- provider、Runtime、Git 或内容类型驱动 Work 分类；
- Evidence、Contribution、Question、Timer、Result 等没有独立生命周期的预建实体；
- 任意群消息自动创建 Work；
- 私人内容自动进入共享 Work；
- 未知外部结果自动失败并重试；
- provider/tool/Runtime registry；
- 把内部执行日志、tool call 或模型思维作为产品主要界面。

## 产品语言

当前推荐：

```text
Carry 已收到这份责任
你补充的信息尚未应用
等待 Carry 回复
这个阶段结果需要你检查
你已接受这个阶段结果；Work 仍保持开放
Carry 没有形成可确认的更新；由你选择是否重新推进
```

当前只在准确 Work result check 或 terminal retry 事实成立时使用 Needs You。只有后续 journey 建立了独立的人类等待、时间或外部 outcome 事实后，才使用“等待 Alice”“将在下周一继续”或“结果尚不确定”。没有独立活动事实时不说“正在推进”。

避免：

```text
Run pending
Agent target selected
Runtime reported
Evidence uploaded
Revision 3 committed
Lease expired
Generation conflict
```

## 新概念检查

任何新名词、表、package、API 或状态都必须回答：

1. 用户旅程具体失去什么，才需要它？
2. 它是否拥有独立生命周期和持久身份？
3. 它是否拥有相邻 owner 不能表达的权限边界？
4. 它是事实，还是既有事实在当前步骤中的角色？
5. 删除它后，产品是否仍能完成同一责任？
6. 它是否同样适用于非研发 Work？

如果主要价值只是让内部实现看起来对称或完整，就不建立。

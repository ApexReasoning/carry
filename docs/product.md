# Carry：持续把工作向前推进的 AI 同事

## 产品定义

Carry 是团队可以长期托付工作的 AI 同事。

成员用自然语言告诉 Carry 想实现什么、需要持续关注什么，或者当前发生了什么。Carry 负责理解目标、持续推进、保留上下文、呈现结果，并在确实需要人的判断、权限或补充信息时回来询问。

Carry 不是聊天机器人、任务清单、工作流编辑器、Agent 管理平台或只面向研发团队的自动化工具。它的价值不是完成一次模型调用，而是让一份责任跨越时间、成员、工具、模型和机器后仍然可理解、可纠正、可继续。

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

一个词只有同时拥有独立生命周期、持久身份、独立权限边界和明确用户价值时，才可以升级成新的产品对象。一个事实在某次判断中的用途，不构成新对象。

例如：

- Artifact 可以是长期保存的不可变文件；
- “evidence”只是 Message、Artifact、外部回执或数据库事实支持一次判断时扮演的角色；
- 因此 Carry 不建立 Evidence 对象、Evidence API 或 Evidence owner。

相同规则适用于 Result、Question、Timer、Event、Delivery、Plugin 等候选词：先完成真实旅程，只有相邻事实无法自然表达独立生命周期时才提升概念。

## Space

Space 是团队边界，包含成员、团队权限、共同 Work、已确认的外部连接和允许的执行机器。

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

每个开放 Work 有且只有一名当前负责人。创建者默认是第一任负责人。

负责人转交必须明确发生并保留真实任期；Carry 可以建议，不能根据活跃度、职位或最后发言者自动改变负责人。

### 生命周期

Work 的正式生命周期只有：

```text
Open
Paused
Closed
```

“正在推进”“等待回复”“计划稍后继续”“需要你决定”是从事实得出的活动描述，不是新的生命周期状态。

Pause 阻止新的执行。Close 明确结束旧责任。继续一个 Closed Work 必须显式 Reopen，不能由旧执行或旧时间约定偷偷恢复。

### 结果

成员需要检查的是 Work 中的结果，不是另一个必须学习的顶层对象。

第一条结果旅程应优先使用现有 Work 内容表达。只有当一个结果确实需要独立版本、接受/要求修改/撤回的生命周期，并且这些规则不能自然属于 Work 时，才在 Work 内提升独立 Result identity。

接受一个阶段结果不会自动关闭仍有后续责任的 Work。

### 未来继续

时间是 Work 继续行动的条件，不是独立产品。

第一条未来继续旅程优先保存 Work 的下一次明确继续时间。只有一个 Work 确实需要多个可独立取消、重复和审计的时间约定时，才提升 Timer identity。

成员使用自然语言表达时间；不需要 cron、JSON、DSL 或工作流编辑器。

## Needs You

Needs You 只展示某个成员必须亲自处理的 Work 事项，例如：

- 回答 Carry 无法自行判断的问题；
- 检查或接受一个重要结果；
- 批准会改变外部系统的操作；
- 接受负责人转交；
- 决定继续、暂停或关闭；
- 处理真实的不确定外部结果。

Needs You 不显示 Agent 重试、Runtime 状态、lease、fence 或技术恢复。

## 私人对话

成员可以在原生界面或已连接渠道中私下与 Carry 交谈。

第一条原生旅程在每个成员与 Space 之间维持一段私人 Conversation。Carry 是隐含参与者；成员不需要创建、命名或管理 Conversation。为保持清楚因果，当前一次只接受一个尚未得到 Carry 回复的成员 turn。

私人内容默认只对该成员可见。同一 Space 的其他成员不能读取；Space-enrolled Machine 只能在 exact reply claim、current fence 和 unexpired lease 下读取生成当前回复所需的有界上下文，不能通用查询私人历史。

明确的自然语言委托可以直接形成共享 Work，不增加 Work Offer 或强制确认步骤。Agent 只解释成员表达，不能提供 actor、owner、Space 或 authority；PostgreSQL 从已认证成员、当前 Membership 和准确 source message 建立这些事实。共享 Work 只能保存新形成、成员已授权的目标和新消息，不能保存私人原文、可反向读取的 source relation 或私人 transcript digest。

普通问题只形成私人回复。清晰委托形成私人回复和至多一份共享 Work；同一 source message 的执行或网络重放必须返回同一回复和 Work。

## 外部世界

Carry 只在真实旅程出现时引入必要的物理事实。

### 普通消息

沿已有、已授权会话关系发送一条已经保存的消息，是普通投递。渠道接受不代表对方已经阅读。

### 新的外部后果

创建、修改、删除、付款或扩大受众属于 Action。Action 必须固定准确目标和参数，并使用真实授权。响应丢失时保持 Unknown。

Action 的独立身份来自它的授权和外部后果生命周期；它不是因为一次 tool call 就自动成立。

### 没有明确目标的外部事实

只有第一条真实、授权采集且没有明确 Conversation 或 Work 目标的来源出现后，才建立 Event owner。Event 不能自行创建 Work 或授予权限。

### 第三方能力

第三方 Skill、MCP server 或其他能力是 Carry 的实现能力，不是新的同事，也不拥有 Work。

声明需要权限不等于获得授权。长期 credential 不进入 Agent prompt、内容包或模型输出。会改变外部系统的调用仍然遵守 Action 规则。

在第一条真实安装与调用旅程前，不冻结 Plugin marketplace、两种 MCP transport、通用 tool registry 或 provider registry。

## Artifact 与依据

Artifact 只在 Carry 必须长期保存和引用不可变 bytes 时成立，例如附件、安装包或可下载交付物。

Artifact 保存 bytes 的 identity、digest、类型、来源、权限和保留策略。它不保存业务判断，也不因为被用于证明某件事就变成另一份 Evidence。

支持判断的依据可以是：

- 一条 Message；
- 一个 Artifact；
- 一个外部只读回执；
- 一个 Action outcome；
- 一个数据库并发事实。

需要记录关联时，拥有该判断的 owner 保存准确引用；不建立开放 polymorphic Evidence 仓库。

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
- 外部 thread 关联是可撤销的通信同意，不是 Membership；
- provider-native payload 不是长期产品模型；
- 临时下载 URL 和 credential 不进入长期 Work 或 Agent prompt；
- 删除必须区分停止未来访问、删除可删除内容和保留必要审计事实；第一条 Conversation journey 不提前建立删除或 retention lifecycle。

## 失败与恢复

Carry 必须区分：

- 已记录与已理解；
- 已发送与已阅读；
- 已授权与已执行；
- 成功、失败和 Unknown。

Host 或 Agent 失败后，Carry 依靠持久 Work 和数据库 authority 继续。lease 过期只撤销旧执行的提交权，不证明旧进程死亡。

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

推荐：

```text
Carry 正在推进
你补充的信息尚未应用
Carry 正在等待 Alice 回复
Carry 将在下周一继续
这是当前结果
Carry 需要你的决定
Carry 没有形成可确认的更新；由你选择是否重新推进
这次操作可能已经发生，结果尚不确定
```

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

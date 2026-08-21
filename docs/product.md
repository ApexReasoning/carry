# Carry 产品定义

本文件拥有界面、动作、产品词汇、隐私边界和不变量。owner、权限与并发见 `docs/architecture.md`；节点与研究见 `docs/implementation.md`。这里不写事务细节。

Carry 是用户可以长期托付责任的 AI 同事：用户在 carry.ai Web 把一件事交给一个有名字的 Agent，那个 Agent 负责推进这份 Work，并在真的需要人的信息、判断或授权时回来。

## 1. 用户可见概念

`Space`、`Agent`、`Conversation`、`Work`、`Host`、`Inbox`。

- **Space**：成员、Host、Agent、Conversation 和 Work 的协作边界。有可重复的显示名和一个全局唯一的 URL slug。
- **Agent**：一个有名字、有头像、属于一台 Host 的持久身份。用户对它说话、把 Work 交给它、在 Work 页面看到它。它不是用户创造的人格或职位，而是那台机器上一个真实 runtime 的产品身份。
- **Conversation**：成员与一个确定 Agent 的连续对话。
- **Work**：一份持续责任，有一名人类负责人和一名 Agent 负责人。
- **Host**：一台已接入 Space 的机器，用户在设置里增删。
- **Inbox**：所有需要人处理的 Work 事项。

以下永不出现在人看到的界面、文案和邮件里：Run、attempt、lease、fence、version、digest、credential、socket、session handle。它们是内部机制，不是产品词汇。面向 Agent 的机器接口可以暴露研究证明必需的最少内部事实。

新增一个用户可见名词必须同时满足四条，否则用现有 owner 的字段或一个局部值表达：当前旅程没有它会具体失败；它有独立身份和生命周期；它保护相邻事实无法表达的权限或并发边界；用户能感知它的价值。流程中的一步、一个角色、一个视图、一次临时计算都不是新概念。

## 2. 界面与动作

### 2.1 登录与进入 Space

登录方式是 GitHub、Google、Email，三种都保留。GitHub 登录只证明身份，不授予任何仓库权限。第一版没有 onboarding 表单，也没有姓名编辑器；新 User 使用稳定的 `Member <短 ID>` 系统标签，界面不得把它冒充成真实姓名。未来只有在一条明确的个人资料旅程出现时才允许修改。

登录后只有两种情况：

- 带着邀请来的人先完成认证，然后看到并接受被邀请的 Space，跳过正常初始化；
- 其他人从自己已属于的 Space 中选一个，或者新建一个。

正常入口 `/` 始终显示完整的零/一/多个 Space 选择与新建页；选择后进入 `/s/{slug}`。URL 是当前 Space 的唯一事实，不保存默认 Space，也不因只属于一个 Space 而自动进入。新建时用户只填显示名。显示名可以与别人重复；系统由它派生一个全局唯一的归一化 slug。slug 冲突时界面就地给出确定的下一个数字后缀建议；建议不预留、也不承诺仍可用，用户可以尝试它或修改 Space 显示名。第一版 slug 创建后不可修改。所有 slug 都位于 `/s/` 命名空间，不建立顶层保留名表。

没有"第一个 Space"特例：创建和选择用同一条路径。

### 2.2 邀请

Membership 只保留两项彼此独立的窄权限：管理成员、连接 Host。它们不是角色层级或通用权限系统。创建 Space 的成员初始拥有两项；当前持有人可以把自己拥有的某一项授予同 Space 的 Active 成员，邀请人也只能授予自己已有的权限。除非结束整个 Space，每项始终至少有一名 Active 持有人。

具有成员管理权限的成员从设置里生成邀请链接。链接包含现有邀请 ID 的准确路径；该 ID 只用于在登录后恢复导航意图，持有它不披露邀请内容，也不授权接受。认证前只显示通用登录方式，不预览 Space、邀请人、权限、收件人、有效期或状态。准确受邀 Email 的当前 owner 登录后能看到 Space、邀请人、两项权限，并在接受前用页面内 Email 验证完成近期证明；Google/GitHub profile email 不授予邀请权限。链接一次性、有有效期、可撤销；准确 owner 能看到 revoked、expired、accepted 的真实终态，其他 User、未知 ID 和由别人接受的邀请都得到同一个无元数据的 unavailable 结果。已失效的链接给出明确原因，不静默失败。成员移除也要求这项权限；连接或撤销 Host 要求连接 Host 权限。

### 2.3 Host 与 Agent

用户在设置里选择 Add Host，在目标机器上运行显示出来的命令（`carry setup`），然后在浏览器里核对这台机器确实是自己刚才操作的那一台。确认后 Web 显示这台 Host 以及它上面的 Agent 清单。设置里可以增加和移除 Host。

Agent 清单上每个 Agent 显示：名字（在 Space 内唯一）、确定性生成的头像、所属 Host、人类 owner、状态（Active / Removed），以及在线、最近活跃和正在参与的 Work。Space 内所有成员都可以选择其中的 Active Agent；第一版没有 per-Agent access mode。当具体 Agent 能可靠报告当前可选模型时，界面才显示模型选择；发现失败时使用 provider 默认值且不显示选择器。没有头像上传，没有模型或 provider 目录。

一个新 Agent 的人类 owner 是在浏览器中批准这台 Host 接入的已认证成员，不来自 setup shell、进程发现内容或模型输出。再次 setup 或发现只更新在场：不改写已有 Agent 的人类 owner、名字或生命周期，也不复活 Removed Agent。自愿情况下，只有当前人类 owner 可以把 Agent 转给同一 Space 的另一名 Active 成员，或把它转为 Removed；安全撤销 Host/Agent 与成员强制移除是独立的强制路径，不因此获得 Work owner 权限。

Host 掉线不删除 Agent 身份：Agent 仍在清单里，只是不在线。

### 2.4 对话

聊天界面只有一个输入框。用户在其中选择当前可访问的 Agent 之一。一个 Conversation 在第一条消息后固定它的 Agent；换 Agent 就是开一个新 Conversation。支持模型选择时，模型同样在第一条消息后固定。

Conversation 可以随时回来继续，包括在 Host 重启之后。恢复可能变慢，但对话内容和 Work 事实不会丢。

### 2.5 创建 Work

每个 Work 有且只有一名人类负责人和一名 Agent 负责人。人类负责人拥有目标与范围、对外授权、Inbox 回应、验收和关闭；Agent 负责人拥有推进、计划、评论理解与转达、协作者选择与检查、提出需要人的事项和时间安排。

Work 只能通过 Agent 创建，共三条入口：

- **对话**：用户在对话里交办，这条由 Conversation 拥有的准确成员消息被投递给固定 Agent；Agent 明确表示接下并创建 Work。人类负责人是发出被接受消息的成员，Agent 负责人是这个 Conversation 的 Agent。
- **Web 表单**：按钮上写明 `Create with <Agent>`。提交只在 Conversation owner 下持久化一条明确指向所选 Agent 的结构化请求，不直接创建 Work；拥有该 Agent 的 Host 拉取并 claim 后，由 Agent 决定接下、要求澄清或拒绝。接下时人类负责人是提交成员，Agent 负责人是所选 Agent。正文里写什么都不改变负责人。
- **本机 Agent**：机器上已注册的 Agent 通过 `carry` 创建。人类负责人是该 Agent 的人类 owner，Agent 负责人就是这个 Agent。调用方不能被归属到唯一一个已注册 Agent 时直接拒绝。

界面上没有"不经过 Agent 直接建 Work"的按钮。请求等待 Agent 时页面显示准确目标与等待状态；进程丢失后仍可继续，刷新或响应丢失会按同一请求事实恢复，不会凭空显示 Work，也不会因重放创建两份。Server 只接纳成员请求和 Agent 授权的创建结果，永不主动推送或替 Agent 接下。

### 2.6 Work 页面

Work 页面随时回答六个问题：

- 谁负责：人类负责人和 Agent 负责人；
- 谁在做：当前和历史参与的 Agent、各在哪台 Host、各自在处理什么；
- 准备怎么做：一份有序的计划项列表，由 Agent 负责人维护；
- 进行到哪：每个参与 Agent 的当前活动和一段有界的实时流；
- 产出了什么：产出项列表，可以指向对应的计划项；
- 要我做什么：准确的问题、选择、授权或异常。

计划只是展示真相，不是调度真相：系统不执行计划项，也不从计划里推导流程语义。持久保存的是用户可见的进展与产出，不是完整 tool trace 或模型思维；诊断内容只在展开时可见。

人类负责人可以验收一个准确的产出版本。验收只表示这个版本被接受，不会自动关闭 Work；产出后续修订需要重新验收。没有独立 Result owner，也不把产出内容或 checks 通过推断成人已经接受。

### 2.7 评论与协作

用户的评论一律送到 Agent 负责人。它理解、澄清，并按语义把相关部分转达给正在参与的协作者。用户不需要点名某个 Agent，也没有任务分派界面。

一个 Work 可以有一个或多个正在进行的 Agent session，顺序或并行。协作者不改变任何一位负责人。多个 Agent 参与是 Agent 负责人在有用且可用时的选择，不是产品要求：只有一个 Agent 的 Work 必须同样完整可用。

目标 Agent 不可用时，界面直接说明是哪个 Agent、为什么不可用、用户可以做什么。系统永不按名字换一个"差不多的"Agent 顶上。

### 2.8 Inbox 与渠道

Agent 负责人认为需要人时，在 Work 上提出需要人的事项，它们出现在 Inbox。每一项说明：为什么需要人、谁来处理、要回答什么、各选择的后果、是否阻塞 Work、是否已过时或被解决。一个 Work 同时可以有不止一项。除此之外，当前 Agent 负责人 Removed 或不再在场的 Open/Paused Work，也根据已有 Work、Agent 与 Machine 事实直接出现在其人类负责人的 Inbox；这不是由失联 Agent 创建的新事项，也不新增 owner。Inbox 是这些权威事实的查询，不是 activity feed。

用户连接邮件或飞书渠道后，投递自动发生，不需要对每一条事项再批准一次。由系统事实派生的 owner unavailable 同样投递给准确的人类负责人，不依赖 Agent 生成通知内容。渠道只是投递：发送成功不等于已读；投递失败或结果未知不改变 Work 与 Inbox 的真实状态；从渠道回到产品仍需正常登录和权限。

### 2.9 时间

Agent 负责人可以为这份 Work 约定一次未来继续，或者按天/按周周期继续。用户能看到下次时间、时区、是否启用和上次结果。第一版人只有只读显示，以及在安全需要时的暂停与取消；不提供通用的人类日程编辑器。关闭 Work 停止未来继续。时间条件属于这份 Work，不构成通用调度器或 workflow builder。

### 2.10 结束、下线与交接

生命周期只表达 `Open`、`Paused`、`Closed`。只有人类负责人可以暂停、恢复、关闭和重开。

暂停后 Work 仍可读，但不再产生新 claim，旧执行提交被拒绝，未来继续暂停；已经提交到外部而结果 Unknown 的动作仍保持 Unknown。恢复从当前持久事实开始新的执行机会，不复活旧 lease、fence、批准或 provider session。

关闭表示不再继续承担这份责任，停止新的 claim 与未来继续，但不删除历史，也不把未验收产出或 Unknown 外部动作改写成成功或失败。重开保留两位负责人和全部历史，从新权威开始；如果 Agent 负责人不可用，重开后仍明确显示 owner unavailable。关闭与产出验收是两件事。

Host 暂时掉线不会改写 Agent 负责人。Work 保持 Open，但不再接受旧执行提交的新进展，并显示 `Agent owner unavailable`。人类负责人可以等待原 Agent 恢复，也可以暂停、关闭，或把 agent ownership 转给同一 Space 中另一个 Active 且提交当时可用的 Agent；系统永不自动挑一个替代者。

计划移除 Host 或 Agent 时，确认界面先列出它负责的全部 Open/Paused Work，但不在这里批量改 owner。当前操作者只可以打开自己作为人类负责人的 Work 并逐份移交；其他 Work 由各自的人类负责人处理。移除 Host 或 Agent 的权限本身不授予修改这些 Work 的权限。安全撤销不能被交接阻塞；用户可以立即撤销 Host 或 Agent。被撤销 Host 绑定的 Agent 与被单独移除的 Agent 都转为 Removed；Carry 立即拒绝它们后续提交，但不声称失联机器上的本地进程已经停止。受影响的 Work 进入 owner unavailable，直接出现在各自人类负责人的 Inbox，之后再逐份移交。

只有 Work 的人类负责人可以确认 Agent 负责人转移。确认界面显示旧负责人、新负责人、会失去提交权的执行和暂停的未来继续。转移在一个权威状态变化里完成：旧 Agent 的当前执行立即失去提交权；旧 Agent 保留为历史参与者；已有产出、评论、计划、需要人的事项和 Unknown 外部动作都不被改写；未来与周期继续先暂停，由新 Agent 阅读交接事实后重新确认。

新 Agent 不继承旧 Agent 的 provider 私有 session、完整 tool trace 或私人 Conversation。转移提交时会校验替代 Agent 当时可用，但不承诺它随后持续在线；如果它在提交后掉线，同一份 Work 再次进入 owner unavailable，系统仍不自动换人。可用时，新 Agent 从这份 Work 的持久目标、评论、计划、产出、参与活动、未解决问题和 Unknown 事项开始一个新 session，并先给出用户可见的接手摘要，再继续推进；摘要不是负责人转移事务的完成条件。转移不需要旧 Agent 在线或批准，也不会把 Work 的人类负责人一起改变。

成员自愿离场时，必须先用普通 owner 转移清空自己负责的每一个 Open/Paused Work，并把自己拥有的 Active Agent 转给另一名 Active 成员或置为 Removed；否则离场失败并列出剩余项。

具有成员管理权限的人可以强制移除不合作或失联成员。确认页只读列出目标的 Open/Paused Work 与 Active Agent，并明确：目标的 Agent 会转为 Removed，目标的 Open/Paused Work 会由执行移除的人立即承接。执行者不能替目标选择第三方。如果目标是连接 Host 权限的最后持有人，该项同时由执行者承接；成员管理权限不可能随目标消失，因为执行者本人已经持有。移除事务完成后，承接的 Work 直接进入执行者 Inbox，说明负责人变化来自成员移除；执行者再用普通规则逐份转移、暂停或关闭。强制撤销不等待目标、Host 或 Agent 确认。

任何时刻都不允许出现没有有效人类负责人的 Open/Paused Work，也不允许出现人类 owner 已不是当前 Space 成员的 Active Agent。

结束整个 Space 是另一条明确旅程。具有成员管理权限的人必须先看到仍存在的 Open/Paused Work、Host、Agent、渠道、Unknown 外部动作与数据保留后果；所有 Open/Paused Work 必须先由各自人类负责人关闭，Space 结束不会替他们批量关闭。确认后未来登录、claim、schedule 和本机创建失权，Space 从普通选择中消失，既有 Work lifecycle 与历史不被改写。Carry 不承诺远端进程、已投递内容或副本已经被擦除。

不同产物用不同方式检查：代码看 diff、checks 和 PR；研究看来源和未解决的不确定性；运营动作看目标和外部回执。合并一个 PR 不定义通用的 Work 成功。

## 3. 产品不变量

以下事实永远不能互相冒充：已收到 ≠ 已理解；正在执行 ≠ 有可用 Agent；一个 session 结束 ≠ Work 完成；一个产出项 ≠ 用户已接受；已发送 ≠ 已读；已批准 ≠ 已执行；Failed ≠ Unknown；Host 掉线 ≠ Agent 不存在。

Unknown 不能被猜成成功或失败，也不会被自动重放。重试必须说明会复用什么、会重新执行什么、是否可能重复外部后果。

Host 或 Agent 失败后 Work 仍然存在：旧执行失去提交权；只有人类负责人显式转移后，新 Agent 才从持久事实继续。本地失败不能表现成系统已经接受。丢失一个 provider 侧的会话续接可能降低效率，但不丢失 Conversation 与 Work 的真相。

## 4. 隐私与授权

- 私人内容默认只对准确成员可见；共享 Work 不保存私人原文或可反向读取的来源关系；
- Agent 只获得当前工作所需的有界上下文，也只能改变当前工作的事实；
- Agent 不持有成员凭据，也没有绕过所在 Host 直接访问服务端的路径；
- Host 接入只让那台机器上的 Agent 在那一个 Space 里新建 Work；它们不能凭这一点列出、读取或修改已存在的 Work；
- Agent 负责人请另一个 Agent 帮忙不扩大数据范围或外部权限；
- 模型输出和外部内容不能创造成员、可见范围、凭据或外部权限；
- 外部动作必须绑定真实成员、准确目标、准确参数和当前批准；
- 移除一台 Host、一个 Agent 或一名成员只撤销未来的权限，不声称远端进程已停止或副本已消失。

## 5. 明确拒绝

第一版不建立：人用 CLI 作为 Web 的平行产品；不经 Agent 的 Work 创建入口；Agent persona、角色 roster 或 marketplace；头像上传或媒体子系统；provider/model/runtime 目录；workflow builder 或通用 Plan/Step 图；任务分派与协调界面；通用 Effect/Action/Capability/Plugin；自然语言命令入口或通用动作注册表；把完整 tool trace 当作主产品；内容派生权限；Unknown 自动重试；按 Git 分类 Work；为未发布路径保留兼容层。

## 6. 完成标准

一项产品能力完成，必须同时有这些证据：

- 新用户能从真实 Web 入口走完整条 journey，没有预置数据；
- 真实 Agent（Pi 或 Codex）真的改变了这条 journey 的结果；
- 目标 Agent 不可用、Host 掉线、Agent 被移除、负责人转移、响应丢失都有用户能理解的恢复；旧执行恢复后仍不能写；
- 数据库直接证明并发唯一 winner、幂等、过期与迟到写入被拒绝；
- 权限和隐私不能被模型内容或本地进程绕过；
- 两位负责人在界面上都成立：人能验收和关闭，Agent 的推进和提问可追溯；
- 同一设计在至少一条真实非代码 Work 上同样成立；
- 被替代的界面、路由、schema、查询、客户端、测试和文档已经删除。

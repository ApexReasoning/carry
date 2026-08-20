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
M2 Public team entry           Nodes 5–10
M3 Trusted execution           Nodes 11–13
M4 Useful responsibility       Nodes 14–17
M5 Carry Cloud MVP             gate after Node 17
M6 Product expansion           Nodes 18–19
M7 V1                           gate after Node 19
```

Node 不是 package 或 migration 批次，而是用户结果。默认一到三个工作日；第三天仍不能关闭时应删减或拆分。Milestone gate 只验证已经逐 Node 关闭的累计产品旅程、发布与运维证据，不伪装成另一条 Node。

Carry Cloud MVP 的截止点是 M5：一个团队通过三种首发认证之一进入，显式创建或加入 Space，连接一台 Space-enrolled Machine，把非 Git Work 托付给 Carry，得到可检查的带来源结果，并能转交、停止和约定未来继续。渠道、外部写操作、自托管发布与 managed Cloud execution 不阻塞这个截止点。

按单一串行 writer、每个 Node 完整研究/review/CI、真实 provider canary 和生产邮件准备估算：Node 12 后约四到六个工作周可进入团队 dogfood；Node 14 后约五到八个工作周可验证第一份 grounded useful result；M5 Carry Cloud MVP 约需七到九个工程工作周，考虑 OAuth、SMTP 与部署外部等待后按九到十三个自然周承诺。

## 7. 已完成基线：M0、M1 与 Node 5

详细 Node 关闭记录属于 Git/PR 历史；当前稳定行为由 Product、Architecture、协议和测试定义。

- **Node 0 — Foundation/Machine enrollment:** 发布 `carry-server` 与 `carry`；成员以 User identity enrollment/revoke Machine；Host 只使用独立 Machine mTLS；private key 与 pending enrollment identity 先以 `0600` 保存；Runtime diagnosis 只在本地。
- **Node 1 — First durable Work:** CLI/Web 可幂等创建、读取和补充 Work；PostgreSQL 保持消息顺序与重启后连续性；Web 用 HttpOnly Browser Session，不保存长期 bearer；内部 sequence/CAS 不进入 User API。
- **Node 2 — Native execution parity:** Pi/Codex 保留原生 adapter，只共享 Carry-owned Execute/Reply 合同；strict output 只有经 Machine fenced commit 才能修改 owner facts；核心不保存 provider/runtime/model/session。
- **Node 3 — Host interruption recovery:** expired Attempt 永不复活；新 Machine/Attempt 增加 fence 并取得相同 fixed Run range；旧 Machine renew/commit/finish 均失败；Failed/Unknown 只由成员幂等 `Try again` 开启 fresh Run。
- **Node 4 — Private Conversation:** 每个 `(Space, member)` 一个私人 Conversation、一个 outstanding turn；User API 与 Agent context 有界；exact Machine claim/fence/lease 才能读 fixed context；reply-once commit 可原子创建至多一份 Work，但 Work 不保存私人 source、digest 或 transcript；Browser 只保存随机 request key并 fail-closed sign-out。
- **Node 5 — Result review and Needs You:** Agent 只解释 `review_required`；PostgreSQL 仅为覆盖 current input head 的准确 Work 内容创建 result check；Needs You 查询当前 owner 的待检查结果或 explicit retry；幂等接受绑定 exact version/digest，不关闭 Work、不自动继续，也不产生外部 authority。

当前基线明确不包含 native Session recovery、Agent credential/API、provider routing、Work Offer、queued private turns、retention lifecycle、Work/Conversation target union、独立 Result/Attention owner、结果历史正文或 accept-and-continue。

Node 6 进入前仍保留一个明确迁移约束：旧 token-to-browser 与 CLI bearer 只是 Node 0–5 的过渡基线，不扩展成长期产品合同。后续每个 Node 从当时用户旅程、至少五个一手产品比较与 Loop 只读考古重新推导；详细源码只在该 Node entry 调研，本次产品路线不作为代码模板。

## 8. Node 6：Production email identity and first Space

### 用户结果

新用户可以通过生产事务邮件发送的一次性验证码建立稳定 Carry User，显式命名并创建第一个 Space，成为首位 active member；返回用户可以再次登录同一 User 并安全 logout。

Resend HTTPS API 是官方 Cloud 的唯一 concrete production email transport；它只负责提交准确邮件，验证码负责邮箱 possession proof。一个 caller 和一个实现不赚得独立 adapter package、provider interface hierarchy、registry 或 fallback。首版不保存成员密码，也不把 email OTP 宣称为 MFA、NIST AAL 或抗钓鱼认证。

Node 6 同时纠正服务端 owner boundary：`cmd/carry-server` 只静态组合 concrete 实现并保留 Resend HTTP；`internal/server` 只负责 inbound HTTP/route、syntax、Browser Session authentication、cookie 与 status；邮箱 request/verify/replay、code/session derivation 与 Resend outcome 编排属于 Identity，首个 Space command/digest/identity 属于 Space，Machine enrollment certificate/identity 编排属于 Machine。它们只使用现有 owner 在消费点定义的窄依赖，PostgreSQL 保留完整 atomic transaction；Work/Conversation/Run 的 exact command handler 继续直接调用完整 PostgreSQL use case，不建立 forwarding Service 或新 package/owner。

### 关闭证据

- request、verify、resend、错误尝试、过期、single use、并发与 response-loss replay 有直接 Browser 证据；
- code 固定六位十进制、五分钟 expiry、五次唯一错误尝试与六十秒 resend cooldown；resend 创建新 challenge 并永久使旧 code 失效；
- 邮箱经过 trim、严格 addr-spec validation 和整体 Unicode lowercase；不做 provider-specific dot、plus 或 alias 重写；
- 每次投递固定准确 recipient、challenge、payload 与 provider idempotency key；Resend Accepted 不等于 delivered/read，response loss 不猜测成功或失败，只在 challenge 有效期内以相同 key 重放完全相同请求；
- 不存在与存在的邮箱返回一致响应，限流保护地址、来源与投递信誉，验证码不进入日志或 browser storage；
- Browser Session 由 Carry 签发、HttpOnly、可 logout/revoke，并在 stale cookie 下 fail closed；
- Space 创建必须由用户显式发生，creator 获得当前旅程需要的 `can_manage_members` 与 `can_enroll_machines`，不建立 generic admin role；并发创建/重放不制造重复 Space；
- 旧 token-to-browser bootstrap 不再是正常 Cloud human login truth；Browser exchange 和 token-entry UI 删除，现有 bearer/bootstrap 只为 CLI 过渡保留到 Node 11。

### 明确不做

Google/GitHub、method linking、邀请、成员密码、Passkey、CLI credential、自托管发布、domain auto-join 或默认 Space。

## 9. Node 7：Google and GitHub authentication parity

### 用户结果

用户可以分别通过 Google 或 GitHub 建立或返回稳定 Carry User；两种方法与邮箱 OTP 最终都只签发同一种 Carry-owned Browser Session。

Google 使用稳定 `(issuer, sub)`，GitHub 使用稳定 numeric user ID。provider email 只作为 provider fact；相同邮箱不能自动关联、合并 User 或产生现有 Space Membership。

两种方法从 same-origin POST 开始，使用 provider-fixed、数据库过期的一次性 transaction、独立 browser binding、state 与 PKCE S256；Google 另验证 nonce 和完整 ID token。callback URI 只来自必填 canonical HTTPS external origin 与固定 route，不能由 request/forwarded host、`return_to` 或 provider 内容改变。provider 网络 I/O 在 transaction 外；exact callback 只有一个 exchange winner，ambiguous exchange 终结 Unknown，provider identity、User、现有 Browser Session 与 completion 原子提交，committed response-loss replay 不再次访问 provider。

### 关闭证据

- Google 与 GitHub 各有真实 Browser signup、repeat login、logout、denial、state/nonce、callback replay 和 provider outage 证据；
- provider subject collision fail closed，provider access token 不成为 Carry Session 或长期产品 credential；Google 只请求 `openid`，GitHub 不请求 email/repository/organization scope；
- 三种首发方法在 Milestone 边界同时可用，但没有 method linking 时，相同邮箱仍保持分离身份并清楚提示下一步；
- 外部 provider 不能建立或扩大 Space authority。

### 明确不做

Apple、Microsoft、Passkey、企业 SSO、自动 same-email merge、GitHub repository authorization 或 broker Organization authority。

## 10. Node 8：Explicit identity linking and recovery

### 用户结果

已登录成员可以在 Settings 中明确关联或移除另一种首发认证方法；丢失一种方法后，可以使用仍已关联的方法回到同一 Carry User，而不是恢复旧 secret 或新建隐式合并账号。

### 关闭证据

- 当前已关联方法与待关联方法都经过 fresh proof；
- 已被另一 User 占用的 provider identity 或 email challenge 拒绝 linking，不 merge 两个 User；
- 至少一种可用方法保留，unlink/revoke 与并发 linking 一个 winner；
- 敏感变更可以撤销既有 Browser Sessions，并在 response loss 后准确收敛；
- lost-all-method 不提供弱人工绕过，产品清楚说明无法恢复的边界。

### 明确不做

管理员重置他人身份、按邮箱合并两个 User、成员密码、Passkey/MFA、secondary email、企业 IdP recovery 或账户合并工具。

## 11. Node 9：Member admission

### 用户结果

管理成员可以从 Settings → Members 邀请准确邮箱；受邀者认证并验证该邮箱后，看到 Space、邀请人与将获得的权限，明确接受后加入现有 Space。

### 关闭证据

- `can_manage_members` 是窄 authority，不建立角色 framework；
- pending、expiry、revoke、resend、wrong-email、already-member、single winner 与 response-loss replay 由 PostgreSQL 裁决；
- 邀请固定准确 recipient、Space 与将获得的 authority；provider accepted 不等于 delivered/read，Unknown 不盲目重发或伪造受邀者已收到；
- 第一位新 User 与既有 User 都能在认证后返回准确邀请并接受；
- 认证成功不自动接受邀请，provider/email identity 不能自行产生 Membership；
- Space manager 不能恢复、登录或冒充另一成员，也不能读取其私人 Conversation。

## 12. Node 10：Member removal

### 用户结果

持有 `can_manage_members` 的成员可以移除一名 active Space member；移除立即撤销其未来 Space 访问，但不改写真实历史，也不能留下无人负责的 Open Work 或没有成员管理 authority 的 Space。

### 关闭证据

- actor、target Membership 与当前 authority 在一个 PostgreSQL transaction 中验证，并发 removal 一个 winner；
- target 仍负责 Open Work 时拒绝 removal，成员先使用显式 transfer；
- 唯一持有 `can_manage_members` 的成员不能移除自己或被移除；
- removal 后旧 Browser/CLI session 即使仍代表同一 User，也无法读取或修改该 Space、私人 Conversation、Work 或 Machine inventory；
- exact response-loss replay 收敛，different target/request 冲突；
- removal 不删除历史作者、共享 Work 或私人 Conversation 内容，也不自动转交责任。

## 13. Node 11：Browser-approved member CLI

### 用户结果

成员运行 `carry login`，在已登录 Browser 中检查准确 server、device context 与 Space 后批准这次 installation；随后可以用现有 `carry work` 命令创建、读取和补充准确 Space 的 Work，credential 丢失或撤销后只能安全替换，不能恢复旧 secret。

### 关闭证据

- approval、deny、expiry、cancel、poll interval、single redeem、并发与 response-loss replay 直接可见；
- user code、poll secret 与 final CLI credential 分离，终端不接收邮箱验证码、provider token 或 Browser Session；
- final credential 绑定准确 User/server，Space context 可检查但不因 CLI login 自动扩大 Membership；
- `carry work create/list/show/message` 通过 Browser-approved credential 完成真实 journey，所有 Space/Work authority 仍由服务端当前 Membership 裁决；
- revoke 后旧 CLI credential 立即失败；
- CLI credential 与 Machine certificate 永不混用，旧 member bearer 路径被删除。

### 明确不做

CI/service account、共享 automation token、Machine enrollment token、通用 device registry 或通过 CLI approval 授予执行 authority。

## 14. Node 12：Machine connection and recovery

### 用户结果

成员运行 `carry host connect`，在 Browser 中检查准确 public key fingerprint、Space 与显示名后批准这台 Machine；之后可以查看持久 inventory、远程 revoke、本地 cleanup 并重新连接一台新 Machine。

Node 进入时优先重新走查 Multica 当前 Add a computer、browser sign-in、daemon diagnostics 与 deletion/re-registration 操作链路，并与 Tailscale device、GitHub/GitLab runner、OpenHands 和至少一个相关一手产品比较。详细源码与 Loop 考古留到 Node entry；只吸收产品确认和恢复优点，不复制 heartbeat、online/offline、last-seen、Runtime/provider/version、capacity 或 shared PAT 模型。DeepSeek Harness 的 sandbox seam 只作为诚实表达 enforcement、runner failure 与 denial 的对照：Node 12 的 remote revoke 继续只证明服务端 certificate authority，不引入 sandbox、plugin 或 Session owner。

### 关闭证据

- Browser approval 准确绑定 public key、Space、display name 与 approving member；
- approval secret、member/CLI credential 与 durable Machine certificate 分离；
- list 只展示 display name、Space、enrollment/revocation actor/time 与 authoritative Active/Revoked；
- remote revoke 只承诺服务端 certificate authority 已撤销，不声称远端进程停止或文件删除；
- confirmed revoke 后 cleanup 可恢复；service unreachable 时 local-only erase 明确不声称 remote revoke；
- response loss、stale credential rejection 和 fresh re-enrollment 有直接证据，新连接不复活旧 Machine identity。

## 15. Node 13：Private reply failure

### 用户结果

私人回复无法形成时，准确成员在原消息旁看到 Failed 或 Unknown，并可以明确 `Try again`；失败不会伪装成 Carry transcript 消息或 Needs You Work。

### 关闭证据

- 成员能区分仍可恢复、明确 Failed 与无法确认 outcome 的私人回复；
- terminal outcome 记录前的 lease/claim recovery 有明确上限，旧 claim/worker 不能晚到提交；
- Failed 或 Unknown 一旦对成员可见就不再自动 replay，只有成员显式 `Try again` 才允许 fresh reply；
- `Try again` 幂等，新 member message 不与旧失败恢复竞争；
- 失败不伪装成 Carry 消息，也不进入共享 Work 或 Needs You；
- 持久字段、expiry 推进者和 recovery transaction 只在 Node 13 操作链路研究与合同冻结后确定。

## 16. Node 14：Cited public-source research

### 用户结果

成员可以把一份公开资料研究责任交给 Carry，并明确允许将准确、有界的公开研究问题发送给当前显示的 concrete research provider；Pi 与 Codex 形成一份带可检查来源链接的 briefing，并进入现有 exact result review。

第一条 fixture 应是跨领域且可直接判断的真实问题，例如比较若干厂商当前条款并区分 observed fact、Carry inference 与未知缺口。引用仍属于 Work 当前内容；URL、网页文本和模型输出不能产生 authority，也不因“被引用”升级成 Evidence 或 Artifact owner。

Node 进入实现前必须把研究授权和 provider submission 冻结为现有 Work/Run owner 的 typed facts：PostgreSQL 固定 authorizing member、Work、canonical research question、concrete provider audience、披露与成本边界、expiry、request identity 与 payload digest，并只把 immutable、Attempt-scoped facts 交给当前 Host。具体 caller 在网络 I/O 前机械验证每个 query 未扩大边界；Prompt、tool schema、manifest、credential presence 或模型自述不能补齐权限。这个合同不预建 Capability、Tool、Plugin 或 Action owner。

### 关闭证据

- 新用户不需要 repository、Git、channel 或 provider/tool 配置即可从委托走到 cited result review；
- authority 固定 authorizing member、Work、允许披露的 canonical research question、concrete external audience 与有效期；模型生成的附加 query 只能在该边界内收窄，不能扩大披露；
- provider 的 retention/logging 边界在授权前可检查；未明确授权的 Work Message、私人 Conversation、credential 与内部 authority facts 不进入外部请求；
- 每条关键外部主张有可打开来源，来源缺失、冲突、超时与部分失败保持显式；
- 请求与返回有界，恶意网页内容不能改变 actor、Space、Work、credential 或 external consequence；provider submission 区分 prepared、accepted、rejected、response received 与 Unknown，只有 provider idempotency 或直接 reconciliation 证据证明安全时才以同一 request identity 和完全相同 payload 重试；
- Pi/Codex 完成同一产品 journey，但保留 concrete adapter 做法；
- 能力是只读的，不产生外部写操作、长期 bytes、通用 browser、MCP/plugin registry 或 capability marketplace。

## 17. Node 15：Responsibility transfer

### 用户结果

当前负责人可以把一份 Open Work 明确转交给另一位 active Space member；新负责人可以理解共享 Work 当前事实并承担后续判断，私人 Conversation 不随转交泄漏。

### 关闭证据

- 开放 Work 始终只有一个负责人，竞争 transfer 一个 winner；
- actor、target member、Work 与预期当前 owner 在同一 PostgreSQL transaction 中验证；
- former member、跨 Space actor、inactive target 和 stale request 被拒绝；
- 旧 owner 立即失去 owner-only review/decision authority，但真实历史作者不被重写；
- transfer 不自动创建 Run、Message、通知或外部后果。

## 18. Node 16：Pause, close, and return

### 用户结果

当前负责人可以临时 Pause 并 Resume 一份仍承担中的 Work，也可以在责任结束时 Close 并在以后明确 Reopen；任何停止都会让旧执行立即失去提交权，Resume/Reopen 都不复活旧 Run、Attempt 或 native Session。

Pause 保留一份仍需承担但暂不推进的责任；Close 表示当前不再承担。两者直接属于 Work，不建立 lifecycle engine。

### 关闭证据

- Pause/Close 与当前 Run authority 在一个权威 transaction 中 fence；
- stale renew、commit、finish 和 retry 被拒绝；
- Pause/Resume 与 Close/Reopen 的不同产品含义、幂等、并发 winner 与 owner authority 有直接证据；
- Resume/Reopen 后只有 fresh eligible execution 可以继续；
- lifecycle 不按领域、Git、provider、Runtime 或完成方法分类。

## 19. Node 17：Future continuation

### 用户结果

当前 Work owner 可以为同一 Work 约定一个准确、可检查的未来继续时间；Pause 或 Close 后旧约定不执行。

第一条 journey 只需要一个未来条件。具体持久字段、时间推进者和 claim transaction 在 Node 进入时重新研究后冻结；不因路线图预建 Timer、recurrence 或 occurrence history。

### 关闭证据

- owner 可以检查绝对时间与时区，修改或取消后旧条件永久失效；
- 同一继续条件最多使 Work eligible 一次，并发推进一个 winner；
- 到点时没有可用 Machine，Work 保持 overdue-and-eligible，下一台有效 Machine 最多领取一次；在领取前 Pause、Close 或 cancel 仍使它失效；
- Pause/Close fence 旧条件，Resume/Reopen 不自动恢复它；
- 进程、Machine 或 native Agent Session 中断不丢失条件；
- overlap、recurrence 与 recurring catch-up 未被当前 journey 定义或猜测。

## 20. M5 gate：Carry Cloud MVP

### 累计用户旅程

一个团队可以从公开 Carry Cloud 入口，通过 Google、GitHub 或生产 email OTP 建立 User，显式创建或加入 Space，移除 former member，连接一台独立 Machine，把非 Git 研究 Work 托付给 Carry，得到并检查带来源结果，转交、停止或约定未来继续，同时准确看到私人回复与执行失败。

MVP 仍由团队连接一台 browser-approved Space Machine 执行；managed Carry Cloud execution 只有在 sandbox、provider credential、privacy 和 operations 的独立旅程被赚得后才进入。

### Gate 证据

- 三种认证方法、linking/recovery、邀请/removal、member CLI、Machine、private Conversation、Work、Needs You、cited briefing、transfer、lifecycle 与 future continuation 通过真实公开 journey 串联；
- 同一份研究 Work 在第一台 Host 中断或 Machine 更换后保持责任，未来条件到点后由有效 Machine 最多继续一次并形成后续 grounded update；转交后的新 owner 能从共享 Work 理解、纠正和检查它；
- production SMTP/provider delivery、OAuth callback、anti-abuse、session/logout/revoke、migration 与 secret 配置有 live evidence；
- 从空数据库和当前历史版本执行 migrations，PostgreSQL backup/restore 演练通过；
- `make check`、race、关键 protected Pi/Codex/provider canary、Web accessibility 与全仓三门 review 无 blocker；
- release candidate 可重建，删除 stale experiment、script、dependency 与 generated artifact；
- Cloud MVP 不宣称 channel delivery、recurrence、external Action、自托管发布或 managed execution 已存在。

## 21. Node 18：First connected conversation

### 用户结果

成员可以从一个已授权渠道与 Carry 或 Work 交流；重复 callback 不复制消息，普通 outbound delivery 不伪造已读，response loss 保持 Unknown。

Node 进入时从一个用户确认的 concrete provider journey 推导 native actor、target、thread、credential、privacy、idempotency 与 delivery outcome。不要预建 Connector registry、InboundMessage、Event branch、统一 rich content 或第二 provider framework。

## 22. Node 19：First external Action

### 用户结果

Carry 提出一个准确、单一类型的外部写操作，由正确成员检查固定 target 与 parameters 后批准，唯一 fenced worker 执行，响应丢失保持 Unknown。

Action identity 只能由独立授权与 consequence lifecycle 赚得。Node entry 从一个用户确认的真实后果选择 typed command；如果一条旅程同时包含 repository authorization、code mutation、branch、PR、CI 与 merge，就拆分而不是建立 generic command JSON、universal approval engine 或 provider registry。批准必须绑定已经持久、已经展示的 exact proposal identity 与 target/parameters digest，不能批准一份重新渲染的参数副本；执行事实区分尚未 dispatch 与已经 dispatch 但 outcome 未观察，后者在 reconciliation 前不得盲目重试。

## 23. M7 gate：V1

### 累计用户旅程

一个团队可以从正式发布入口把 Work 长期交给 Carry，跨进程和 Machine 持续推进，使用带来源的只读能力、一个 concrete channel 与一个准确 external Action；自托管部署保持同一 User、Space、Membership、Browser Session、privacy 与 authority 语义。

### Gate 证据

- 自托管明确配置 canonical external origin、redirect/callback allowlist、TLS/secure-cookie expectation、secret generation/rotation、sender/template 与 bootstrap/lockout recovery；
- 自托管至少提供 production SMTP 或 OIDC/OAuth，缺失安全配置时 fail closed，不提供共享 token 或无用户模式；
- 从空数据库执行全部 migrations，并完成 release migration、backup/restore 与真实 auth/channel/Action canary；
- `make check`、race、Web accessibility 与全仓三门 review 无 blocker；
- `carry-server` image、`carry` binaries 与 Web assets 可重建，并生成 checksum、SBOM 与 provenance；
- 删除 stale experiment、script、dependency、generated artifact 和本文件。

## 24. 条件 promotion

以下不占固定 Node，也不预建 owner：

- recurrence：用户反复重建 future continuation，并且 missed/overlap/catch-up 行为需要独立产品承诺；
- second channel：第一渠道已经证明真正共享的 delivery semantics；
- managed Carry Cloud execution：团队无需自有 Machine 的 journey 已经赚得 sandbox、provider credential、privacy 与 operations 边界；所有观察或修改同一 workspace 的 filesystem、subprocess、terminal、LSP 与 browser capability 绑定同一个 execution-world identity，混合 local/remote world 必须拒绝而不是静默组合；sandbox 分别声明 filesystem、network、process、secret 与 host access 的 enforcement，`partial` 不能作为 `full`；当时从 DeepSeek Harness 的 per-call policy、wrapped argv 与 enforcement fact 重新推导，不复制其 plugin/Session 架构；
- Passkey/MFA、secondary email、企业 SSO/SCIM 或 admin recovery：真实安全或组织旅程要求；
- Event：第一条没有明确 Conversation/Work target 的授权生产来源；
- Artifact：第一份必须长期保存的 immutable bytes；
- 多个并行执行者：一个 executor 无法自然完成的真实 journey；
- Agent API：原生 Agent 或 bridge 需要直接调用服务端能力；
- native Session optimization：fresh execution 的成本或时延被生产证据证明不可接受。

每次 promotion 都重新走产品旅程调研、概念准入、Node contract 和关闭证据。详细源码与 Loop 考古在该 Node entry 再做，不提前冻结实现。

## 25. Node 关闭记录

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

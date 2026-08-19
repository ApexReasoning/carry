# Carry 仓库与 CI 整洁设计

## 1. 目标

仓库结构应该帮助开发者回答三个问题：

1. 产品代码在哪里；
2. 谁拥有这段代码；
3. 用哪一个命令证明改动正确。

仓库不是历史档案馆，也不是所有实验、评审、生成物和临时脚本的永久容器。

本文件约束目录、文档、Makefile、scripts、tests、generated code 和 CI。它不定义产品或领域架构；这些分别由 `docs/product.md` 和 `docs/architecture.md` 决定。

仓库层面对“克制与自由”的职责是：克制当前树中的永久结构，保留开发与演化的自由。

- 只有当前产品、真实工具链、已发布协议和持续有效证据进入长期导航；
- 临时研究、review、生成 artifact 和被替代实现不获得永久目录；
- stable command 只承诺可验证结果，不冻结底层工具组合；
- boundary check 表达少量禁止方向，不维护中央 package allowlist；
- experiment 可以自由回答问题，但不能悄悄成为生产依赖或永久 CI；
- Git 历史负责保留过去，当前工作树负责给出一个清楚答案。

仓库整洁不是把文件数降到最低。真实 consumer、可重建 source、失败证据和发布合同必须保留；没有当前消费者的结构则应删除，让未来需求可以从当时事实重新设计。

## 2. 仓库为什么会失控

代码库通常不是因为某一个大错误变乱，而是因为临时方案不断变成永久结构。

最常见的原因是：

### 生成物和手写代码混在一起

数百个 sqlc 文件、Web build assets 或协议生成文件放在普通源码目录后，文件统计、搜索、review 和 ownership 都会失真。

### 历史证据留在当前导航中

每个阶段都增加 plan、review、audit、handoff 和 evidence，却不删除已被替代的文件。最后必须再写一份“文档权威地图”解释应该相信谁。

### 同一个检查被表达三次

```text
GitHub Actions step
→ Make target
→ scripts/* wrapper
→ 实际 go/pnpm 命令
```

如果每一层都保存一部分逻辑，开发者无法知道本地与 CI 是否真的执行同一件事。

### 中央脚本知道所有 package

一个测试脚本手工维护 package 列表、数据库列表和标签映射；一个边界脚本手工维护所有 domain package。每新增 package 都要修改远处的中央清单，ownership 开始扩散。

### 实验成为永久 CI

实验已经回答完问题，却继续保留 module、fixture、image 和 PR matrix job。CI 逐渐在维护历史研究，而不是保护当前产品。

### 没有删除门禁

重构只增加新路径，没有在同一个节点删除被替代的代码、脚本、文档和 CI job。仓库于是同时保存多代设计。

Carry 通过明确区分当前产品、生成物、实验和历史来避免这些问题。

## 3. 根目录保持稳定

以下是当前 V1 预期形态，不是冻结所有工具配置文件的穷举 allowlist：

```text
.
├── .github/
│   └── workflows/
├── apps/
│   └── web/
├── cmd/
│   ├── carry/
│   └── carry-server/
├── docs/
├── e2e/
├── experiments/
├── internal/
├── protocol/
│   └── user/
│       └── v1/
│           └── openapi.yaml
├── scripts/
├── AGENTS.md
├── Makefile
├── compose.yaml
├── go.mod
├── go.sum
└── README.md
```

规则：

- 不为了未来能力预建目录；
- 新的根目录必须代表独立工具链、发布物或长期证据边界；
- 一个普通 package 不成为根目录；
- 一个临时任务不成为根目录；
- 删除最后一个消费者时同时删除空目录。

`experiments/` 和 `scripts/` 不是必需存在的装饰。没有真实内容时不创建。

## 4. 当前代码放在哪里

### `cmd/`

只保存发布入口和 composition root：

- `cmd/carry-server`：服务端二进制；
- `cmd/carry`：成员 CLI 与 `carry host` 模式。

入口只负责：

- 建立进程 `context` 和 signal cancellation；
- 构造 CLI root；
- 映射最终 exit code；
- 注入诊断版本。

`carry` 的 Cobra adapter 位于 `internal/cli/`。root 在一个可见位置静态组合命令；下一层按 `login`、`host`、未来已经 earned 的 `work` 等用户命令组分包，同组叶子命令按行为拆文件。禁止 `init()` 注册、动态 command registry 和向所有命令暴露所有凭据/客户端的万能 Factory。

`carry-server` 的操作面仍然较浅，继续使用标准库 `flag`；两个二进制不为形式对称共享 CLI framework。

业务规则不放在 `cmd/` 或 `internal/cli/`。

### `internal/`

按事实 owner 和具体 adapter 组织。当前已经由旅程赚得的主干是：

```text
internal/
├── identity/
├── space/
├── conversation/
├── work/
├── run/
├── host/
├── agent/
│   ├── pi/
│   └── codex/
├── postgres/
├── server/
└── cli/
```

`internal/cli/` 是具体 Carry CLI adapter，不是通用 ownership bucket。结构由 `docs/architecture.md` 决定，不由数据库表、HTTP route 或 UI 页面决定。

禁止新增：

```text
internal/common
internal/utils
internal/platform
internal/integration
internal/registry
```

### `apps/web/`

Web 使用自己的 Node 24、pnpm、TypeScript 和 React 工具链。`package.json`、`pnpm-lock.yaml` 和 Node 版本文件都留在 `apps/web/`，不在仓库根目录再维护一份 JavaScript workspace。

Web 不复制 Go domain，也不把后端 package tree 映射成 feature tree。User API 的 OpenAPI source 位于 `protocol/user/v1/openapi.yaml`，由服务端协议拥有；Web 只在 `apps/web/app/generated/` 保存可重建的生成客户端和 Zod schema。只有新的真实协议受众出现后才增加另一棵 version 目录，不预建 Host 或 Agent 协议。

### `e2e/`

只保存跨进程、通过公开协议运行的少量关键旅程。

Package 集成测试留在 owner 附近，不搬到 `e2e/` 只是为了统一。

### `experiments/`

实验必须有：

- 一个明确决策问题；
- 独立 README；
- 独立 fixture；
- 可重复命令；
- promotion 条件；
- retirement 条件。

实验不进入生产 import graph，不默认进入 PR required CI。

问题得到回答后，只保留仍有决策价值的最小证据；删除运行框架和一次性 artifact。

## 5. 文档保持一个当前答案

根目录 `AGENTS.md` 是 coding agent 的短执行入口：它要求 Agent 找到当前 Node、读取相关 canonical 文档、在编码前声明允许路径和关闭证据，并遵守 review 预算。它不复制完整产品或架构设计，也不成为第六份 canonical 设计文档。

`docs/` 根目录只保存当前 canonical 文档：

```text
docs/
├── product.md
├── architecture.md
├── code-style.md
├── repository.md
└── implementation.md  # 第一版建设期间的唯一活动计划
```

未来只有出现真实需要时才增加：

```text
docs/decisions/    少量长期 ADR
docs/operations.md 首次生产部署准备开始时
docs/protocols/    已发布、需要独立维护的协议
docs/runbooks/     真实事故路径的操作手册
```

明确不建立：

```text
docs/plans/stages/
docs/reviews/
docs/handoffs/
docs/audits/
docs/evidence/
```

实施计划放在 Issue 或 PR。Review 结论留在 PR。CI 证据保存在 CI artifact 和发布记录。

ADR 只记录满足以下条件的决定：

- 代价高且难以反转；
- 未来维护者无法从代码直接推断原因；
- 存在被认真评估的替代方案；
- 决定仍然约束当前实现。

`implementation.md` 是第一版建设期间唯一允许留在仓库的活动计划，并在第一版关闭时删除。不得再为每个节点建立子计划文件。

被替代的文档直接删除。Git 历史已经保留它，不需要在当前树中继续充当噪声。

## 6. Makefile 是开发命令入口

Makefile 只提供稳定、可发现的项目命令，不承载复杂业务逻辑。

第一版目标保持小而明确：

```text
make format        自动格式化手写代码
make generate      通过仓库固定版本的 sqlc 运行所有代码生成
make test          运行快速单元测试
make test-db       运行真实 PostgreSQL 集成测试
make check-go      只读验证 Go、生成代码、数据库测试和两个二进制
make check-web     只读验证 Web 格式、类型、测试和 build
make check-product 运行少量关键浏览器旅程
make check         组合三个 check target
make build         构建 carry-server、carry 和 Web
make dev           启动本地开发环境
```

规则：

- 一个 target 对应一个开发者能说清楚的结果；
- 不为每个底层命令建立 target；
- 不在 Makefile 中编写大段 Bash；
- target 不根据环境偷偷改变安全语义；
- CI 与本地调用同一 `check-*` target；
- `check-*` target 只读，格式或生成结果不正确时失败，不能自动修改后通过；
- target 失败时输出实际失败命令和修复入口；
- 不保留无人调用的 target。

如果 `make check` 只是组合其他 target，它可以存在。具体检查逻辑必须属于工具配置、测试或一个有名字的 script。

## 7. Scripts 只做必要的跨工具编排

`scripts/` 不是另一个应用层。

适合 script 的任务：

- 创建和清理隔离测试数据库；
- 用同一参数顺序调用几个外部工具；
- 检查生成结果是否干净；
- 封装具有明确安全检查的本地开发操作。

不适合 script 的任务：

- 产品规则；
- authority 判断；
- package ownership；
- 复杂状态机；
- HTTP 或数据库业务逻辑；
- 一份手工维护的全仓 package registry；
- 只转发一条 `go`、`pnpm` 或 `docker` 命令。

### Script 规则

- 使用 lowercase kebab-case；
- 一个文件只完成一个结果；
- Bash 使用 `set -euo pipefail`；
- 提供简短 `usage`；
- 写操作先验证目标，危险操作 fail closed；
- 临时文件使用 `mktemp` 并通过 trap 清理；
- 不读取未声明的 ambient credential；
- 不在失败时留下后台进程或测试数据库；
- 输出人可读，成功时保持安静；
- 复杂 script 必须有测试或 fixture。

当 Bash 开始解析复杂 JSON、维护状态机、实现并发调度或复制 Go 领域类型时，停止继续扩写。优先把可证明规则放回 owner 的 Go test，或使用现成工具，而不是建立仓库私有脚本框架。

禁止创建：

```text
scripts/lib/common.sh
scripts/helpers.sh
scripts/utils.sh
```

两个 script 只有几行相同命令时允许重复。不要用 shell function library 隐藏执行路径。

## 8. 测试数据库脚本保持通用而不中央注册 package

PostgreSQL test runner 只负责：

1. 验证目标是本机测试 PostgreSQL；
2. 创建唯一隔离数据库；
3. 运行 migration；
4. 把 `DATABASE_URL` 交给调用方指定的测试命令；
5. 无论成功失败都清理数据库。

数据库名使用：

```text
carry_test_<UTC timestamp>_<hex12>_postgres
```

runner 不维护“哪些 package 使用数据库”的中央列表。需要数据库的 package 自己通过统一 test helper 或调用参数声明需求。

数据库不存在、无法连接或 migration 失败都是 FAIL，不是 SKIP。

## 9. Generated code 与手写代码物理分开

允许提交的生成代码必须满足：

- 构建或开发者使用确实需要；
- source schema 是唯一编辑入口；
- `make generate` 可以完整重建；
- 生成目录明确；
- 手写代码不混入生成目录；
- CI 生成后要求 working tree 无 diff。

建议位置：

```text
sqlc.yaml                    sqlc 唯一配置入口
internal/postgres/queries/   手写 query source
internal/postgres/dbsqlc/    sqlc 生成代码
apps/web/app/generated/      协议生成客户端、类型和 Zod schema
```

规则：

- PostgreSQL application query 必须进入 sqlc query source，不在 Go 文件中散落 SQL string；
- SQLC 只能通过 `make generate` 运行；
- 不手改生成文件；
- 不对生成文件做美学拆分；
- review 重点看 source SQL/schema 和生成结果是否符合预期；
- generated directory 从重复、复杂度和手写文件统计中排除。

Web `dist/`、coverage、Playwright report、临时协议输出和 container build context 不提交 Git。

不要把 Web build assets 复制并提交到 Go handwritten package。Release pipeline 负责组合发布物。

## 10. CI 只证明当前产品

CI 的目标不是运行所有可能的工具，而是用最短、最稳定的路径阻止真实回归。

第一版 PR workflow 的基线是：

```text
.github/workflows/ci.yml
```

开始正式发布后，再由真实发布路径增加：

```text
.github/workflows/release.yml
```

不按语言、目录、实验或每个检查拆出十几个 workflow。

## 11. PR CI 设计

V1 默认按三个失败 owner 分组，但不把 job 数量写成永久上限。

### `go`

一次 Go toolchain setup 后调用：

```text
make check-go
```

该 target 负责证明全部手写 Go（包括 `cmd/`、`internal/`、`e2e/`、`scripts/` 与仍保留的 `experiments/`）格式与 module 文件干净、generated code 可重建、Go 静态检查和测试通过、真实 PostgreSQL 集成测试实际运行，并且 `carry-server` 与 `carry` 可以构建。具体命令只在 Makefile 和工具配置中维护。

### `web`

一次 Node 24 与 pnpm setup 后调用：

```text
make check-web
```

该 target 在 `apps/web` 的唯一 lockfile 下完成 frozen install、格式与类型检查、测试和 production build。具体 pnpm 命令不复制到 workflow。

### `product`

调用：

```text
make check-product
```

该 target 启动 PostgreSQL、carry-server 和必要 Web runtime，只运行当前已经实现的最高价值旅程：

- 成员建立 Browser Session；
- 创建、读取并补充 Work；
- Machine enrollment 后用独立 mTLS claim 并推进 Work；
- Host 中断后新的 Attempt 安全继续，旧 Attempt 不能晚到提交；
- 成员在 Web 私聊 Carry，普通问题只得到私人回复；
- 清晰委托原子形成一份共享 Work，而私人原文和 source identity 不进入 Work。

未来 journey 只有在实现并成为发布合同后才进入 required product suite。需要真实模型 credential 的 Pi/Codex canary 不在不可信 PR 上运行，改由 protected canary 执行。

### 为什么不拆更多 job

每增加一个 job 都会重复 checkout、toolchain setup、cache、权限和日志入口。

只有以下情况才拆 job：

- 需要不同 operating system；
- 需要不同 secret 权限；
- 运行时间差异足以显著缩短反馈；
- 失败 owner 完全不同。

“一个 Make target 一个 job”不是拆分理由。

## 12. CI 不复制项目逻辑

Workflow YAML 只负责：

- trigger；
- permissions；
- concurrency；
- toolchain setup；
- service container；
- 调用 `make check-go`、`make check-web` 或 `make check-product`；
- 上传必要 artifact。

不在 YAML 中编写几十行 migration、测试数据库、release 或验证逻辑。

同样，Makefile 和 scripts 不能反过来重新实现 GitHub Actions 的权限、matrix 和 artifact 行为。

正确层次：

```text
Workflow 决定在哪个环境运行
Makefile 暴露稳定项目命令
Script 编排少量跨工具细节
Go/TS test 证明产品规则
```

每条规则只属于其中一层。

## 13. CI 安全

- Workflow 顶层默认 `contents: read`；
- job 只申请自己需要的额外权限；
- 第三方 Action 固定完整 commit SHA；
- fork PR 不获得 repository、cloud、Connector 或模型 secrets；
- 不使用 `pull_request_target` 执行 PR 代码；
- deployment 使用受保护 environment 和短期 OIDC credential；
- cache key 不接收不可信脚本输出；
- PR test 不连接生产数据库、生产 Space 或真实客户渠道。

需要 secrets 的 live canary 使用独立、受保护、手动或定时 workflow，并且只操作专用 canary Space。

## 14. 慢检查放在正确位置

不是所有检查都应该阻塞每个 PR。

### PR required

- format；
- generated clean；
- unit/integration；
- build；
- 少量关键 product journey。

### Main 或 nightly

- 全量 race；
- reachable dependency vulnerability scan；
- 较长浏览器矩阵；
- 已经实现的第三方能力 fixture 矩阵；
- Pi/Codex 真实 native canary；
- container build 与扫描。

### Release

- 从干净 tag 构建；
- 全平台 `carry` binaries；
- `carry-server` image；
- Web release assets；
- checksum、SBOM 和 provenance；
- migration upgrade evidence。

一项检查只有在过去阻止过真实缺陷，或保护明确发布合同后，才升级为 PR required。

## 15. 不使用过早的 CI 优化

第一版不使用：

- 复杂 path filter；
- 动态 job generator；
- reusable workflow 层层调用；
- 自建 test scheduler；
- 大型 matrix；
- 根据 diff 猜测可跳过哪些领域测试；
- 多级 cache 调优；
- flaky test 自动重试隐藏失败。

先完整运行小而快的 suite。只有 CI 数据证明时间或费用成为问题，才增加优化，并记录它可能漏测的边界。

## 16. Experiments 不成为永久 CI 面

实验默认通过自己的 README 命令运行。

只有以下情况才进入 CI：

- 当前架构决定仍依赖这份证据；
- 实验代码仍在活跃修改；
- 失败会改变当前发布判断。

实验 promotion 后：

- 测试迁入真实 owner；
- 删除实验 module 和专用 CI job；
- README 保留最终结论或直接删除；
- 不同时维护实验实现和生产实现。

实验被否决后删除代码，只在对应 Issue/ADR 留结论。

## 17. Boundary 检查从简单开始

Node 0 建立一个小型 `scripts/check-boundaries`，从第一天保护已经确定的稳定方向：

```text
Domain 不能导入 server/postgres/connector/agent
carry-server 不能导入本地 Agent 进程实现
禁止 common/utils/platform/registry 等垃圾桶目录
```

它只使用 `go list` 和路径检查表达禁止方向，不维护完整 package dependency manifest，也不枚举所有允许 package。正常新增一个已赚得的 owner 不应要求更新中央 allowlist。

其他依赖质量仍由 Go `internal` 结构、小而清楚的 package API 和 architecture review 保护。只有重复出现的新违规类型，才给脚本增加一条稳定规则。概念预算不能靠中央 allowlist 维护；无消费者的 owner 必须删除，而不是加入例外。

## 18. Release 文件不污染源码

Release 产物只进入 CI artifact、release page 或 image registry：

```text
carry-server image
carry binaries
Web dist
checksums
SBOM
provenance
```

仓库不提交：

```text
bin/
dist/
coverage/
playwright-report/
embedded-webdist/
release bundles
```

`carry-server` 与 Web 的最终组合由 release pipeline 完成，不把编译结果复制进 `internal/server` 后再提交。

## 19. 删除规则

每个功能或重构节点结束时必须同时检查：

- 被替代的 package；
- 旧 route；
- 旧 migration helper；
- 无调用 Make target；
- 无调用 script；
- 实验 module；
- CI job；
- generated output；
- plan、review 和 handoff 文档；
- dependency 和 tool config。

删除条件不是“以后永远不会用”，而是“当前没有消费者、合同或仍有效证据”。未来需要时从当时真实需求重新建立。

## 20. 仓库整洁审查

合并重要改动前回答：

1. 新文件是否放在事实 owner 旁边？
2. 是否创建了新的中央清单或垃圾桶？
3. 本地和 CI 是否调用同一命令？
4. 一条检查是否被重复编码在 YAML、Makefile 和 script？
5. generated code 是否与手写代码分开？
6. 实验是否已经有 promotion/retirement 结论？
7. 是否留下被替代的代码、文档、target 或 job？
8. 删除新抽象后，仓库是否反而更容易理解？

如果最后一个答案是“是”，不要合并这个抽象。

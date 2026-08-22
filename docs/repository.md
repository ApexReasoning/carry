# Carry 仓库与 CI

本文件拥有目录、文档位置、Makefile、scripts、生成代码、CI 和删除规则。产品由 `docs/product.md` 决定，结构由 `docs/architecture.md` 决定，节点路线由 `docs/implementation.md` 决定。

仓库要能回答三个问题：产品代码在哪里；谁拥有这段代码；用哪一个命令证明改动正确。

只有当前产品、真实工具链、已发布协议和仍然有效的证据进入长期导航。研究、评审、审计和证据 artifact 属于节点的 GitHub Issue 与 PR，不进入仓库目录。

## 1. 根目录

当前 V1 的预期形态，不是冻结所有工具配置文件的穷举清单：

```text
.
├── .github/workflows/
├── apps/web/
├── cmd/{carry,carry-server}/
├── docs/
├── e2e/
├── internal/
├── protocol/user/v1/openapi.yaml
├── scripts/
├── AGENTS.md
├── Makefile
├── compose.yaml
├── go.mod
├── go.sum
└── README.md
```

- 不为未来能力预建目录；
- 新根目录必须代表独立工具链、发布物或长期证据边界；
- 一个普通 package 不成为根目录，一个临时任务不成为根目录；
- 删除最后一个消费者时同时删除空目录。

## 2. 代码位置

### `cmd/`

只放发布入口与 composition root。入口负责：进程 `context`、信号取消与退出码；解析浅层 operator 配置；在一个可见位置静态构造具体 adapter、已有 owner 行为和入站路由；注入版本信息。业务规则不放在 `cmd/` 或 `internal/cli/`。

`carry` 的 Cobra adapter 在 `internal/cli/`，root 在一个可见位置静态组合两套表面：人用来把一台 Host 接入 Space、查看和移除它的浅层 operator 表面，和 Host 启动的 Agent 使用的本机表面。Agent 侧只调用本机 Host，不读成员凭据，不直连 Server。禁止 `init()` 注册、动态 command registry，以及向所有命令暴露所有凭据/客户端的万能 Factory。准确的命令、字段与传输由节点 15、16、18 的研究决定。

`carry-server` 的操作面仍浅，继续用标准库 `flag`。两个二进制不为形式对称共享 CLI framework。

### `internal/`

按事实 owner 和具体进程 adapter 组织。当前树：

```text
internal/
├── identity/ space/ conversation/ work/ machine/ run/
├── agent/{pi,codex}/        具体进程 adapter（待迁走）
├── host/
├── cli/{host,login,credentialfile,userapi,work}/
├── postgres/
└── server/
```

目标树：

```text
internal/
├── identity/ space/ agent/ conversation/ work/ machine/ run/
├── host/{pi,codex}/         具体进程 adapter，只由 Host 组合使用
├── cli/                     浅层 operator 接入表面 + Agent 本机表面
├── postgres/
└── server/
```

两处结构性迁移，由拥有它们的节点执行，不提前部分完成：

- `internal/agent/` 由具体 adapter 的家变成 Agent 身份 owner（节点 15）；
- `internal/agent/pi|codex` 迁到 `internal/host/pi|codex`（节点 15），旧路径在同一节点删除，且不建立 registry。

`internal/cli/login`、`internal/cli/credentialfile`、`internal/cli/userapi`、`internal/cli/work` 及其 Identity、Server、PostgreSQL、OpenAPI、Web 垂直路径承载的是作废的人用 CLI 产品，由节点 18 在 Agent 本机创建路径成立后整体删除（见 `docs/implementation.md` §7）。

`internal/server/` 只是入站 HTTP/Host transport、worker 组合与路由组合：多步策略回到现有 owner，不在 server 里建第二个 service 层。不同 actor 的同名操作各有自己的 handler。

禁止新增 `internal/common`、`internal/utils`、`internal/platform`、`internal/integration`、`internal/registry`。

### `apps/web/`

Web 使用自己的 Node、pnpm、TypeScript、React 工具链。`package.json`、lockfile 和 Node 版本文件都留在 `apps/web/`，仓库根目录不再维护第二份 JavaScript workspace。

Web 按产品概念组织 feature，不复制 Go package 树。User API 的 OpenAPI source 在 `protocol/user/v1/openapi.yaml`，只覆盖人通过 Web 消费的行为；生成客户端只放在 `apps/web/app/generated/`。面向 Agent 的本机接口不消费 User API。Host API 由 Machine mTLS 受众使用，只有在发布后确实需要独立生成和维护时才增加 versioned protocol source。

### `e2e/`

只放少量跨进程、经公开协议运行的关键旅程。文件按长期产品行为命名，不按节点、里程碑或评审批次命名。跨旅程共用的 process/build/network fixture 集中在一个 harness 文件，owner-specific fixture 留在对应行为文件旁边。package 集成测试留在 owner 附近。

### `scripts/` 与 `experiments/`

没有真实内容时不创建。实验必须有明确决策问题、独立 README、独立 fixture、可重复命令、promotion 与 retirement 条件；不进入生产 import graph，不默认进入 PR required CI。问题回答后只保留仍有决策价值的最小证据。

## 3. 文档只保留一个当前答案

七个文件是当前唯一的规范文档：

```text
AGENTS.md              coding agent 执行合同与门顺序
README.md              仓库入口
docs/product.md        界面、动作、词汇、隐私、不变量
docs/architecture.md   owner、拓扑、权限、并发、依赖方向
docs/code-style.md     可执行的美学门
docs/repository.md     路径、命令、生成代码、CI、删除
docs/implementation.md 节点路线、研究程序、评审协议、证据
```

### 节点 artifact 属于 Issue

每个节点开工前开一个 GitHub Issue。它是这些内容的持久 owner：十字段旅程冻结；冻结问题与反证问题；来源相关性矩阵；准确的 Loop 考古目标；八列证据行；真实或受阻 canary；精确设计冻结（package、准确文件预算、删除范围、事务阶段、证据、命令、不做）。评审结论留在 PR，CI 证据留在 CI artifact。

明确不建立：`docs/plans/`、`docs/reviews/`、`docs/handoffs/`、`docs/audits/`、`docs/evidence/`，以及任何按节点拆出的子计划文件。只读的 `carry.supervisor` 只维护稳定标准和每次 delta review，不把历史审计复制回仓库。

将来只有出现真实需要时才增加：少量长期 ADR（代价高、难反转、无法从代码推断、仍在约束当前实现）；`docs/operations.md`（首次生产部署准备开始时）；`docs/protocols/`（已发布且需要独立维护的协议）；`docs/runbooks/`（真实事故路径）。

`implementation.md` 是第一版建设期唯一允许留在仓库的活动计划，第一版关闭时删除。被替代的文档直接删除，Git 历史已经保留它。

## 4. Makefile

```text
make format        自动格式化手写代码
make generate      通过仓库固定版本的 sqlc 运行所有代码生成
make test          快速单元测试
make test-db       真实 PostgreSQL 集成测试
make check-go      只读验证 Go、生成代码、数据库测试和两个二进制
make check-web     只读验证 Web 格式、类型、测试和 build
make check-product 少量关键浏览器旅程
make check-vulnerabilities 扫描当前 Go 调用图中的可达漏洞
make check         组合三个 PR required check target
make build         构建 carry-server、carry 和 Web
make dev           启动本地开发环境
```

- 一个 target 对应一个开发者能说清楚的结果；不为每个底层命令建 target；
- 不在 Makefile 里写大段 Bash；
- target 不根据环境偷偷改变安全语义；
- CI 与本地调用同一个 `check-*` target；
- `check-*` 只读：格式或生成结果不正确时失败，不能自动修改后通过；
- 失败时输出真实失败命令和修复入口；
- 不保留无人调用的 target。

## 5. Scripts

适合：创建和清理隔离测试数据库；用同一参数顺序调用几个外部工具；检查生成结果是否干净；封装带明确安全检查的本地开发操作。

不适合：产品规则；权限判断；package ownership；复杂状态机；HTTP 或数据库业务逻辑；手工维护的全仓 package 清单；只转发一条 `go` / `pnpm` / `docker` 命令。

规则：lowercase kebab-case；一个文件一个结果；Bash 用 `set -euo pipefail`；提供简短 `usage`；写操作先验证目标，危险操作 fail closed；临时文件用 `mktemp` 并 trap 清理；不读未声明的 ambient credential；失败时不留后台进程或测试数据库；成功时保持安静；复杂 script 必须有测试或 fixture。

禁止 `scripts/lib/common.sh`、`scripts/helpers.sh`、`scripts/utils.sh`。两个 script 有几行相同命令时允许重复，不用 shell function library 隐藏执行路径。

### 测试数据库 runner

只做五件事：验证目标是本机测试 PostgreSQL；创建唯一隔离数据库；跑 migration；把 `DATABASE_URL` 交给调用方指定的测试命令；无论成败都清理。

数据库名：`carry_test_<UTC timestamp>_<hex12>_postgres`。

runner 不维护"哪些 package 用数据库"的中央列表。数据库不存在、连不上或 migration 失败是 FAIL，不是 SKIP。

## 6. 生成代码与手写代码物理分开

```text
sqlc.yaml                    sqlc 唯一配置入口
internal/postgres/queries/   手写 query source
internal/postgres/dbsqlc/    sqlc 生成代码
apps/web/app/generated/      协议生成客户端、类型和 Zod schema
```

- 已提交并可能在任何环境执行过的 migration 是不可变历史：永不修改、删除或重排；删除/替换 live schema、约束、索引或数据语义一律新增前向 migration，并用升级测试证明旧库可前进；
- 应用 SQL 必须进入 sqlc query source，不在 Go 文件里散落 SQL 字符串；
- sqlc 只通过 `make generate` 运行；不手改生成文件；不对生成文件做美学拆分；
- CI 在生成后要求 working tree 无 diff；
- review 看的是 source SQL/schema 与生成结果是否符合预期；
- 生成目录从重复度、复杂度和 `docs/code-style.md` 的全部规则中排除。

Web `dist/`、coverage、Playwright report、临时协议输出和 container build context 不提交。不把 Web build assets 复制进 Go package 再提交；发布组合由 release pipeline 完成。

## 7. CI

CI 的目标是用最短、最稳定的路径阻止真实回归。V1 只有一个 PR workflow，正式发布后再由真实发布路径增加一个 release workflow。不按语言、目录、实验或每个检查拆出十几个 workflow。

PR 按三个失败 owner 分组，每组一次 toolchain setup 后调用一个 target：`make check-go`、`make check-web`、`make check-product`。Main push 另跑 `make check-vulnerabilities`，避免外部漏洞库可用性阻断普通 PR，同时阻止已知可达漏洞留在主分支。具体命令只在 Makefile 和工具配置中维护。

只有下列情况才拆更多 job：需要不同操作系统；需要不同 secret 权限；运行时间差异足以显著缩短反馈；失败 owner 完全不同。"一个 Make target 一个 job"不是理由。

### 分层

```text
Workflow 决定在哪个环境运行
Makefile 暴露稳定项目命令
Script   编排少量跨工具细节
Go/TS test 证明产品规则
```

每条规则只属于其中一层。Workflow YAML 只做 trigger、permissions、concurrency、toolchain setup、service container、调用 target、上传必要 artifact。

### 安全

顶层默认 `contents: read`；job 只申请自己需要的额外权限；第三方 Action 固定完整 commit SHA；fork PR 不获得任何 secrets；不用 `pull_request_target` 执行 PR 代码；deployment 使用受保护 environment 与短期 OIDC 凭据；cache key 不接收不可信脚本输出；PR 测试不连接生产数据库、生产 Space 或真实客户渠道。需要 secrets 的 live canary 使用独立、受保护、手动或定时的 workflow，只操作专用 canary Space。

### 检查放在哪一层

- **PR required**：format；generated clean；unit/integration；build；少量关键产品旅程。
- **Main 或 nightly**：全量 race；可达依赖漏洞扫描；较长浏览器矩阵；Pi/Codex 真实 canary；container build 与扫描。
- **Release**：从干净 tag 构建；全平台 `carry` binaries；`carry-server` image；Web release assets；checksum、SBOM、provenance；migration 升级证据。

一项检查只有在过去阻止过真实缺陷，或保护明确的发布合同后，才升级为 PR required。

第一版不使用：复杂 path filter；动态 job generator；层层调用的 reusable workflow；自建 test scheduler；大型 matrix；按 diff 猜测可跳过的领域测试；多级 cache 调优；用自动重试隐藏 flaky 失败。

## 8. 边界检查

一个小型 `scripts/check-boundaries` 用 `go list` 和路径检查表达少数稳定的禁止方向：

```text
owner package 不能导入 server / postgres / host / 具体进程 adapter
carry-server 不能导入本地 Agent 进程实现
禁止 common / utils / platform / registry 等垃圾桶目录
```

它不维护完整依赖清单，也不枚举所有允许 package。新增一个已赚得的 owner 不应要求修改中央 allowlist。只有重复出现的新违规类型才增加一条稳定规则。无消费者的 owner 必须删除，而不是加入例外。

## 9. 删除

每个节点结束时同时检查：被替代的 package；旧路由；旧 migration helper；无调用的 Make target 与 script；实验 module；CI job；生成输出；依赖与工具配置。

删除条件不是"以后永远不会用"，而是"当前没有消费者、没有合同、没有仍然有效的证据"。未来需要时从当时的真实需求重新建立。

## 10. 合并前的仓库检查

1. 新文件是否放在事实 owner 旁边，并以一个 owner 行为命名？
2. 是否创建了新的中央清单或垃圾桶目录？
3. 本地和 CI 是否调用同一个命令？
4. 同一条检查是否被重复编码在 YAML、Makefile 和 script 里？
5. 生成代码是否与手写代码分开？
6. 节点 artifact 是否留在 Issue 与 PR，而不是新增仓库文档？
7. 是否留下被替代的代码、文档、target 或 job？
8. 删掉这个新抽象后，仓库是否反而更容易理解？

第 8 条答"是"就不要合并这个抽象。

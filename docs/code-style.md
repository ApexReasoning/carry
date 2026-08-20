# Carry 代码美学

## 1. 目标

代码美学不是格式整齐，也不是抽象越多越高级。

Carry 的代码必须让读者快速看清：

- 这段代码属于谁；
- 它保护什么事实；
- 谁可以调用它；
- 成功路径是什么；
- 失败后留下什么；
- 删除它会失去什么。

最重要的标准是：

> 用最少的概念，完整表达当前真实行为。

这条标准服从 Carry 的“克制与自由”：

- 对 identity、authority、causality、time、outcome 和 invalid state 保持克制，用准确类型、事务和失败路径限制它们；
- 对自然语言、目标、解释方法和 concrete adapter 保持自由，不用 enum、通用接口和提前抽象缩窄它们；
- 少代码不是独立目标。删除证明真实或权限所需的代码不是克制，而是缺陷；
- 多扩展点也不是自由。没有消费者的 option、interface、registry 和 compatibility path 只会限制未来重新设计。

代码 review 因此同时问两个问题：这段约束保护了什么真实不变量？这段结构是否无必要地排除了合法路径？

格式交给工具。Review 时间用于判断 ownership、authority、failure 和 unnecessary code。

### 执行原则

`AGENTS.md` 唯一定义“不确定性先显式化、只建立 earned structure、范围克制且纵向完整、以直接证据完成”四条执行原则。本文件只补充代码表达：规则应让 owner、authority、failure 与 deletion path 可读；必要事务和测试不因缩短代码被删除；没有当前消费者的 abstraction 不因形式整齐被保留。

## 2. 代码首先服从架构

目录、package、文件和类型都不能自行创造架构。新边界只使用 `docs/architecture.md` 的概念准入问题，本文件不建立第二套准入标准。

边界一旦成立，代码仍应让它拥有的事实、不变量和当前消费者清晰可见。名词、文件长度、表结构、UI 视图、语法相似或目录对称都不能单独证明一个 package。

明确禁止没有 owner 的垃圾桶：

```text
common
utils
helpers
platform
integration
provider
registry
resource
tool
runtime
manager
orchestrator
models
shared
```

`shared` 只能用于已经失去产品含义的前端交互基础，例如 Button 或 Dialog；不能成为产品代码回收站。

## 3. 一份事实只有一个 owner

同一个状态不能在 domain、PostgreSQL、HTTP 和 Web 中各保存一份“当前真相”。

正确方向：

```text
Domain 拥有意义与规则
PostgreSQL 保存并原子维护事实
HTTP 定义传输协议
Web 展示服务端已经裁决的结果
```

错误方向：

```text
HTTP 重新判断权限
Web 根据按钮可见性推断权限
PostgreSQL adapter 发明新的领域状态
Agent prompt 代替服务端规则
```

跨边界转换必须改变表示或验证新的事实。纯字段搬运且没有边界意义的转换应删除。

## 4. 名字必须直接说明行为

好名字让调用点不需要跳进实现。

推荐：

```go
works.Create(ctx, command)
runs.Claim(ctx, machineID)
machines.Revoke(ctx, command)
```

避免：

```go
manager.Process(ctx, data)
service.Handle(ctx, item)
utils.Normalize(value)
```

### 统一动词

| 动词 | 含义 |
| --- | --- |
| `Create` | 建立新的持久事实 |
| `Record` | 保存已经发生的事实 |
| `Claim` | 原子取得有限期处理权 |
| `Renew` | 延长当前有效 claim |
| `Commit` | 基于准确 version/fence 原子提交已经验证的结果 |
| `Find` | 查找可能不存在的单个值 |
| `Load` | 读取必须存在的值 |
| `List` | 读取集合 |
| `Parse` | 验证并转换外部表示 |
| `Decode` | 解码，不增加产品意义 |
| `Format` | 生成展示文本 |
| `Diagnose` | 只读判断物理状态 |
| `Reconcile` | 用新证据收敛 Unknown |

避免 `Do`、`ExecuteThing`、`ProcessData`、`HandleItem` 等没有决定含义的名字。

### 名词规则

- 当前已经确认的 owner 是 Identity、Space、Conversation、Work、Machine 和 Run；Pi/Codex 是具体 adapter；
- 不为同一概念创造缩写、别名或“更技术”的第二套名字；
- Evidence、Completion、Observation、Draft 等行为角色不能仅凭用途获得 ID、表、package 或 API；
- ID 在代码中保留 owner：`workID`、`runID`，不要只写 `id`；
- 布尔值表达肯定命题：`isOpen`、`hasUnappliedInput`、`canSubmit`；
- 避免双重否定和布尔参数组合。

## 5. Package 只暴露最小合同

一个 package 应能用一句话说明自己拥有的事实。

规则：

- 默认不导出；
- 只有另一个真实 owner 需要时才导出；
- 接口由消费者定义；
- 构造函数返回具体类型；
- 不建立通用 CRUD Repository；
- 不用 service locator、全局容器或隐式 registry 发现依赖；
- composition root 显式传入具体实现。

接口示例：

```go
// Host owns this exact consumer need.
type RunClient interface {
    Claim(context.Context) (run.Claim, error)
    Renew(context.Context, run.Claim) (time.Time, error)
}
```

避免：

```go
type Repository[T any] interface {
    Create(context.Context, T) error
    Update(context.Context, T) error
    Delete(context.Context, string) error
    List(context.Context) ([]T, error)
}
```

少量重复比错误抽象便宜。只有共同语义和共同变化原因都已经出现，才提取抽象。

## 6. 文件讲述一个行为故事

文件按 owner 和行为命名，不按机械类别命名。

推荐：

```text
work_message.go
machine_enrollment.go
run_claim.go
run_commit.go
browser_session.go
```

避免：

```text
types.go
models.go
interfaces.go
helpers.go
utils.go
constants.go
manager.go
```

规则：

- 一个文件可以包含完成同一行为所需的类型、验证和私有辅助函数；
- 不为每个 struct 建文件；
- 不为每个函数建文件；
- 不因为行数拆分；
- 当一个文件跨越两个 owner、两个协议或两个独立失败生命周期时才拆分；
- 测试文件与行为文件相邻并使用相同主题名称。

目录深度不是设计质量。能在一个清楚 package 中表达的行为，不拆成多层转发目录。

## 7. 函数表达一个完整决定

函数应有一条清楚主路径。

推荐早返回：

```go
if !work.IsOpen() {
    return ErrWorkClosed
}
if !authority.CanRevise(work) {
    return ErrForbidden
}
return store.Revise(ctx, command)
```

避免多层嵌套、隐式 fallback 和跨抽象层跳跃。

应该拆分函数的情况：

- 名字已经无法概括整个行为；
- 同时处理协议解析、产品决定和数据提交；
- 某段逻辑拥有独立不变量或测试；
- 参数来自多个无关 owner；
- 资源获取与资源使用的责任不清楚。

不应该拆分的情况：

- 仅为了满足行数指标；
- helper 只是转发相同参数；
- 读者需要来回跳转才能看完一条简单路径；
- 抽取后没有新的准确名字。

## 8. 类型只保护真实不变量

类型用于阻止错误，不用于装饰代码。

值得独立类型的例子：

- authority 或 generation；
- canonical digest；
- credential；
- Work lifecycle；
- Run fence；
- 已经真实出现的外部 outcome。

不值得独立类型的例子：

- 没有验证规则的普通字符串 wrapper；
- 只为了少写参数创建的 `Options`；
- 同一 package 内字段完全相同的重复 DTO；
- 用泛型重命名普通集合；
- 既有事实在一次判断中的角色，例如 Evidence；
- 只在一个函数调用之间短暂存在、没有独立 lifecycle 的 Completion 或 Observation。

一个候选类型如果没有独立 identity、lifecycle 或 invalid state，应优先使用准确字段、返回值或相邻 owner 的 command。

已知物理事实必须强类型。自然语言和第三方内容可以保持开放，但开放内容不能替代：

- authority；
- lifecycle；
- time；
- idempotency；
- causality；
- external outcome。

零值只有在领域中确实有效时才可用。缺少 owner、authority 或目标不能被解释成默认权限。

## 9. 权限在代码中必须可见

高后果写入的调用签名应让 reviewer 看见：

- actor；
- target；
- expected version；
- authority 或 fence；
- idempotency identity。

不能依赖：

- Prompt 中的一句话；
- 可见按钮；
- Agent 自我声明；
- 历史上曾经有权；
- 空配置选择的默认身份。

预检查不是最终授权。并发可改变的权限必须与写入在同一事务中重新验证。

没有安全含义的默认值可以存在。涉及权限、隔离、数据保留和 provider 选择的默认值必须显式。

## 10. 失败路径保持真实

错误必须保留操作、对象和根因。

```go
return fmt.Errorf("claim Run %s: %w", runID, err)
```

错误字符串使用小写，不加句号，不写无意义的 `operation failed`。

规则：

- 内层负责增加上下文；
- 最外层日志一次；
- 不同时 log 又 return 同一个错误；
- 忽略错误必须有协议级理由；
- 外部响应丢失不能被改写成 Failed；
- 只有安全幂等或只读证据存在时才重试；
- 不静默切换 Runtime、credential、Host 或 provider。

错误类型只在调用方确实需要稳定分支时建立。不要把所有错误都变成复杂 response union。

## 11. 并发和资源必须有 owner

### Goroutine

每个 goroutine 必须回答：

- 谁启动；
- 谁取消；
- 错误交给谁；
- 谁等待退出；
- 进程关闭时如何收敛。

构造函数不得偷偷启动无人管理的后台任务。

### Context

- `context.Context` 是 Go 方法的第一个参数；
- 不保存在长期 struct 中；
- caller 决定取消；
- 已经观察到的外部结果不能因为 caller 断开而丢失；
- cleanup 使用明确、有限的独立期限。

### Resource

创建进程、listener、socket、临时目录或文件的 owner 同时负责关闭。

成功的 `Close` 应可重复调用。失败的 cleanup 不能忘记尚未完成的责任。

### Transaction

禁止跨事务 check-then-act：

```text
先读权限
→ 离开事务
→ 再写数据
```

必须改为锁定或条件更新，在一个事务中建立完整不变量。

网络调用不放在数据库事务中。

## 12. Go 代码规则

Go 是 `carry-server`、`carry` CLI 与 Host 的实现语言。

### Package

- package 名使用简短、单数、小写词；
- package 名表达 owner，不表达层：`work`、`run`，而不是 `workservice`；
- package context 已经说明的词不在导出名中重复：`run.State`，不是 `run.RunState`；
- 不创建只有一个转发函数的 package。

### Constructor

保留 constructor 的条件：

- 建立有效不变量；
- 注入必须依赖；
- 获得并拥有资源；
- 隐藏有意私有的表示。

仅复制字段的 `NewThing` 应删除并使用 struct literal。

### Interface

- 在消费者 package 定义；
- 只包含当前消费者需要的方法；
- 不为了 mock 预先给每个 struct 建接口；
- constructor 返回具体实现；
- 测试替换发生在真实边界。

### Representation

- domain struct 不带数据库和 HTTP 双重 tags；
- sqlc 类型不离开 PostgreSQL adapter；
- HTTP response 使用私有 wire 类型；
- restore 函数负责把数据库 nullability 转为有效 domain 值并检查持久不变量。

### Go 工具

提交前至少通过：

```text
gofmt
go test
go vet
```

需要额外 linter 时，只增加能防止真实缺陷或保护架构合同的规则。

## 13. TypeScript 代码规则

TypeScript 用于 Web，不复制服务端领域裁决。

### 类型

- 开启 `strict`；
- 外部 JSON 从 `unknown` 开始并在边界验证；
- 不使用 `any`、双重 assertion、`@ts-ignore` 或无依据的 non-null assertion；
- 使用 discriminated union 表达真正互斥的状态；
- 使用 literal union，除非确实需要 runtime enum；
- generic 必须保存调用前后真实类型关系。

简单类型优于复杂 `Pick`、`Omit`、mapped 和 conditional type 组合。

### 文件和模块

TypeScript 文件使用 lowercase kebab-case：

```text
work-summary.tsx
work-api.ts
work-schema.ts
```

规则：

- 类型放在拥有它的行为旁边；
- 不建立全局 `types.ts`、`services.ts`、`constants.ts` 或 `utils.ts`；
- 不使用宽泛 barrel 隐藏依赖；
- 使用 `import type`；
- API client 和 protocol types 从明确协议生成，不手工复制 Go struct。

### 函数

- 顶层命名行为使用 function declaration；
- callback 和短闭包使用 arrow function；
- 本地明显类型让编译器推断；
- exported contract 显式可读；
- async 操作由 caller 提供 AbortSignal；
- 较旧请求不能覆盖较新结果。

## 14. React 代码规则

### 组件角色

只有三种组件角色：

1. Route：拥有 URL、loader/action 和页面组合；
2. Feature：使用产品语言表达一段行为；
3. UI primitive：拥有无产品含义的交互和可访问性。

Provider 只用于稳定应用能力，例如身份或主题，不作为页面数据仓库。

### 组件边界

提取组件必须至少满足一项：

- 有准确产品或交互名称；
- 独立拥有状态或行为；
- 与父组件有不同变化原因；
- 已有真实复用且不需要模式 flags；
- 单独测试能明显表达合同。

不要因为 JSX 超过若干行就拆分，也不要为每个组件建立文件夹和 `index.ts`。

### State 与 Effect

- state 放在改变它的最近 owner；
- server state 优先属于 route loader/action/fetcher；
- 不把 props 或可推导值复制进 state；
- Effect 只同步 React 外部系统；
- 用户点击触发的操作放在 handler；
- 每个订阅、timer 和 async Effect 都有 cleanup 或 stale-result 防护；
- 不关闭 exhaustive dependency 检查来控制时序。

### 产品语言

Web 页面说：

- 当前情况；
- 下一步；
- 结果；
- 等待谁；
- 需要成员决定什么。

普通页面不暴露 lease、fence、attempt、generation 或 provider Session。

### Accessibility

- 优先使用语义 HTML；
- 所有交互支持键盘和可见 focus；
- 表单有 label 和可理解错误；
- icon-only button 有 accessible name；
- 颜色不是唯一状态载体；
- motion 尊重 reduced-motion；
- WCAG 2.2 AA 是发布底线。

## 15. SQL 与迁移

SQL 不是 repository 的内部细节；它负责并发真相。

规则：

- 表和列使用 owner 语言；
- constraint 表达数据库能够证明的不变量；
- claim、fence、连续序号和唯一 winner 在 SQL 中原子完成；
- application query 使用 sqlc；手写 SQL 只存在于 migration 和 sqlc query source；
- query 名称描述 use case，不描述 CRUD；
- 一个 transaction query 可以跨多个表，不为每张表建立 repository；
- migration 一旦进入共享历史就不可修改；
- destructive migration 使用 expand → migrate → contract；
- 不手改 sqlc 生成文件；
- SQLC 只通过 `make generate` 生成并要求无 diff。

focused PostgreSQL test 必须使用真实数据库。数据库缺失导致的 skip 不算 pass。

## 16. HTTP 与协议

协议是明确边界，不是 domain struct 的自动序列化。Go HTTP 路由统一使用 chi；handler 仍使用标准 `net/http` 类型，不建立第二层 controller framework。

规则：

- route、method 和 path parameter 由 chi 明确声明；
- 不混用 `http.ServeMux`、其他 router 或自建 route switch；

- request 先验证 syntax，再交给 domain 验证 meaning；
- response 不泄漏数据库结构；
- mutating endpoint 使用 idempotency identity；
- concurrency conflict 与 validation failure 使用不同错误；
- unknown field 对 Carry 自有 command 默认 fail closed；
- provider-owned envelope 的兼容策略由具体 Connector 决定；
- 协议版本与 release 字符串分开；
- Web 和 CLI 使用生成 client 或共享明确 wire schema，不复制权限规则。

### Carry CLI

`carry` 使用 Cobra 表达已经存在的嵌套命令树；`carry-server` 的浅层操作面继续使用标准库 `flag`。

- Cobra 只能出现在 `internal/cli/` 的具体 CLI adapter；domain owner 不导入 CLI framework；
- root command 每次构造，不使用 package global、`init()` 注册或动态 command registry；
- 顶层命令组获得各自的窄依赖，成员 token 和 Machine mTLS client 不放入同一个万能 Factory；
- Host 命令不把本地 Runtime 探测提升成服务端 report/status 产品面；
- 叶子命令只负责参数转换、调用具体行为和终端展示；权限与持久事实仍由 owner 裁决；
- `context`、stdin、stdout 和 stderr 从进程入口显式注入；`os.Exit` 只出现在 `main`；
- 注释解释 principal separation、Unknown 和本地密钥等非显然不变量，不逐行复述 flag binding。

## 17. 注释和文档

注释解释代码无法表达的约束：

- 为什么网络调用必须在事务后；
- 为什么某个 Unknown 不能重试；
- 为什么使用数据库时间；
- 为什么一个看似简单的 fallback 不安全。

删除以下注释：

```go
// Set the status.
status = Active
```

Exported comment 描述 caller 需要知道的合同，不复述函数名。

TODO 必须有 owner 或 issue，以及明确退出条件：

```go
// TODO(#123): remove v1 decoding after all stored envelopes are migrated.
```

没有退出条件的 TODO 不进入主分支。

## 18. 测试也是产品代码

测试名称描述行为：

```text
TestExpiredAttemptRejectsLateCommit
TestConcurrentWorkInputsReceiveUniqueSequence
```

避免：

```text
TestStage3
TestHandler2
TestHappyPath
```

测试规则：

- 证明公开行为和持久不变量；
- 不断言私有 helper 调用顺序；
- 并发规则用真实 race 证明唯一 winner；
- response-loss 测试必须保留 Unknown；
- UI 测试通过 role、label、文本和用户操作观察；
- snapshot 不是行为测试默认方案；
- test helper 保持在最近的测试 package；
- 不为测试导出生产 API；
- mock 只替代真实外部边界，不替代被测 domain。

测试数据应说明业务意义，不使用 `foo`、`bar`、`test1` 掩盖场景。

## 19. Generated code

Generated code 必须可识别、可重建、不可手改。

修改顺序：

```text
修改 source schema / SQL / generator config
→ make generate
→ 检查生成 diff
→ 运行对应测试
```

生成目录不承载手写业务逻辑。生成 API 很难使用时，修正 source contract 或在明确消费边界建立小 adapter，不修改生成结果。

## 20. Node 关闭的三门独立 Review

每个 Node 关闭前由三名 fresh-context reviewer 接收同一 frozen contract，分别回答一门。三门都以“责任确定，路径自由”和四条执行原则为共同标准；工具通过不能代替任何一门。

### 逻辑与直接证据

- 冻结的用户 journey 是否真的执行并产生正确结果？
- 成功、失败、并发、幂等和恢复是否有可执行证据？
- 是否会丢失事实、重复外部后果、错误重试或猜测 Unknown？
- 绿色命令是否覆盖 changed path，而不是只证明相邻代码可编译？

### 架构、产品哲学与 AI-native

- responsibility 是否由唯一 owner 持有，dependency 是否指向 authority owner？
- identity、authority、causality、time、privacy 和 outcome 是否保持窄而可机械证明？
- 是否让内容、模型输出、tool annotation 或 provider state 产生权限？
- 是否增加了当前 journey 没有赚得的 owner、状态、协议、registry、兼容层或第二份事实？
- 在可信边界内，是否仍保留自然语言、concrete adapter 和合法执行路径的自由？
- 删除一个抽象是否让责任更确定、合法路径更多且产品承诺仍完整？

### 实现美学与纵向完整

- package、文件、类型、函数和变量名字是否准确表达 owner 与 failure？
- 主路径是否线性，authority 与 consequence 是否在调用签名中可见？
- 是否存在转发层、重复模型、无意义 wrapper、未来 flag 或机械拆文件？
- 注释是否解释真实约束而不是复述代码？
- 是否删除 replaced code，同时保留当前 journey 必需的 migration、protocol、generated artifact、test 与文档？
- 是否把少行数误当简单，因而削弱事务、失败处理或直接证据？

三门只报告本 gate 的 blocker 与高价值删除项；不以未来便利扩大 frozen scope。父进程必须 disposition finding，blocker 修复后由原 reviewer 窄确认。

## 21. 合并前必须删除的代码噪声

仓库级清理规则由 `docs/repository.md` 定义。代码 review 重点删除：

- 无调用者的 exported code；
- 只转发参数的 wrapper；
- 没有真实替换需求的接口；
- 未使用字段和未来 flags；
- 同一事实的第二份类型；
- 无安全证明的 retry 或 silent fallback；
- 测试专用生产 API；
- 没有退出条件的 TODO；
- 被注释掉的旧实现。

Git 历史已经保存删除内容。不要把失效代码留在主分支当档案。

## 22. Definition of Done

一个改动完成时，应能简短回答：

1. 改了哪个 owner？
2. 新增或改变了什么真实行为？
3. 哪个事务或类型保护它？
4. 失败时留下什么可观察事实？
5. 哪些测试证明它？
6. 是否删除了被替代的代码？
7. 是否还存在可以无损删除的 wrapper、字段、接口或文件？

如果第 7 个答案是“有”，改动还没有完成。

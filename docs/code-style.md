# Carry 代码美学

代码要像一份关于责任的准确记述：谁校验、谁决定、谁持久化、谁执行外部动作、答案未知时会发生什么。

本文件的每条规则在 review 时都有可观察后果。生成代码和 vendored 代码全部排除。

## 1. 阻断项

只要下列任何一条成立，diff 不合并，除非 `carry.supervisor` 记录了例外更清楚也更安全的具体理由。

| # | 阻断 | 修法 |
| --- | --- | --- |
| B1 | 一个 `if` 合并了三个以上互相独立的事实 | 一个分支一条规则，一个含义一个错误 |
| B2 | 一个长条件被藏在 helper、方法或 `isValid` 里 | 展开并拆开，或让该 helper 成为一条有自己错误的 owner 规则 |
| B3 | 一个 command / struct 字面量携带接收方拥有或可推导的事实 | 删掉这些字段 |
| B4 | struct 字面量一行写多个字段 | 一行一个字段 |
| B5 | 一个函数同时做线格式解析、产品策略、数据库事务和网络 I/O | 按责任拆 |
| B6 | 事务是一堵匿名的墙，而不是有名字的线性阶段 | 给阶段命名 |
| B7 | 同一个值在没有新边界事实的情况下被再校验一次，或把事务前预检查当作最终授权 | 只在拥有它的边界校验；写事务是新边界，锁读后必须重验可并发变化的权限 |
| B8 | 一个 helper / type / interface / package 只做转发、改名或减少行数 | 删掉 |
| B9 | 测试断言的是仪式、helper 调用顺序或同一事实的另一种表示 | 断言用户可见行为或不变量 |
| B10 | package 不对应事实 owner、具体进程 adapter 或明确命名的 composition/transport 边界；边界 package 拥有了产品策略；文件名不能说明内聚行为 | 放回准确 owner/边界，或删掉这个桶 |
| B11 | 节点关闭时被替代的路径还活着 | 在同一个节点删除 |

拆分依据永远是责任和事务阶段，不是行数。一个同时解析请求、判断策略、开事务并调用 provider 的短函数，比一个阶段清晰的长线性事务更糟。不要用行数阈值制造架构。

## 2. 条件

一个 `if` 表达一个停下来的理由。

```go
// 错：九个独立事实、一个没有含义的错误、没有恢复路径。
if code == "" || code != pending.Code || pending.Used ||
    pending.ExpiresAt.Before(now) || pending.SpaceID != spaceID ||
    member.ID == "" || !member.Active || !member.CanConnectHost ||
    fingerprint != pending.Fingerprint {
    return ErrInvalid
}
```

```go
// 同样错：九个事实换了个名字。读者仍然不知道哪一条失败，调用方仍然无法恢复。
if !pending.isValid(code, spaceID, member, fingerprint, now) {
    return ErrInvalid
}
```

```go
// 对：每条规则有名字、有结果、有恢复。
if pending.Code != code {
    return ErrCodeMismatch
}
if pending.Used {
    return ErrCodeAlreadyUsed
}
if !pending.ExpiresAt.After(now) {
    return ErrCodeExpired
}
if pending.SpaceID != spaceID {
    return ErrWrongSpace
}
if !member.CanConnectHost {
    return ErrMemberNotPermitted
}
```

- 当多个事实构成一个不可分割的谓词（一个区间、一次准确的重放相等判断）时，`&&` 可以用；
- 只有当一个谓词是一条拥有自己错误的 owner 规则时才抽出来；`isValid`、`ok`、`check`、`verifyAll` 是气味；
- 不同失败含义得到不同分支、不同错误、不同用户恢复。

## 3. 命令与字面量

一个 command 只携带调用方合法拥有的事实。它不携带接收方能推导的 ID、attempt、fence、数据库时间、digest、默认值或重放标记。

```go
// 错：调用方复述了 Host 和 PostgreSQL 已经知道的一切，
// 于是两个 owner 可以对同一次执行产生分歧。
command := ReportProgressCommand{RunID: runID, Attempt: attempt, Fence: fence,
    MachineID: machineID, AgentID: agentID, WorkID: workID, SpaceID: spaceID,
    Now: time.Now(), Version: version, Digest: digest, Source: "cli"}
```

```go
// 更好：只保留调用方真正拥有的意图。
command := ReportProgressCommand{
    Progress: progress,
}
```

幂等键不自动属于调用方。要把它放进 command，必须先回答：接收方（Host 或 PostgreSQL）能不能从它已经拥有的事实推导出同一个键？能推导就删掉这个字段（B3）。只有当"这两次调用是同一次意图"只有调用方知道时，幂等键才是调用方拥有的事实，并且要在 diff 里写明这个理由。

构造函数由校验或不变量赚得，不由"把十个参数抄进十个字段"赚得。不保留字段相同的 request / command / params / input / row 结构体家族。

## 4. 函数与事务

```go
// 错的形状：一个 handler 同时拥有四个抽象层。
func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
    // 解码 body、归一化字段
    // 检查成员关系、批准与范围
    // 开事务、锁权威行、插入提交、commit
    // 调外部 provider、重试、分类错误、写响应
}
```

```go
// 对的形状：每个名字说明它拥有什么。
func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
    request, err := decodeSubmit(r)                  // 线格式
    ...
    submission, err := s.work.Authorize(ctx, request) // 策略 + 事务
    ...
    outcome := s.provider.Send(ctx, submission)       // 网络 I/O，可能 Unknown
    ...
}
```

上面只示范责任分层，不规定哪个 owner、哪个 provider 或哪种事务拆分；具体归属由 `docs/architecture.md` 和节点设计冻结决定。

事务在适用时按这个顺序读（不适用时不强行凑齐阶段）：

```text
begin → 锁/读权威事实 → 拒绝终态或重放输入
      → 重新校验当前权限 → 写唯一的状态转移
      → 记录有后果的提交 → commit
```

事务前预检查只改善错误体验，不授予最终权限。进入写事务是新的边界事实：可能并发变化的 Membership、目标 Agent、lease、fence、批准与版本必须在锁读后、写转移前重新校验。禁止跨事务 check-then-act。网络调用不放在数据库事务中；先持久化准确的提交事实，再在事务外调用并记录 Succeeded、Failed 或 Unknown。

使用 owner 本地的阶段 helper，例如 `loadPendingConnection`、`consumeInvitation`、`rejectStaleFence`。不建事务框架。权威时间全部来自 PostgreSQL；除非纯领域计算需要时钟，不要把 `now` 穿过命令层。

不同 actor 的同名动作不合并成一个带分支的函数：成员在 Web 创建 Work 与本机 Agent 凭常驻权限创建 Work，权限来源不同，各自拥有自己的 handler 与事务。

## 5. 仪式性抽象

```go
// 错：转发、单实现 interface、桶名字。
type WorkService interface{ Update(context.Context, UpdateInput) error }
func (m *WorkManager) Update(ctx context.Context, in UpdateInput) error {
    return m.repo.Update(ctx, in)
}
```

删掉，直接调用 owner。Interface 放在消费方，只包含它真正使用的最小方法集，不为"将来可能有第二个实现"创建。流程中的一个角色仍然是角色；一次临时计算仍然是局部值；一个数据库投影不自动成为 owner。

依赖要赚得：新依赖必须移除比它引入更多的项目自有复杂度，并且当前就有消费者。

## 6. Package 与文件

package 只为三类责任之一存在：一个事实 owner（`identity`、`space`、`agent`、`conversation`、`work`、`machine`、`run`）；一个具体进程 adapter（`host/pi`、`host/codex`）；或一个明确命名的 composition/transport 边界（`cmd`、`server`、`postgres`、`cli`、`host`、`e2e`）。第三类只组合、翻译或适配，不拥有产品策略。文件按这个 package 中一个内聚行为命名。

```text
错  internal/work/service.go        一个 owner 的全部行为堆在一个桶里
错  internal/agent/manager.go       名字不说明它拥有哪条行为
错  internal/common/validation.go   跨 owner 的垃圾桶
对  internal/agent/registration.go  Agent 身份注册这一条行为
对  internal/postgres/agent.go      镜像该 owner 行为的持久化
对  internal/server/agent_api.go    这条行为的入站 transport
对  apps/web/app/features/agents/   按产品概念组织的 Web feature
```

在没有当前 owner 行为的情况下不创建 `service.go`、`manager.go`、`utils.go`、`common.go`、`helpers.go`。派生视图（在线、最近活跃、Inbox）是查询，不是新文件家族的理由。

## 7. 错误

- 返回错误，应用路径不 panic；
- 用操作名包装：`fmt.Errorf("claim run: %w", err)`，保持 `errors.Is` / `errors.As` 可用；
- 只有当调用方要据此做真实决定时才引入 sentinel 或类型化错误；
- 恢复方式不同时，永不把过期、重放、未授权、迟到、目标不可用和缺失合并成一个错误；
- 错误里永不出现 token、代码、私人内容或 provider 原始响应体。

## 8. Go

通过仓库命令跑 `gofmt`。package 名小写并准确描述 owner、adapter 或边界。导出标识符写简短的用途注释。默认值类型，除非需要变更、身份或复制成本要求指针。接受 interface，返回具体类型。禁止全局变量、`init()` 注册和动态 command registry。Cobra 只在 `internal/cli/` 显式构造；`carry-server` 的操作面仍浅，继续用标准库 `flag`。HTTP 只用 chi。测试用 `t.Cleanup`、`t.Setenv`、`t.TempDir`。

启动 goroutine、子进程、listener 或长轮询的代码同时拥有取消、等待与关闭责任；构造函数不启动无人管理的后台任务。创建 socket、临时目录或其他资源的一方负责关闭和清理。teardown 必须等待相关工作静止，不能留下继续写数据库或文件的后台执行。

## 9. SQL

- 已提交且可能执行过的 migration 永不修改、删除或重排；live schema 变化使用新的前向 migration 并覆盖从旧版本升级；
- 应用查询放在 `internal/postgres/queries/` 并经 sqlc；`internal/postgres/dbsqlc/` 永不手改；
- 查询名说明真实的状态转移，不叫 `UpdateState`；
- 幂等、唯一 winner、slug 与 Agent 名字的唯一性由唯一索引或部分索引强制；迟到的写者必须在 SQL 里失败，不能只在 Go 里失败；
- 数据库时间拥有 lease、过期、顺序、重试和定时；
- 有后果的外部提交在权限被消费之前或与之原子地记录；
- 宁可一个阶段清楚的事务，也不要几次调用制造部分真相；宁可几条可读语句，也不要一个谓词藏住无关策略。

## 10. HTTP 与协议

Handler 只做认证、解码、线格式校验、调用一个 owner 操作、编码。它不含事务，也不编排 provider。公开协议只包含当前消费者；Host API 不为对称而发布。当 omitted、`null`、空值和零值含义不同时要区分。边界上时间戳用 RFC 3339 UTC。有后果的命令按 §3 的推导规则决定幂等键归属。生成客户端只重新生成，不打补丁。

## 11. 面向 Agent 的接口与 Host

它是 Agent 的本机接口，不是人用产品，也不是第二套 User API。以下是交互原则；准确的动词、字段、传输、退出码集合与幂等键归属由节点 18 的研究冻结（节点 16 提供 Pi/Codex 第一方证据），不在本文件预定。

- 不按内容分裂命令：计划、进度、需要人的事项、请求协作者是同一份结构化输入里的字段，不各自成为一条命令；
- 输入输出是机器可读的结构；stdout 只承载协议，人读文本与诊断走 stderr；输出确定、有界、可被另一个 Agent 解析；
- 可发现性稳定：调用方能用固定方式取得当前版本的输入/输出结构，不靠运行时生成的动作清单；
- 错误类别稳定且互斥：本地失败、Host 拒绝、Server 拒绝、Unknown 永不合并；
- 调用方提供的内容永不携带 Host 或 Server 已拥有的身份与权限事实；
- 没有自然语言动作入口，没有按名字派发的通用动作表面，没有插件注册表；
- "当前 Work 上下文中"与"无 Work 的本机创建"是两条不同的权限校验路径，不共用一次校验。

## 12. TypeScript 与 React

严格 TypeScript；不用 `any`、宽泛断言或重复生成的协议类型。一个组件渲染一个产品概念；一个 hook 拥有一个远端行为；route 组合 feature，不变成 controller。UI 状态用显式 union，不用互相作用的布尔；loading、empty、unavailable、error 由同一个权威远端状态派生。不在乐观状态里镜像 Server 权威。可访问名称说明动作及其后果；危险操作说明范围和未来影响。大组件按可见的产品责任拆，不拆成 `Header`/`Body`/`Footer` 的属性中转。

## 13. 测试

一个测试让一条产品承诺或不变量无法抵赖，顺序是：owner 行为；针对真实隔离数据库的 PostgreSQL 事务与并发；HTTP 或本机 Host 接口合同；浏览器或跨进程旅程；有凭据和隔离目标时的真实外部 canary。

```ts
// 错：断言仪式。换一个实现就红，产品承诺却没有被保护。
expect(createWork).toHaveBeenCalledWith(expect.objectContaining({ agentId }));
```

```ts
// 对：断言用户可见事实。
render(<WorkPage workId={work.id} />);
expect(await screen.findByText("Owner: Mia")).toBeVisible();
expect(await screen.findByText("Agent owner: pi-1")).toBeVisible();
```

```go
// 错：状态码不是证据。
require.Equal(t, http.StatusOK, response.StatusCode)
```

```go
// 对：并发下 PostgreSQL 只允许一个 winner，迟到者带着可恢复的错误失败。
first, second := claimConcurrently(t, ctx, store, run.ID)
require.NoError(t, first)
require.ErrorIs(t, second, run.ErrAlreadyClaimed)
```

每个测试说明执行了什么、哪个用户可见事实证明了它。适用时必须覆盖的失败：无效受众或成员关系；畸形、过期与已用邀请；准确重放与冲突重放；并发 winner；lease 过期与迟到 fence；目标 Agent 不可用；Host 或 Agent 丢失；撤销与 owner 转移竞态；移除者试图改变别人的 Work owner；旧 Agent 恢复后的迟到写入；替代 Agent 在转移提交后立即掉线；响应丢失与 Unknown 在交接后仍不被猜测或重放；投递失败但 Work 与 Inbox 不被污染；时区与周期边界；被删除的路由或命令确实不存在。

## 14. 名字与最后一次通读

注释解释意外的约束，不解释语法。名字使用产品和 owner 语言；有具体名词时避免 `Manager`、`Processor`、`Coordinator`、`Resource`、`Payload`、`Data`、`Info`、`Utils`、`Common`。

宣布 diff 就绪前回答：读者能否说出每个被改事实的 owner；happy path 是否线性；每个失败分支是否只有一个理由和一个恢复；字面量是否只携带调用方拥有的事实；事务阶段是否可见；新 package 是否对应事实 owner、具体 adapter，或一个不拥有产品策略的明确 composition/transport 边界；有没有重复或转发；这个节点是否删除了它替代的东西；是否有直接证据显示这条旅程真的跑过。

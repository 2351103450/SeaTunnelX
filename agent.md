# SeaTunnelX Agent 工作指南

本文档用于初始化本仓库的 AI/代码 Agent 协作规范，作为默认执行基线。

## 适用范围

- 默认作用域为整个仓库：`D:\ideaProject\SeaTunnelX`。
- `agent/` 目录是独立 Go 模块，负责运行时 Agent 能力。
- 除非 `.proto` 发生变更并需要重新生成，否则不要手改 protobuf 生成文件。

## 仓库结构速览

- `main.go`：后端启动入口。
- `frontend/`：Next.js 前端工程。
- `agent/`：SeaTunnelX Runtime Agent（独立 Go 模块）。
- `internal/`：后端核心业务逻辑。
- `scripts/`：`proto`、`tidy`、`swagger`、`license` 等脚本。
- `docs/`：项目文档。

## 常用命令

### 后端（根模块）

```bash
go mod tidy
go run main.go api
go test ./...
```

### 前端

```bash
cd frontend
pnpm install
pnpm dev
pnpm test
```

### Agent 模块

```bash
cd agent
go mod tidy
go run ./cmd
go test ./...
```

### Make 目标（根目录）

```bash
make tidy
make swagger
make proto
make check_license
make pre_commit
```

## 开发约定

- 变更保持最小化、聚焦当前任务。
- 延续现有架构和命名风格，避免无关重构。
- 行为变更必须同步补充或更新测试。
- 提交前至少运行与改动相关的测试与检查。
- 不提交本地二进制、临时文件和机器相关产物。

## Protobuf 变更说明

当修改 `.proto` 后，需要重新生成并确认以下文件更新：

- `internal/proto/agent/agent.pb.go`
- `internal/proto/agent/agent_grpc.pb.go`

并执行：

```bash
go test ./internal/proto/agent/...
```

## UI/前端 待办与约定

- **页面标题左侧图标**：各主页面标题左侧应带 logo/图标，与现有风格一致。参考：控制台（Dashboard）使用 `Ship`、告警中心使用 `Activity`（lucide-react）；布局为 `flex items-center gap-3`，图标 `h-8 w-8 shrink-0 text-primary`，右侧为标题与副标题。新增或改版主页面时请保持该风格。
- **过滤栏左对齐**：整个项目内，列表/检索页的过滤栏（搜索框、下拉筛选、搜索/清除按钮等）统一采用左对齐布局：不做 `w-full` 撑满或中间弹性占位把操作推到右侧；搜索框使用 `flex-1 min-w-[200px] max-w-sm` 等合理宽度，与筛选控件从左往右自然排列。参考：集群管理 `ClusterMain` 条件栏。

## 提交前检查清单

- 代码可编译通过。
- 相关测试已通过。
- 如有行为变化，配置与文档已同步更新。
- 没有引入无关文件改动。

# 错误处理

> 本项目中的错误处理方式。

---

## 概述

错误通过**每包一个 err.go 的哨兵错误**（`errors.New`）定义，从 repository → service → handler **逐层返回**，在 handler 中通过 `getStatusCodeForError(err)` 映射为 **HTTP 状态码**。API 错误响应使用统一 JSON 结构：`{ "error_msg": "<消息>", "data": null }`（或省略 `data`）。成功响应为 `{ "data": <载荷> }`，`error_msg` 为空或省略。

---

## 错误类型

- **哨兵错误**：各应用在 `err.go` 中用 `errors.New("包名: 描述")` 定义。例如：
  - `cluster`：`ErrClusterNotFound`、`ErrClusterNameDuplicate`、`ErrClusterNameEmpty`、`ErrNodeNotFound`、`ErrNodeAlreadyExists`、`ErrNodeAgentNotInstalled` 等
  - `auth`：`ErrUserNotFound`、`ErrInvalidPassword`、`ErrUserInactive`、`ErrUserAlreadyExists` 等
  - `monitor`：`ErrConfigNotFound`、`ErrInvalidMonitorInterval`、`ErrEventNotFound` 等
- **错误码**：可选数字码（如 `ErrCodeClusterNotFound = 3001`）在同一 `err.go` 中定义，用于文档或后续客户端；对外契约以 HTTP 状态码为主。
- **包装**：需要补充上下文时使用 `fmt.Errorf("...: %w", err)`；调用方用 `errors.Is(err, ErrClusterNotFound)`，包装后的哨兵仍可匹配。

---

## 错误处理模式

- **Repository**：当 `errors.Is(err, gorm.ErrRecordNotFound)` 时返回领域哨兵错误（如 `ErrClusterNotFound`），否则返回底层错误。不要在 repository 内为「未找到」打日志，由 handler 或 service 决定。
- **Service**：原样返回或带上下文包装错误；不要吞掉错误。校验失败时返回哨兵或明确错误，便于 handler 映射为 400。
- **Handler**：调用 service 后若 `err != nil`，用 `getStatusCodeForError(err)` 得到状态码，并响应 `c.JSON(statusCode, XxxResponse{ErrorMsg: err.Error()})`。绑定/校验失败用 `http.StatusBadRequest` 并给出清晰消息（如「无效的集群 ID」）。

示例（来自 cluster handler）：

```go
cluster, err := h.service.CreateCluster(ctx, ...)
if err != nil {
	if errors.Is(err, ErrClusterNameEmpty) || ... {
		c.JSON(http.StatusBadRequest, CreateClusterResponse{ErrorMsg: err.Error()})
		return
	}
	statusCode := h.getStatusCodeForError(err)
	c.JSON(statusCode, CreateClusterResponse{ErrorMsg: err.Error()})
	return
}
c.JSON(http.StatusOK, CreateClusterResponse{Data: cluster.ToClusterInfo()})
```

---

## API 错误响应

- **结构**：响应结构体包含 `ErrorMsg string` 与可选 `Data`。错误时设置 `ErrorMsg` 为 `err.Error()`，`Data` 为 nil 或省略；成功时设置 `Data`，`ErrorMsg` 为空。
- **HTTP 状态码**：
  - `400 Bad Request`：非法入参、缺少必填项或业务校验失败（如名称为空、角色非法）。
  - `404 Not Found`：资源不存在（如集群、节点、配置）。
  - `409 Conflict`：重复或冲突状态（如集群名已存在、节点已存在）。
  - `500 Internal Server Error`：未预期错误（如 DB 失败）；若敏感则避免在 `error_msg` 中暴露内部细节。
- **映射**：每个 handler 实现（或共用）`getStatusCodeForError(err)`，根据 `errors.Is(err, ErrXxx)` 返回对应状态码；默认 500。

---

## 常见错误

- 直接向客户端返回 `gorm.ErrRecordNotFound`；应先在 repository 映射为领域哨兵，再在 handler 映射为 404。
- 新增哨兵后未在 `getStatusCodeForError` 中处理，导致本应 400/404/409 的请求返回 500。
- 在 service 和 handler 两处都打同一错误的日志；建议在边界打一次（如 HTTP 在 handler，后台任务在 service）。
- 在生产环境的 `error_msg` 中暴露堆栈或内部路径。

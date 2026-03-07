# 质量规范

> 后端开发的代码质量标准。

---

## 概述

项目使用 **Go**，采用 handler、service、repository 分层。代码按功能组织在 `internal/apps/<name>/` 下，命名与错误处理统一。测试使用 Go 自带 `testing` 包，部分模块在测试中使用真实 DB（如 SQLite）。代码风格与格式化遵循常见 Go 实践，与 `golangci-lint` 等工具一致。

---

## 注释与文件头

### 双语注释

- **注释须中英双语。** 添加注释时需同时提供中文与英文，便于团队协作。可用单行中英并列，如 `// 解析请求体并校验 Parse and validate request body`，需要时再用简短块注释。

### 注释密度

- **注释不宜过密。** 仅在有必要处添加注释：非常规逻辑、业务规则、复杂算法或对外 API 约定。避免逐行注释或重复代码已表达的含义。

### 新建文件须带 License 头

- **每个新建的后端源文件必须在文件顶部添加 Apache 2.0 License 头。** 紧接在 `package` 声明之前，`*/` 与 `package` 之间不要多空行。完整内容如下：

```
/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
```

---

## 禁止模式

- **全局可变状态**：不得用全局变量持有 service 或 DB，应通过构造函数注入（如 `NewHandler(service, auditRepo)`）。仅允许的全局实例为 `internal/db` 的 DB 与 `internal/logger` 的 logger。
- **省略 context**：不得新增省略首参 `context.Context` 的 repository 或 service 方法，否则会破坏链路与取消传播。
- **在 API 中直接暴露 gorm.ErrRecordNotFound**：不得向 handler 返回 `gorm.ErrRecordNotFound`，应在 repository 中映射为领域错误（如 `ErrClusterNotFound`），再在 handler 中映射为 HTTP 状态码。
- **记录敏感信息**：不得在日志中输出密码、令牌、API 密钥或任何用于认证的配置，也不得记录可能包含凭据的请求/响应体。
- **忽略错误**：不得对影响正确性的错误使用 `_ = ...`，要么处理错误，要么在注释中说明为何可以忽略（如尽力而为的清理）。

---

## 必须遵循的模式

- **Context 传递**：从 HTTP handler 经 service 到 repository 一路传递 `ctx`；每次 DB 调用使用 `r.db.WithContext(ctx)`（或事务）。
- **领域失败用哨兵错误**：在 `err.go` 中定义包级哨兵错误；在 handler 中用 `getStatusCodeForError(err)`（或等价函数）返回一致的 HTTP 状态码。
- **响应结构**：统一使用 `ErrorMsg` 表示错误、`Data` 表示成功载荷，并显式设置 HTTP 状态码（400、404、409、500）。
- **新功能代码**：新功能放在 `internal/apps/<name>/` 下，按需包含 handler、service、repository、model、err；在 `internal/router/router.go` 中注册路由与依赖。
- **迁移**：新增持久化实体时，将对应 model 加入 `internal/db/migrator/migrator.go` 的 `AutoMigrate` 列表。

---

## 测试要求

- **单元测试**：使用同包下的 `*_test.go`。多场景优先用表驱动测试。在可行时 mock 外部依赖（如 DB）；部分测试使用真实 SQLite（如 `cluster/repository_test.go`、`audit/repository_test.go`）。
- **覆盖重点**：以 repository 和 service 逻辑为主；handler 常通过间接方式覆盖。关键路径（如重名、未找到）应有测试。
- **命名**：测试函数名为 `TestXxx`；子测试用 `t.Run("描述", ...)`。示例：`TestRepository_Create_clusterNameDuplicate_returnsErrClusterNameDuplicate`。
- **DB 测试**：使用真实 DB 时，避免依赖共享全局状态；尽量每个测试使用独立 DB 或事务（参见现有 `repository_test.go` 写法）。

---

## 代码评审检查项

- 新接口使用统一的请求/响应与错误模式（`ErrorMsg`、`Data`、`getStatusCodeForError`）。
- 所有新 DB 访问均使用 `WithContext(ctx)`。
- 新领域错误已加入 `err.go` 并在 handler 的状态码辅助函数中完成映射。
- 日志中无敏感信息与 PII。
- 新表/新 model 已加入 migrator 的 `AutoMigrate` 列表。
- 在可行处为新 repository 与 service 行为补充测试。
- 不为 service 或 repository 使用全局变量；依赖在 `router.go` 或 `cmd` 中组装。

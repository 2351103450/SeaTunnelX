# 前端开发规范

> 本项目的前端开发最佳实践。

---

## 概述

本目录包含前端开发相关规范。控制台为 **Next.js** 应用（App Router），使用 **React**、**TypeScript**、**Tailwind CSS** 及 **shadcn/ui** 风格组件。API 访问通过 `lib/services/` 下的 **services** 统一完成，使用共享 **axios** 客户端与 **BaseService**；后端响应格式为 `{ error_msg, data }`。

---

## 规范索引

| 文档 | 说明 | 状态 |
|------|------|------|
| [目录结构](./directory-structure.md) | App Router、组件、lib、hooks | 已填写 |
| [API 与 Services](./api-and-services.md) | API 客户端、服务层、错误处理 | 已填写 |
| [UI 约定](./ui-conventions.md) | 布局、筛选栏、页面标题、i18n | 已填写 |

---

## 使用方式

- 遵循代码库中的**实际约定**（参见 `frontend/` 与 `agent.md`）。
- 新页面放在 `app/(main)/...` 下；新领域 UI 放在 `components/common/<domain>/`。
- 仅通过 `lib/services` 调用后端；使用统一的响应类型与错误处理。

---

**语言**：本目录下所有文档均使用**中文**。

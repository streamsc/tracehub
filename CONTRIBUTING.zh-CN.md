# 为 TraceHub 贡献代码

[English](CONTRIBUTING.md) | [简体中文](CONTRIBUTING.zh-CN.md)

## 事实来源

- 版本范围和验收条件保存在 `docs/requirements/`。
- GitHub Issues 跟踪交付工作和缺陷。
- Git 标签标识已发布的产品版本。
- `CHANGELOG.md` 是唯一的变更日志。

Issue 不能增加或改变版本范围。实现新增或改变的用户可观察行为前，必须先
更新并接受相应需求。

## 分支与 Pull Request

`main` 是唯一长期分支，并且必须保持可发布。代码、配置、测试和发布变更
使用短期 `agent/<description>` 分支，并且必须通过 Pull Request 合并。
维护者可以将仅涉及文档的修正直接推送到 `main`。

每个 Pull Request 必须：

- 在适用时关联 Issue 和需求编号；
- 说明用户可观察行为和验证结果；
- 在适用时同步更新测试、文档、翻译和 `CHANGELOG.md`；
- 删除被本次变更替代的代码、配置、测试和文档；
- 不包含无关变更或秘密信息。

Pull Request 使用 squash merge，合并后删除分支。单人维护阶段不要求其他人
批准；出现固定协作者后，应将规则调整为至少一次批准。

## Issue 与需求

尚未进入版本范围的想法使用 Proposal Issue。已接受需求中可独立交付的工作
使用 Implementation Issue，并且必须分配给目标版本 Milestone。一项需求可以
对应多个 Implementation Issue。不符合已接受需求或已发布行为的问题使用 Bug Issue。

需求编号采用全局 `REQ-001` 格式且永不复用。需求状态只能是 `Draft`、
`Accepted`、`Released` 或 `Removed`。

## 提交

使用简短的 Conventional Commit 标题：

```text
feat: add Codex session import
fix: reject a truncated session
docs: define v0.1.0 requirements
test: cover duplicate uploads
refactor: simplify session parsing
chore: prepare v0.1.0-alpha.1
```

每个提交只处理一项明确变更。提交标题不能替代 Issue、需求、测试结果或变更日志。

## 完成标准

只有在验收条件和相关测试通过、用户可观察行为已记录、失败边界明确、中英文
内容已同步，并且被替代路径已经删除时，一项变更才算完成。

版本和发布规则参见[发布管理](docs/releasing.zh-CN.md)。

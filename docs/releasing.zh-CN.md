# 发布管理

[English](releasing.md) | [简体中文](releasing.zh-CN.md)

## 版本规则

TraceHub 整个仓库使用一个[语义化版本](https://semver.org/lang/zh-CN/)。组件和
Agent 适配器不使用独立产品版本。已发布的 Git 标签是发布版本的事实来源。

- Alpha 标签使用 `v0.1.0-alpha.1`，每次发布递增最后的数字。
- 稳定版标签使用 `v0.1.0`。
- 第一阶段不使用 beta 或 release candidate 渠道。
- `v1.0.0` 之前，次版本可以包含不兼容变更。
- 补丁版本只修复已有行为，不得故意破坏同一次版本。新增产品能力必须进入新的次版本。

Alpha 发布期间，需求文件保持 Draft 或 Accepted。稳定版发布要求包含的需求在
发布前全部为 Accepted，并由发布 Pull Request 改为 Released。

## 变更日志

`CHANGELOG.md` 是唯一变更日志，并使用英文维护。改变用户可观察行为的 Pull
Request 必须更新 Unreleased 中相应分类。不影响用户的内部变更无需添加记录。

## 发布清单

1. 从当前 `main` 创建发布分支和 Pull Request。
2. 确认目标需求、关联 Issues、测试、文档、翻译和全新安装说明。
3. 将相关 Unreleased 条目移动到准确版本号和发布日期下。
4. 稳定版发布时，将包含的需求标记为 Released，并关闭 Milestone。
5. 将发布 Pull Request squash merge 到 `main`。
6. 在合并后的 `main` 提交上创建并推送 annotated tag。标签必须与
   `tracehub version` 输出的版本完全一致。
7. 等待 Release artifacts 工作流通过。该工作流执行源码检查、构建四个声明平台
   的二进制、验证程序版本、生成 `SHA256SUMS`、上传工作流制品、向 GHCR 发布
   公开的 Linux amd64/arm64 镜像，并使用相同的不可变二进制资产创建 Draft
   GitHub Release。
8. 将该标签触发的工作流所生成的资产作为唯一发布制品。不得手工构建或上传发布
   资产。在发布声明的每个平台上，仅使用 Draft Release 中的资产进行安装并运行
   文档中的核心流程。
9. 完善 Draft Release 正文。Alpha 必须标记为 prerelease，列出已完成和未完成
   需求，确认安装验证结果，然后发布 Release。

工作流也可以针对分支手动运行，作为发布前检查。手动运行只构建并上传工作流制品，
不发布容器镜像，也不创建 GitHub Release。其制品不是发布资产，不得复制到
Release。标签工作流运行前不得手工创建对应 Release；如果 Release 已存在，唯一
发布路径会明确失败。

## 容器镜像

标签触发的发布会生成 `ghcr.io/streamsc/tracehub:vX.Y.Z[-alpha.N]` 以及不带前导
`v` 的等价标签。两个名称指向同一个不可变多平台镜像。Alpha 发布永不更新
`latest`。

工作流会在创建 Draft Release 前将关联的 `tracehub` 容器包设为公开。发布验证
必须确认匿名拉取可用：

```bash
docker pull ghcr.io/streamsc/tracehub:v0.1.0-alpha.3
```

在可安装产物尚未于多台设备完成 Codex 会话提交、跨设备发现和会话读取流程前，
不得发布稳定版 `v0.1.0`。

## 发布失败与回退

不得移动或删除已发布标签来复用版本，也不得用不同内容替换同名发布产物。
如果发布存在问题，应在 Release 说明中标记影响，在受影响环境恢复上一个已知
可用版本，从新分支修复问题，并发布下一个 Alpha 或补丁版本。替代产物必须
使用新版本号和新校验值。如果标签触发的工作流失败，必须保持该标签不变，修复
原因后准备下一个 Alpha 或补丁版本，不得为同一版本重新运行内容已经改变的构建。

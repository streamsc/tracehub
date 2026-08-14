# TraceHub

[English](README.md) | [简体中文](README.zh-CN.md)

面向 AI 智能体的私有会话中枢。

TraceHub 从多台设备收集智能体会话历史，完整保存原始记录，并通过
MCP 提供经过授权的搜索与读取能力。

第一版支持 Codex 会话。后续可以通过独立适配器支持其他智能体，而无需
改变归档与查询模型。

## 状态

早期开发阶段。

## 规划组件

- `server`：上传、归档、索引与查询服务
- `sync`：本地会话发现与增量上传
- `mcp`：经过授权的会话搜索与读取接口
- `adapters/codex`：Codex JSONL 解析适配器


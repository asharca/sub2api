# 上游来源与合并记录

`release/standalone` 使用独立版本号。下表记录本分支已整合的基线，不代表来源仓库的最新状态。

| 来源 | 已整合基线 | 主要能力 |
| --- | --- | --- |
| Wei-Shaw/sub2api | `v0.2.23` 后的 `main` 提交 [`2af3f1e15`](https://github.com/Wei-Shaw/sub2api/commit/2af3f1e15) | 多账号接入、API Key 分发、计费、并发控制、管理后台、[管理 MCP](mcp.md) 和 [OpenAI 图片接口](OPENAI_IMAGES_CODEX.md)。 |
| ranxi2001/sub2api | `production` 提交 [`1d2ed630e`](https://github.com/ranxi2001/sub2api/commit/1d2ed630e) | [Excel / BPS 路由](excel-bps.md)、[Codex 票据管理](astra-ticket-acquisition.md)、[Mihomo 出口管理](mihomo-static-node-proxies.md)、工单和渠道监控。 |

本版在这些能力之上维护会话日志、归档查询与导出，以及独立的安装和更新渠道。感谢两个来源项目及其贡献者。

## 后续同步

同步时保留 Git 合并历史，不用来源分支覆盖独立发布分支。合并完成后更新对应来源的基线；合并本身不触发发布或生产部署。

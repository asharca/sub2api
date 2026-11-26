# Sub2API · asharca 独立版

Sub2API 是一个自托管 AI API 网关：管理员接入上游账号并分配 API Key，客户端通过网关调用模型，由网关负责鉴权、调度和用量计费。这个版本面向还需要**查询、归档和导出会话**的部署者，维护在 `release/standalone` 分支。

[安装与迁移](docs/新人入门.md) · [接入 Codex](docs/新人入门-使用Sub2API.md) · [版本下载](https://github.com/asharca/sub2api/releases) · [报告问题](https://github.com/asharca/sub2api/issues)

## 功能

- **会话管理**：查询、归档和导出会话记录。记录可能包含用户输入和模型回复，启用前请明确访问权限、留存期限和备份范围。
  管理后台显示快照扫描行数、压缩体积和 S3 已确认的上传量。可在快照或上传阶段取消（需管理员二次验证）；取消成功不会删除源记录。进入收尾或数据库清理阶段后不可取消。上传达到 100% 只表示数据已传输，须等状态变为「已完成」才确认归档和所选的源记录清理均已成功。
- **账号与路由**：按账号和模型配置 [Excel / BPS](docs/excel-bps.md)，管理 [Codex 票据](docs/astra-ticket-acquisition.md)，通过 [Mihomo](docs/mihomo-static-node-proxies.md) 为账号指定出口，并在[复合分组](docs/COMPOSITE_GROUPS.md)中按请求内容配置智能路由规则。

## 安装与升级

本版从 **`v1.0.0`** 起独立编号。可下载版本以本仓库的 [GitHub Releases](https://github.com/asharca/sub2api/releases) 为准。

| 部署方式 | 操作 |
| --- | --- |
| Linux amd64 二进制 | 从目标 Release 下载归档，用同页 `checksums.txt` 校验后安装。安装脚本和替换步骤见[新人入门](docs/新人入门.md)。 |
| Docker Compose | 使用本分支的 [Compose 文件](deploy/docker-compose.yml)。镜像标签不带 `v`：例如 `v1.0.0` 发布后，对应 `ghcr.io/asharca/sub2api:1.0.0`。可通过 `SUB2API_IMAGE_TAG` 指定已发布版本。 |
| 从 `-asharca.N` 旧版迁移 | 旧更新器不识别新标签。先备份 PostgreSQL、配置和数据目录，再手动替换二进制或更新镜像并重建容器。启动后核对版本、账号和历史会话；仅回退二进制不会撤销数据库迁移。 |

## 维护与反馈

维护文档：[发布说明](.github/release-tools/README.md) · [上游来源与合并记录](docs/UPSTREAM.md)。

报告问题请使用 [asharca/sub2api Issues](https://github.com/asharca/sub2api/issues)，注明版本号、部署方式和复现步骤。不要附上原始会话、API Key、OAuth 凭据或票据。提交改动前请阅读[贡献指南](CONTRIBUTING.md)。

## ⚠️ 重要提醒

使用本项目前，请务必仔细阅读以下内容：

- **🚨 服务条款风险**：使用本项目可能违反 Anthropic 等上游服务商的服务条款。请在使用前仔细阅读相关服务商的用户协议，由此产生的一切风险由用户自行承担。
- **⚖️ 合规使用**：请在符合您所在国家或地区法律法规的前提下使用本项目，严禁将其用于任何违法违规用途。
- **📖 免责声明**：本项目仅供技术学习与研究使用，作者不对因使用本项目导致的账户封禁、服务中断、数据丢失或其他任何直接或间接损失承担责任。
- **🚫 无商业授权**：本项目从未授权任何个人或组织基于本项目开展任何形式的商业化运营。任何以本项目名义或基于本项目从事的商业行为均与本项目及其开发者无关，由此产生的一切纠纷、损失和法律责任由行为主体自行承担。

## 许可证

本项目基于 [GNU 宽通用公共许可证 v3.0](LICENSE)（或更高版本）授权。

Copyright (c) 2026 Wesley Liddick

---

<div align="center">

**如果觉得有用，请给个 Star 支持一下！**

</div>

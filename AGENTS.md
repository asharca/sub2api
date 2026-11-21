# 仓库维护约定

本仓库 `asharca/sub2api` 的独立发布分支是 `release/standalone`。**合并上游、发布版本和部署生产是三件独立的事。** 用户只要求同步或准备版本时，不要顺手推送标签、触发正式发布或部署。

## 合并上游：保留共同历史

1. 先读 [上游来源与合并记录](docs/UPSTREAM.md)。用 `git remote -v` 核对仓库身份：`origin` 是 `asharca/sub2api`，`wei-shaw` 是 `Wei-Shaw/sub2api`，`fork` 是 `ranxi2001/sub2api`。只从后两者拉取，不向它们推送。检查 `git status`；工作区有无关改动时使用独立 worktree，不要清理或提交用户的文件。
2. 从最新的 `release/standalone` 创建临时同步分支。一次只合并一个来源：例如 `git fetch --no-tags wei-shaw main`，或 `git fetch --no-tags fork production`。立即用 `git rev-parse FETCH_HEAD` 取得并记下完整 SHA；若要求合并特定版本，则获取该标签指向的提交，不以分支最新提交代替。不要把上游标签同步成自己的发布标签。
3. 对照上次基线审阅这次新增的提交和差异，以审核过的 SHA 为目标执行 `git merge --no-ff <目标SHA>`。不要用 squash、整段 cherry-pick、rebase 或 reset 代替整线同步；它们不能保留相同的合并关系。解决冲突时逐项核对会话存储、数据库迁移、独立 `VERSION`、更新器来源和发布配置，不整树选择 ours/theirs。
4. 运行本次差异涉及的测试，尤其是有冲突的模块。将来源、分支或标签及实际合入 SHA 写回 `docs/UPSTREAM.md`，不在 README 堆同步日志。将同步分支并回 `release/standalone` 时保留 merge commit（不要在 PR 合入时 squash/rebase）。验收：`git merge-base --is-ancestor <目标SHA> release/standalone` 应退出 `0`；否则不要把它记作已合并基线。

## 发布独立版本：先分支，后标签

1. 从 `v1.0.0` 起使用本仓库自己的稳定标签 `vMAJOR.MINOR.PATCH`，不跟随上游编号，不加 `-asharca.N`。将不带 `v` 的版本号写入 `backend/cmd/server/VERSION`，同步检查 `deploy/docker-compose.yml` 的默认镜像标签和面向用户的安装示例。确认预定标签在本地和 `origin` 均未被使用；不要移动、覆盖或重推已有标签。
2. 按[发布工具说明](.github/release-tools/README.md)运行发布矩阵检查和相关回归测试。获准推送后，先推送 `release/standalone` 至 `origin`，再执行 Actions dry run，核对产物、版本和校验和。只有同一提交的完整 CI、安全扫描与发布演练全部成功才可推标签；任何一项失败或未完成都应停止。
3. **仅在获准正式发布后**，确认待发布 SHA 就是通过演练的提交，且已在 `origin/release/standalone` 历史中。从该提交读取版本创建附注标签；以下命令中的 `SHA` 须预先设为该完整提交号：

   ```bash
   git merge-base --is-ancestor "$SHA" origin/release/standalone &&
     tag="v$(git show "${SHA}:backend/cmd/server/VERSION")" &&
     git tag -a "$tag" "$SHA" -m "$tag" &&
     git push origin "refs/tags/$tag"
   ```

4. 推送标签会触发 [release.yml](.github/workflows/release.yml)。待工作流成功后，核对本仓库 GitHub Release 的 Linux amd64 归档、`checksums.txt`、重登录运行时归档，以及 GHCR 对应版本镜像。镜像标签不带 `v`，不更新 `latest`。不要把推送标签当作发布成功，也不要把发布成功当作生产已升级。

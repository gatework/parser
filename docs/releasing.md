# 发布流程

模块通过公开仓库 `gatework/parser` 的 SemVer tag 发布。版本格式为 `vMAJOR.MINOR.PATCH`，
当前要求 Go 1.26+。模块不包含可执行程序，Release 附件为源码包、提交号和校验和。

1. 更新 `VERSION`、`CHANGELOG.md` 和 README 安装版本，通过 PR 合并到 `main`。
2. 等待该提交的 CI 成功。CI 在 Go 1.26.x、1.27.x 的最新补丁版本上运行 `make check`，
   执行 fuzz 与 workflow 检查，然后归档被验证的 Git 提交。
3. 在同一提交创建与 `VERSION` 相符的 tag，并推送。例如首版：

   ```sh
   git tag -a v0.1.0 -m "Release v0.1.0"
   git push origin v0.1.0
   ```

4. `Release` workflow 按提交号查找成功的 main CI，按 artifact ID 下载该次运行的
   源码包。它检查 tag、版本、模块路径、提交号和 SHA-256，创建 draft，上传附件后
   下载并逐字节核对，最后发布。
5. 在仓库外使用 `GOWORK=off go get github.com/gatework/parser@v0.1.0` 验证版本消费，
   不使用本地 `replace`。读回 Release、远端 tag 和工作区状态。

CI artifact 按提交号与运行次数命名，保留 90 天；Release 附件持久保留。
CI 尚未成功或 artifact 已过期时，发布会失败。先运行该提交的 CI，再手动触发
`Release` workflow 并传入已有 tag；不要移动已发布的 tag。
重试时只更新 draft 的预期附件。已经发布的版本仅做完整性验证，不覆盖其附件。

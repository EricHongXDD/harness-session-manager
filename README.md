# Harness Session Manager

一个独立的 Windows 本地桌面工具，用于管理 DeepSeek Harness 对话和空间。只保留四个页面：**对话、已隐藏、备份、空间**。数据不上传，不需要账号或 API Key。

原项目名为 DSH Space，应用显示名称和发行文件已更新为 Harness Session Manager；本地存储目录保持兼容。

[下载最新版](https://github.com/EricHongXDD/harness-session-manager/releases/latest) · [自动构建](https://github.com/EricHongXDD/harness-session-manager/actions/workflows/windows.yml)

## 使用

解压 `HarnessSessionManager-0.3.1-windows-amd64.zip`，双击 `HarnessSessionManager-0.3.1.exe`。需要已安装的 Microsoft WebView2 Runtime；缺少运行时时提示依赖，不联网下载安装器。升级前关闭旧版窗口，应用数据继续使用原来的 `%LOCALAPPDATA%\DSHSpace`。

- 默认来源为当前用户的 `%USERPROFILE%\.dsh`；可在界面更换包含 `sessions` 的目录。
- 对话按 Harness 的工作区名称、登记顺序和完整文件夹路径分组，包含归档和子对话。每组默认显示 6 条，可展开其余对话。未登记的会话也按 cwd 分组保留。搜索工作区、备注、ID 或完整目录不会改变工作区操作的完整范围。
- 对话读取日志中最新的名称／用户备注；没有名称时读取兼容缓存或首条用户文本。点击对话标题后，原生图片附件会在消息中按需加载，支持 PNG、JPEG、WebP、GIF；缺失或损坏时明确提示。
- **备份**：将所选完整会话目录和引用的外置图片保存成独立 ZIP，记录逐文件 SHA256。多个会话共享的图片只保存一次。图片缺失、摘要不符或无法确认会话格式时停止备份，保留原始数据。ZIP 不加密，请妥善保管。
- **导入备份**：在备份页点击“选择 ZIP 导入”，选取已有 Harness Session Manager 备份。整个 ZIP 与图片引用校验通过后才复制到备份库；再次点击“恢复”写入当前来源。重复导入相同 ZIP 不会增加副本。
- **隐藏**：点击后直接将整个父子会话组移动到独立隐藏区，不弹确认、不创建 ZIP。Harness 下次启动时，其会话枚举与查询缓存不再包含已移走的会话；隐藏不是归档。
- **恢复隐藏**：直接将原目录移回原来源，不创建 ZIP；恢复成功后清理空隐藏容器和记录。旧版为同一次隐藏自动生成的 ZIP，在 ID、来源、时间与成员一致时一并清理；独立手动备份保留。
- **恢复备份**：从 ZIP 恢复到当前选定来源。不同内容的同名会话或图片拒绝覆盖；相同内容恢复可以重复执行，手动 ZIP 保留。
- **永久删除**：对话、所选对话、隐藏组和整个工作区均有入口，点击后必须确认实际清单。会包含关联父子对话，删除工作区时也包含其隐藏对话。确认有效期为 5 分钟，数据或范围改变后必须重新确认。只删除会话数据、无其他对话引用的图片，以及工作区登记；项目代码文件夹和手动 ZIP 保留。永久删除不创建 ZIP，无法撤销。
- 备份、隐藏、恢复或删除失败时会弹窗显示错误详情，包括需要退出 Harness 的提示；页面同时保留错误信息。
- **空间**：显式扫描 Harness 来源与 Harness Session Manager 存储，区分会话、缓存、运行时、隐藏和备份。支持取消；逻辑大小与磁盘分配分开显示，硬链接的磁盘占用去重，稀疏文件按实际分配统计。未知不显示为零。
- 备份页可明确删除不再需要的 ZIP。没有自动清理、没有后台删除真实对话。

备份、隐藏、恢复和永久删除前请退出 Harness，包括托盘、网页服务和 CLI。检测到运行或无法确认状态时停止。隐藏是同卷移动，不释放磁盘空间；当前隐藏区必须与来源位于同一卷。恢复发生冲突或操作中断时，保留已完成事实，并在“已隐藏”核对恢复。永久删除中断会报告已完成数量，请刷新核对剩余内容。不要手动修改隐藏区的清单文件。

## 独立存储

应用数据为 `%LOCALAPPDATA%\DSHSpace`，其中 `backups` 保存 ZIP，`hidden` 保存隐藏原目录及操作记录，`settings.json` 保存来源选择。不会读写 Codex Space 的应用数据，不复制 Harness 根目录的凭据或配置到备份。

隐藏恢复绑定原来源目录身份。来源被重建或迁移时停止直接恢复；只有此前手动保存的 ZIP 可用于备份恢复。新版不会自动生成此类副本。

## 已验证格式

当前适配本机 DeepSeek Harness **0.2.0-rc.2 的 v4 会话**：`sessions/<项目编码>/<会话编码>/session.v4.jsonl.zstd`，同时支持该代普通 JSONL 阅读与原样备份。图片来自 `attachments/v1/objects/<摘要前两位>/<SHA256>`。v4 的 Zstandard 首帧为独立文件头，之后为事件帧。不同代同时存在时选择最高代，遇到未来代、损坏、歧义或无法完整枚举时明确提示，不回退到旧文件假装完整。

此工具不执行 Harness 的协议迁移。备份保留原始字节、会话目录内文件及被引用的图片；恢复不会改写消息。兼容导入和恢复旧版 ZIP，但旧版未收集的图片无法从 ZIP 补出，界面会标记“旧版备份 · 可能不含图片”。系统、助手和工具消息按原生 `data.message` 结构显示，未知事件保留为文本记录，过长事件明确标注有限预览。图片预览上限 20 MiB，不限制正常备份内图片的原始字节保留。

兼容证据见 [验证记录](docs/verification.md)。验证使用合成数据和安装包中未修改的原始持久化／SQLite 查询库，没有读取或移动真实用户会话。未进行原生桌面人工验收；其它 Harness 版本需另行验证。

## 开发

使用 Go 1.27.0、Node.js 24、Wails CLI 2.15.0；界面是无第三方前端依赖的本地静态资源。Go 依赖由 `go.mod` / `go.sum` 固定，Wails 和 WebView2 导航分支位于独立 `third_party`。

```powershell
# 从仓库固定的 Wails 分支编译 CLI：
pwsh -File scripts/setup-build-tools.ps1
pwsh -File scripts/test.ps1
pwsh -File scripts/build.ps1
node tests/preview-smoke.mjs
pwsh -File scripts/package.ps1
# 依赖缓存齐备时可加 -Offline，禁止下载：
pwsh -File scripts/test.ps1 -Offline
```

首次准备开发工具及 Go 依赖可能需要网络，程序核心功能离线运行。脚本默认缓存与临时数据放在本项目 `.cache`，不会向相邻项目写入。

`HarnessSessionManager-0.3.1.exe --demo` 使用含图片和两个工作区的独立合成来源，和普通窗口的单实例标识分开；演示模式不能更换来源或打开真实备份选择器。`--preview` 仅供开发验收，固定合成来源、绑定随机本机回环端口并要求随机请求令牌，不提供真实来源模式。

第三方许可证随免安装包的 `THIRD_PARTY_NOTICES.txt` 提供。本工程自身的公开再分发许可证尚未指定，不擅自套用第三方许可证。

## GitHub 自动打包与发布

推送 `main`、创建面向 `main` 的 PR 或手动运行 Windows workflow，都会执行源码测试、编译 Windows x64 EXE、以合成数据验收生产 EXE，并生成 ZIP、EXE、`SHA256SUMS.txt` 下载产物。云端测试不需要本机 Harness 程序或真实用户数据。

发行时先更新 `wails.json`、前端版本和发行说明，再推送与版本一致的标签，例如 `v0.3.1`。只有版本标签的构建验收成功后，Action 才会创建 Release、上传三个文件并设为最新版；不需要额外配置个人访问令牌。标签与程序版本不符会停止发布。

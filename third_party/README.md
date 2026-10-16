# 本地桌面依赖

Wails v2.15.0 与 go-webview2 v1.0.22 的本地导航修订分支，分别由主工程 go.mod 的 replace 指向本目录。代码已复制为独立目录，没有指向 Codex Space 的符号链接或运行依赖。分支用于执行原生外部导航限制；本应用启用 DisableExternalNavigation。

各目录保留原始上游许可证。运行包根目录 THIRD_PARTY_NOTICES.txt 收录本次实际 Go 模块及 Go 运行时的许可证原文。它不授予 Harness Session Manager 主工程一个新的公开许可证。

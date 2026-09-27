// Package tools 全项目共享的路径、HTTP 客户端、JSON 辅助方法。
package tools

// HideProcessWindow 隐藏子进程可能弹出的控制台窗口（仅 Windows 生效，其他平台为空操作）。
// 用于 java -version 探测、taskkill、整合包安装器、游戏本体等控制台程序的静默拉起；
// 对 GUI 子系统进程（explorer/rundll32 等）无副作用。
//
// 平台实现见 proc_windows.go / proc_other.go：syscall.SysProcAttr 的 HideWindow /
// CreationFlags 字段只在 Windows 上存在，写在共用文件里会让本包无法跨平台编译
// （运行时 GOOS 判断不够——编译期就会失败）。

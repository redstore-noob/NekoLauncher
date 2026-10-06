package bindings

// MonitorAPI / ServerAPI / SystemAPI：内存监控、服务器状态查询、版本与日志。

import (
	"nekolauncher/internal/info"
	"nekolauncher/internal/logs"
	"nekolauncher/internal/monitoring"
	"nekolauncher/internal/network"
	"nekolauncher/internal/tools"
)

// ---- MonitorAPI ----

// GetMemorySnapshot 内存快照：启动器 / JVM 内存与 Java 进程数。
func (a *MonitorAPI) GetMemorySnapshot() monitoring.MemorySnapshot { return monitoring.Snapshot() }

// GetSystemUsage 全系统占用：CPU / GPU / 内存百分比（性能监控小组件使用）。
func (a *MonitorAPI) GetSystemUsage() monitoring.SystemUsage { return monitoring.SystemUsageNow() }

// GetDiskUsage 指定路径所在分区的磁盘占用（磁盘空间小组件使用）。
func (a *MonitorAPI) GetDiskUsage(path string) (monitoring.DiskUsage, error) {
	return monitoring.UsageOf(path)
}

// ---- ServerAPI ----

// PingServer 查询 Minecraft 服务器状态（Server List Ping）。
func (a *ServerAPI) PingServer(host string, port int) (network.MinecraftServerStatus, error) {
	return network.Ping(host, port)
}

// ParseServerAddress 解析 "host" / "host:port" / "[ipv6]:port" 形式的服务器地址。
// 返回结构体而非 (host, port, err)：Wails 桥只支持 1 个返回值 + error。
func (a *ServerAPI) ParseServerAddress(input string) (network.ServerAddress, error) {
	return network.ParseServerAddress(input)
}

// ---- SystemAPI ----

// GetFormattedVersion 格式化版本号，如 "NekoLauncher版本号:1.0.0-preview4"。
func (a *SystemAPI) GetFormattedVersion() string { return info.FormatVersionString() }

// AddLog 写入一条日志；type 为 "ERROR" 时同时返回 error 语义（false）。
func (a *SystemAPI) AddLog(infoText, typ string) bool {
	return logs.AddLogs(infoText, nil, typ)
}

// WriteLog 直接写入共享日志文件。
func (a *SystemAPI) WriteLog(typ, infoText string) bool { return logs.Write(typ, infoText) }

// ClearLogs 清空日志目录，返回删除的文件数。
func (a *SystemAPI) ClearLogs() int { return logs.ClearLogs() }

// GetCurrentLog 读取本次运行的日志全文（运行日志查看器使用）。
func (a *SystemAPI) GetCurrentLog() (string, error) { return logs.ReadCurrent() }

// ExportCurrentLog 把本次运行的日志导出到指定路径。
func (a *SystemAPI) ExportCurrentLog(destination string) error {
	return logs.ExportCurrent(tools.SanitizeSavePath(destination))
}

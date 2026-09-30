// ServerHostAPI Minecraft 开服管理（internal/mcserver 的 Wails 绑定）。
package bindings

import (
	"context"

	"nekolauncher/internal/mcserver"
	"nekolauncher/internal/models"
)

// ServerHostAPI 开服：创建/启停/控制台/配置/文件/导入。
type ServerHostAPI struct {
	ctx context.Context
}

// Startup 注入 Wails runtime ctx，并把整包传输进度钩子桥接为事件。
func (a *ServerHostAPI) Startup(ctx context.Context) {
	a.ctx = ctx
	mcserver.TransferProgressHook = func(p mcserver.TransferProgress) {
		emit(ctx, "mcserver:transfer-progress", p)
	}
}

// ListServers 服务器列表（含运行状态与在线数）。
func (a *ServerHostAPI) ListServers() []mcserver.ServerInfo {
	return mcserver.ListServerInfos()
}

// ListServerMCVersions 某核心支持的 MC 版本（新→旧）。
func (a *ServerHostAPI) ListServerMCVersions(core string) ([]string, error) {
	return mcserver.ListServerMCVersions(callCtx(a.ctx), core)
}

// ListCoreVersions 某核心在指定 MC 版本下的服务端版本（新→旧）。
func (a *ServerHostAPI) ListCoreVersions(core, mcVersion string) ([]string, error) {
	return mcserver.ListCoreVersions(callCtx(a.ctx), core, mcVersion)
}

// RequiredJavaMajor 指定 MC 版本所需的最低 Java 主版本（0 表示未知，不校验）。
func (a *ServerHostAPI) RequiredJavaMajor(mcVersion string) int {
	if required := mcserver.RequiredJavaMajorVersion(mcVersion); required != nil {
		return *required
	}

	return 0
}

// CreateServer 创建服务器（AcceptEULA 必须为 true）。
func (a *ServerHostAPI) CreateServer(options mcserver.CreateOptions) (string, error) {
	return mcserver.CreateServer(callCtx(a.ctx), options)
}

// GetServerLaunchOptions 读取服务器的 JVM 启动参数（内存 + 额外参数）。
func (a *ServerHostAPI) GetServerLaunchOptions(id string) (mcserver.LaunchOptions, error) {
	options, err := mcserver.GetLaunchOptions(id)
	if options == nil {
		options = &mcserver.LaunchOptions{}
	}

	return *options, err
}

// SaveServerLaunchOptions 保存 JVM 启动参数（运行中拒绝；含参数校验）。
func (a *ServerHostAPI) SaveServerLaunchOptions(id string, options mcserver.LaunchOptions) error {
	return mcserver.SaveLaunchOptions(id, options)
}

// DeleteServer 删除服务器（必须已停止）。
func (a *ServerHostAPI) DeleteServer(id string) error {
	return mcserver.DeleteServer(id)
}

// StartServer 启动服务器进程。
func (a *ServerHostAPI) StartServer(id string) error {
	return mcserver.Default().StartServer(callCtx(a.ctx), id)
}

// RestartServer 重启服务器（先软停止保存世界，再启动；不会触发崩溃自动重启）。
func (a *ServerHostAPI) RestartServer(id string) error {
	return mcserver.Default().RestartServer(callCtx(a.ctx), id)
}

// GetServerAutoRestart 崩溃自动重启开关。
func (a *ServerHostAPI) GetServerAutoRestart(id string) bool {
	return mcserver.AutoRestartEnabled(id)
}

// SetServerAutoRestart 保存崩溃自动重启开关。
func (a *ServerHostAPI) SetServerAutoRestart(id string, enabled bool) error {
	return mcserver.SetAutoRestart(id, enabled)
}

// StopServer 停止服务器。force=false 软停止（stop 指令 + 宽限期后强杀），
// force=true 硬停止（立即结束进程树，可能丢失未保存的数据）。
func (a *ServerHostAPI) StopServer(id string, force bool) error {
	return mcserver.Default().StopServer(id, force)
}

// SendServerCommand 向控制台发送指令。
func (a *ServerHostAPI) SendServerCommand(id, command string) error {
	return mcserver.Default().SendServerCommand(id, command)
}

// ---- 服务端内容（mods / plugins） ----

// ServerContentKindForCore 服务器核心对应的内容目录类型（mods / plugins）。
func (a *ServerHostAPI) ServerContentKindForCore(core string) (string, error) {
	return mcserver.ServerContentKindForCore(core)
}

// ListServerContent 列出内容目录里的 jar（含 .disabled）。
func (a *ServerHostAPI) ListServerContent(id, kind string) []mcserver.ServerContentEntry {
	return mcserver.ListServerContent(id, kind)
}

// SetServerContentEnabled 启用/停用内容（靠 .disabled 后缀改名）。
func (a *ServerHostAPI) SetServerContentEnabled(id, kind, fileName string, enabled bool) error {
	return mcserver.SetServerContentEnabled(id, kind, fileName, enabled)
}

// DeleteServerContent 删除一个内容文件。
func (a *ServerHostAPI) DeleteServerContent(id, kind, fileName string) error {
	return mcserver.DeleteServerContent(id, kind, fileName)
}

// SearchServerContent 在 Modrinth 上按核心对应的加载器 + MC 版本搜索。
func (a *ServerHostAPI) SearchServerContent(query, gameVersion, core string, limit int) ([]models.ModrinthProject, error) {
	return mcserver.SearchServerContent(callCtx(a.ctx), query, gameVersion, core, limit)
}

// InstallServerContentFromModrinth 安装最新适配版本，返回（文件名, 版本号）。
func (a *ServerHostAPI) InstallServerContentFromModrinth(id, kind, projectID, gameVersion, core string) (string, string, error) {
	return mcserver.InstallServerContentFromModrinth(callCtx(a.ctx), id, kind, projectID, gameVersion, core)
}

// ---- 玩家与权限 ----

// GetServerPlayers 玩家与权限全貌：在线（RCON）、白名单、OP、封禁 + 白名单开关。
func (a *ServerHostAPI) GetServerPlayers(id string) mcserver.ServerPlayers {
	return mcserver.GetServerPlayers(id)
}

// SaveServerWhitelist 覆盖写入白名单（服务器须已停止；名字会自动补离线 UUID）。
func (a *ServerHostAPI) SaveServerWhitelist(id string, entries []mcserver.WhitelistEntry) error {
	return mcserver.SaveServerWhitelist(id, entries)
}

// SaveServerOps 覆盖写入 OP 列表（服务器须已停止）。
func (a *ServerHostAPI) SaveServerOps(id string, entries []mcserver.OpEntry) error {
	return mcserver.SaveServerOps(id, entries)
}

// SaveServerBanned 覆盖写入封禁名单（服务器须已停止）。
func (a *ServerHostAPI) SaveServerBanned(id string, entries []mcserver.BannedPlayerEntry) error {
	return mcserver.SaveServerBanned(id, entries)
}

// SetServerWhitelistEnabled 开关白名单（写 server.properties）。
func (a *ServerHostAPI) SetServerWhitelistEnabled(id string, enabled bool) error {
	return mcserver.SetWhitelistEnabled(id, enabled)
}

// RunServerPlayerCommand 在运行中的服务器上执行玩家/权限指令（RCON 优先）。
func (a *ServerHostAPI) RunServerPlayerCommand(id, command string) error {
	return mcserver.RunPlayerCommand(id, command)
}

// ---- 备份 ----

// ListServerBackups 备份列表（新→旧）。
func (a *ServerHostAPI) ListServerBackups(id string) []mcserver.BackupInfo {
	return mcserver.ListServerBackups(id)
}

// CreateServerBackup 立即备份一次（运行中走热备份：save-off → save-all flush → 压缩 → save-on）。
func (a *ServerHostAPI) CreateServerBackup(id string) (mcserver.BackupInfo, error) {
	return mcserver.CreateServerBackup(callCtx(a.ctx), id)
}

// DeleteServerBackup 删除一份备份。
func (a *ServerHostAPI) DeleteServerBackup(id, name string) error {
	return mcserver.DeleteServerBackup(id, name)
}

// RestoreServerBackup 用备份覆盖服务器目录（要求已停止；恢复前会自动留一份安全备份）。
func (a *ServerHostAPI) RestoreServerBackup(id, name string) error {
	return mcserver.RestoreServerBackup(callCtx(a.ctx), id, name)
}

// GetBackupSettings 备份策略（自动开关 / 间隔小时 / 保留份数 / 保留天数）。
func (a *ServerHostAPI) GetBackupSettings() mcserver.BackupSettings {
	return mcserver.CurrentBackupSettings()
}

// SaveBackupSettings 保存备份策略。
func (a *ServerHostAPI) SaveBackupSettings(settings mcserver.BackupSettings) {
	mcserver.SaveBackupSettings(settings)
}

// PollServer 轮询：状态/占用/在线数/cursor 之后的新日志。
func (a *ServerHostAPI) PollServer(id string, cursor int64) mcserver.Snapshot {
	return mcserver.Default().Poll(id, cursor)
}

// GetServerProperties 读取 server.properties。
func (a *ServerHostAPI) GetServerProperties(id string) ([]mcserver.Property, error) {
	return mcserver.GetServerProperties(id)
}

// SetServerProperties 保存 server.properties（运行中拒绝）。
func (a *ServerHostAPI) SetServerProperties(id string, properties []mcserver.Property) error {
	return mcserver.SetServerProperties(id, properties)
}

// ListServerFiles 服务器目录文件列表。
func (a *ServerHostAPI) ListServerFiles(id, relativePath string) ([]mcserver.FileEntry, error) {
	return mcserver.ListFiles(id, relativePath)
}

// ReadServerTextFile 读取文本文件（内置编辑器，≤2MB）。
func (a *ServerHostAPI) ReadServerTextFile(id, relativePath string) (string, error) {
	return mcserver.ReadServerTextFile(id, relativePath)
}

// WriteServerTextFile 保存文本文件。
func (a *ServerHostAPI) WriteServerTextFile(id, relativePath, content string) error {
	return mcserver.WriteServerTextFile(id, relativePath, content)
}

// ImportServerWorld 导入存档压缩包（zip）。
func (a *ServerHostAPI) ImportServerWorld(id, zipPath string) error {
	return mcserver.ImportServerWorld(id, zipPath)
}

// ImportServerMod 导入模组 jar（Fabric/NeoForge）。
func (a *ServerHostAPI) ImportServerMod(id, jarPath string) error {
	return mcserver.ImportServerMod(id, jarPath)
}

// ImportServerPlugin 导入插件 jar（Paper）。
func (a *ServerHostAPI) ImportServerPlugin(id, jarPath string) error {
	return mcserver.ImportServerPlugin(id, jarPath)
}

// ExportServer 导出服务器为 .nekoser 包（zip 容器；运行中拒绝）。
func (a *ServerHostAPI) ExportServer(id, destZip string) error {
	return mcserver.ExportServer(id, destZip)
}

// ImportServerNekoser 从 .nekoser 包恢复服务器，返回新服务器 id。
func (a *ServerHostAPI) ImportServerNekoser(zipPath string) (string, error) {
	return mcserver.ImportNekoser(zipPath)
}

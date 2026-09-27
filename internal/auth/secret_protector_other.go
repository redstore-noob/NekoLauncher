//go:build !windows

package auth

// 非 Windows：没有 DPAPI，统一走共用文件里的「密钥文件 + AES-256-GCM」方案。
// 这里提供与 Windows 文件同名的判定/包装函数，让 account_secret_protector.go
// 能在不引用 dpapi* 的前提下保持单一代码路径。

// platformSecretUsesDPAPI 非 Windows 恒为 false。
func platformSecretUsesDPAPI() bool { return false }

// platformProtectDPAPI 非 Windows 不会被调用（见 platformSecretUsesDPAPI）。
func platformProtectDPAPI(string) string { return "" }

// platformUnprotectDPAPI 非 Windows 不会被调用。
func platformUnprotectDPAPI(string) string { return "" }

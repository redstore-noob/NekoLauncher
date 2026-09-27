package bindings

// 离线皮肤设置与皮肤子系统共用的小工具。
// 对应 C# AccountStore.UpdateOfflineSkin 的扩展（支持本地 PNG 路径）。

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"nekolauncher/internal/auth"
	"nekolauncher/internal/config"
)

// ---- 离线皮肤设置（对应 AccountStore.UpdateOfflineSkin 的扩展） ----

// SetOfflineSkin 设置离线账号皮肤。
// path 为内置目录 Id（如 "steve"）时与 C# AccountStore.UpdateOfflineSkin 语义一致；
// 为本地 PNG 路径时为 Wails 版扩展：校验后复制到存储目录 appearance-cache/custom-skins，
// 并把复制后的路径写入 OfflineSkinId 持久化。
func (a *AccountAPI) SetOfflineSkin(accountID, path string) error {
	account := auth.Shared.FindByStableKey(accountID)
	if account == nil {
		return fmt.Errorf("账号不存在：%s", accountID)
	}
	if !strings.EqualFold(account.Type, "offline") {
		return errors.New("只能为离线账号设置离线皮肤。")
	}

	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return errors.New("皮肤标识或路径不能为空。")
	}
	if !isLocalPngPath(trimmed) {
		// 内置目录 Id：直接更新
		auth.Shared.UpdateOfflineSkin(account, GetOfflineSkinChoice(trimmed).Id)
		return nil
	}

	if err := validateSkinFile(trimmed); err != nil {
		return err
	}
	storage := strings.TrimSpace(config.StorageDirectory())
	if storage == "" {
		return errors.New("存储目录不可用，无法保存自定义皮肤。")
	}
	targetDir := filepath.Join(storage, "appearance-cache", "custom-skins")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}
	// 以账号稳定键命名，避免多账号互相覆盖
	key := strings.NewReplacer("\\", "_", "/", "_", ":", "_", " ", "_").Replace(accountID)
	targetPath := filepath.Join(targetDir, key+".png")
	if err := copyFile(trimmed, targetPath); err != nil {
		return err
	}
	auth.Shared.UpdateOfflineSkin(account, targetPath)
	return nil
}

// isLocalPngPath 值是否为本地 PNG 路径（而非内置目录 Id）。
func isLocalPngPath(value string) bool {
	if strings.ContainsAny(value, "\\/") || filepath.IsAbs(value) {
		return strings.EqualFold(filepath.Ext(value), ".png")
	}
	return false
}

// copyFile 复制文件（不存在目标目录时由调用方保证）。
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".tmp")
	if err != nil {
		return err
	}
	tmpName := out.Name()
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// ---- 小工具 ----

// LocalFileURL 把本地路径包装为 /localfile 流 URL（见 localfile_handler.go）。
func LocalFileURL(path string) string {
	return "/localfile?path=" + url.QueryEscape(path)
}

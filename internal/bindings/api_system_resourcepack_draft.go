package bindings

// SystemAPI 扩展：资源包制作器的自动保存草稿。
//
// 工程（贴图 base64 / 文本 / 二进制）序列化为一个 JSON 落在存储目录下的
// resourcepack-draft/，原子写入（先写 .tmp 再 rename），崩溃或误关后下次打开
// 可恢复。规模上限复用导出侧的常量，异常大的工程直接拒绝而不是写满磁盘。

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nekolauncher/internal/config"
)

// ResourcePackDraft 资源包工程草稿。Found 仅在读取时置位。
type ResourcePackDraft struct {
	Found   bool
	Name    string
	SavedAt string
	Files   []ResourcePackFile
}

const (
	// resourcePackDraftDirName 草稿子目录名（位于 config.StorageDirectory() 下）。
	resourcePackDraftDirName = "resourcepack-draft"
	// resourcePackDraftFileName 草稿文件名。
	resourcePackDraftFileName = "draft.json"
	// resourcePackDraftMaxBytes 序列化后 JSON 的体积上限，防止磁盘被写爆。
	resourcePackDraftMaxBytes = 256 << 20
)

// resourcePackDraftPath 草稿文件绝对路径。
func resourcePackDraftPath() string {
	return filepath.Join(
		config.StorageDirectory(),
		resourcePackDraftDirName,
		resourcePackDraftFileName,
	)
}

// SaveResourcePackDraft 原子写入工程草稿。
func (a *SystemAPI) SaveResourcePackDraft(draft ResourcePackDraft) error {
	if len(draft.Files) > resourcePackMaxFiles {
		return fmt.Errorf("草稿文件数超过上限（%d）", resourcePackMaxFiles)
	}
	var total int64
	for _, file := range draft.Files {
		total += int64(len(file.Text))
		total += int64(len(file.PngBase64)) * 3 / 4
		total += int64(len(file.Base64)) * 3 / 4
	}
	if total > resourcePackMaxTotalBytes {
		return fmt.Errorf("草稿体积超过上限（%d MB）", resourcePackMaxTotalBytes>>20)
	}

	payload := ResourcePackDraft{
		Found:   true,
		Name:    strings.TrimSpace(draft.Name),
		SavedAt: draft.SavedAt,
		Files:   draft.Files,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if int64(len(raw)) > resourcePackDraftMaxBytes {
		return fmt.Errorf("草稿序列化后超过上限（%d MB）", resourcePackDraftMaxBytes>>20)
	}

	target := resourcePackDraftPath()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		os.Remove(tmp)

		return err
	}

	return nil
}

// LoadResourcePackDraft 读取草稿；不存在时返回零值（Found=false）而不报错。
func (a *SystemAPI) LoadResourcePackDraft() (ResourcePackDraft, error) {
	target := resourcePackDraftPath()
	raw, err := os.ReadFile(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ResourcePackDraft{}, nil
		}

		return ResourcePackDraft{}, err
	}

	var draft ResourcePackDraft
	if err := json.Unmarshal(raw, &draft); err != nil {
		return ResourcePackDraft{}, fmt.Errorf("草稿已损坏：%w", err)
	}
	draft.Found = true

	return draft, nil
}

// ClearResourcePackDraft 删除草稿（不存在视为成功）。
func (a *SystemAPI) ClearResourcePackDraft() error {
	if err := os.Remove(resourcePackDraftPath()); err != nil &&
		!errors.Is(err, os.ErrNotExist) {
		return err
	}

	return nil
}

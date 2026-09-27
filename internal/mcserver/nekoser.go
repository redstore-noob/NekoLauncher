// NekoSer 服务器整包传输：.nekoser 文件（zip 容器）的导出与导入。
// 包内是服务器目录的原样快照（server.json、server.properties、存档、
// mods/plugins、核心 jar 等），导入时恢复为一个新的服务器目录。
package mcserver

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// TransferProgress 整包导出/导入的进度快照（done/total 为字节数）。
type TransferProgress struct {
	ServerID string `json:"serverId"`
	Phase    string `json:"phase"` // export | import
	Done     int64  `json:"done"`
	Total    int64  `json:"total"`
}

// TransferProgressHook 由绑定层注入（转发为 Wails 事件）；nil 时不上报。
var TransferProgressHook func(TransferProgress)

// transferReporter 按整百分比节流的上报器（字节级事件对大包太密）。
type transferReporter struct {
	serverID    string
	phase       string
	total       int64
	lastPercent int
}

// report 每跨越一个整数百分比上报一次；final=true 时无论如何都上报终值。
func (r *transferReporter) report(done int64, final bool) {
	if TransferProgressHook == nil || r.total <= 0 {
		return
	}
	percent := int(done * 100 / r.total)
	if percent == r.lastPercent && !final {
		return
	}
	r.lastPercent = percent
	TransferProgressHook(TransferProgress{
		ServerID: r.serverID,
		Phase:    r.phase,
		Done:     done,
		Total:    r.total,
	})
}

// ExportServer 把服务器目录整包压缩为 .nekoser 文件。运行中禁止导出
// （世界与配置随时在变，导出来的包不完整）。
func ExportServer(id, destZip string) error {
	if Default().IsRunning(id) {
		return errors.New("服务器运行中，请先停止")
	}
	if err := validateID(id); err != nil {
		return err
	}
	root := serverDirectory(id)
	if _, err := loadServerConfig(root); err != nil {
		return errors.New("服务器不存在")
	}

	// 预扫描：收集文件清单与总字节量，供进度上报
	type fileTask struct {
		path string
		rel  string
		size int64
	}
	var tasks []fileTask
	var totalBytes int64
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		tasks = append(tasks, fileTask{path: path, rel: rel, size: info.Size()})
		totalBytes += info.Size()

		return nil
	})
	if walkErr != nil {
		return walkErr
	}

	out, err := os.Create(destZip)
	if err != nil {
		return err
	}
	defer out.Close()
	writer := zip.NewWriter(out)
	reporter := &transferReporter{serverID: id, phase: "export", total: totalBytes}

	var written int64
	for _, task := range tasks {
		header := &zip.FileHeader{Name: filepath.ToSlash(task.rel), Method: zip.Deflate}
		target, err := writer.CreateHeader(header)
		if err != nil {
			writer.Close()

			return err
		}
		source, err := os.Open(task.path)
		if err != nil {
			writer.Close()

			return err
		}
		_, copyErr := io.Copy(target, source)
		source.Close()
		if copyErr != nil {
			writer.Close()

			return copyErr
		}
		written += task.size
		reporter.report(written, false)
	}
	reporter.report(written, true)

	return writer.Close()
}

// ImportNekoser 从 .nekoser 包恢复服务器，返回新服务器 id。
// 包内必须含 server.json（合法性的锚点）；id 撞名时自动另起目录。
func ImportNekoser(zipPath string) (string, error) {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", fmt.Errorf("不是有效的 .nekoser 压缩包：%w", err)
	}
	defer reader.Close()

	// 先全部清洗路径（拦 ../ 越界）并解析 server.json，再落盘
	entries := make(map[string]*zip.File, len(reader.File))
	var cfg *ServerConfig
	for _, file := range reader.File {
		name, err := cleanZipPath(file.Name)
		if err != nil {
			return "", err
		}
		if name == "" {
			continue
		}
		entries[name] = file
		if name == "server.json" {
			rc, err := file.Open()
			if err != nil {
				return "", err
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return "", err
			}
			cfg = &ServerConfig{}
			if err := json.Unmarshal(data, cfg); err != nil {
				return "", fmt.Errorf("server.json 解析失败：%w", err)
			}
		}
	}
	if cfg == nil {
		return "", errors.New("不是有效的 .nekoser 服务器包（缺少 server.json）")
	}

	name := sanitizeServerName(cfg.Name)
	if name == "" {
		name = "server"
	}
	id := uniqueServerDirectory(name)
	dir := filepath.Join(ServerRootDirectory(), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	var totalBytes int64
	for _, file := range entries {
		if !file.FileInfo().IsDir() {
			totalBytes += int64(file.UncompressedSize64)
		}
	}
	reporter := &transferReporter{serverID: id, phase: "import", total: totalBytes}

	var written int64
	for name, file := range entries {
		if file.FileInfo().IsDir() {
			continue
		}
		dest := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return "", err
		}
		if err := copyZipEntry(file, dest); err != nil {
			return "", err
		}
		written += int64(file.UncompressedSize64)
		reporter.report(written, false)
	}
	reporter.report(written, true)

	// id 以实际落盘目录为准，包内元数据的旧 id 作废
	cfg.ID = id
	if err := saveServerConfig(dir, cfg); err != nil {
		return "", err
	}

	return id, nil
}

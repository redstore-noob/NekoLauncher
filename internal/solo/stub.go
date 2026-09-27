// 安装器模板（stub）的查找：导出 exe 时把载荷拼到模板后面。
// 模板由 NekoSolo/NekoSolo.Installer（C# WPF）单独构建，不进启动器二进制，
// 避免启动器被 C# 工具链绑架（也符合"本体尽量不集成"的约束）。
package solo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// stubEnvKey 指定模板路径的环境变量（测试与高级用户覆盖用）。
const stubEnvKey = "NEKOSOLO_STUB"

// stubCandidateNames 模板文件的候选文件名（按序尝试）。
var stubCandidateNames = []string{"NekoSolo.Installer.exe", "NekoSoloInstaller.exe"}

// StubStatus 模板查找结果（供前端提示导出可用性）。
type StubStatus struct {
	Found bool
	Path  string
}

// FindStubTemplate 按以下顺序查找安装器模板：
//  1. 环境变量 NEKOSOLO_STUB
//  2. 启动器 exe 同级 tools/NekoSolo/*.exe
//  3. 启动器 exe 同级 NekoSolo/*.exe
//  4. 工作目录 NekoSolo/build/*.exe（仓库开发布局的推荐发布位置）
func FindStubTemplate() (string, error) {
	if env := strings.TrimSpace(os.Getenv(stubEnvKey)); env != "" {
		if info, err := os.Stat(env); err == nil && !info.IsDir() {
			return env, nil
		}
		return "", fmt.Errorf("环境变量 %s 指向的文件不存在：%s", stubEnvKey, env)
	}

	var searched []string
	for _, base := range stubSearchBases() {
		for _, name := range stubCandidateNames {
			candidate := filepath.Join(base, name)
			searched = append(searched, candidate)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate, nil
			}
		}
	}
	return "", errors.New(
		"未找到 NekoSolo 安装器模板。请先构建 NekoSolo/NekoSolo.Installer 并把产物放到 " +
			"NekoSolo/build/（或设置环境变量 " + stubEnvKey + "）。已查找：\n" +
			strings.Join(searched, "\n"))
}

// StubTemplateStatus 供绑定层查询模板是否就绪。
func StubTemplateStatus() StubStatus {
	path, err := FindStubTemplate()
	if err != nil {
		return StubStatus{Found: false}
	}
	return StubStatus{Found: true, Path: path}
}

// stubSearchBases 返回候选目录（不含文件名）。抽成变量仅为测试注入。
var stubSearchBases = defaultStubSearchBases

func defaultStubSearchBases() []string {
	var bases []string
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		bases = append(bases,
			filepath.Join(exeDir, "tools", "NekoSolo"),
			filepath.Join(exeDir, "NekoSolo"),
		)
	}
	if cwd, err := os.Getwd(); err == nil {
		bases = append(bases, filepath.Join(cwd, "NekoSolo", "build"))
	}
	return bases
}

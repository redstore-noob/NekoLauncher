// java_lookup.go 本包内的 Java 可执行文件查找。
//
// 与 internal/launch 的 JavaRuntimeLocator 是两套并存实现：本包在安装 Loader /
// 运行安装器时需要"能跑起来就行"的 Java，不关心版本号匹配，因此只做
// 托管运行时目录 → JAVA_HOME → PATH 的朴素查找；版本匹配由 launch 包负责。
package download

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// FindJavaExecutable 查找本机可用的 java 可执行文件。
// 优先在托管运行时目录（<mcDir>/runtime）递归扫描，回退 JAVA_HOME / PATH。
// 对应 C# JavaRuntimeLocator.FindJavaExecutable。
func FindJavaExecutable(runtimeDirectory string) string {
	javaName := "java"
	if runtime.GOOS == "windows" {
		javaName = "java.exe"
	}
	// 1) 托管运行时目录递归扫描
	if runtimeDirectory != "" {
		if found := findFileInTree(runtimeDirectory, javaName, "bin"); found != "" {
			return found
		}
	}
	// 2) JAVA_HOME
	if home := os.Getenv("JAVA_HOME"); home != "" {
		candidate := filepath.Join(home, "bin", javaName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	// 3) PATH
	if path, err := exec.LookPath(javaName); err == nil {
		return path
	}
	return ""
}

// findFileInTree 在目录树中查找指定文件名（要求父目录为 parentName，如 "bin"）。
func findFileInTree(root, fileName, parentName string) string {
	var found string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || found != "" {
			if found != "" {
				return filepath.SkipAll
			}
			return nil
		}
		if d.IsDir() || d.Name() != fileName {
			return nil
		}
		if filepath.Base(filepath.Dir(path)) == parentName {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

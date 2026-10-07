package launch

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"nekolauncher/internal/tools"
)

// NativeLibrary 一个待解压的 native 依赖库。
type NativeLibrary struct {
	ArchivePath string
	Exclusions  []string
}

// MinecraftLibraryResolver 版本依赖库解析与 natives 解压
// （对应 C# Internal/MinecraftLibraryResolver）。
type MinecraftLibraryResolver struct{}

// resolvedLibraries 解析结果：classpath 条目 + 待解压 natives。
type resolvedLibraries struct {
	Classpath []string
	Natives   []NativeLibrary
}

// Resolve 按 rules 过滤版本库列表，返回 classpath 与 native 依赖；
// 缺失文件时报错（列出前 5 个缺失项）。
func (MinecraftLibraryResolver) Resolve(
	ctx context.Context,
	profile *MinecraftVersionProfile,
	minecraftDirectory string,
	features map[string]bool,
) (*resolvedLibraries, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	librariesDirectory := filepath.Join(minecraftDirectory, "libraries")
	classpath := make([]string, 0, len(profile.Libraries)+1)
	var natives []NativeLibrary
	var missingFiles []string
	seen := map[string]bool{}

	for _, library := range profile.Libraries {
		if !MinecraftRuleEvaluator.IsAllowed(library, features) {
			continue
		}

		var descriptor libraryJSON
		if err := json.Unmarshal(library, &descriptor); err != nil {
			continue
		}

		if artifactRelativePath, ok := descriptor.tryArtifactPath(); ok {
			artifactPath, ok := toAbsoluteLibraryPath(librariesDirectory, artifactRelativePath)
			if !ok {
				continue
			}
			if tools.FileExists(artifactPath) {
				key := classpathKey(artifactPath)
				if !seen[key] {
					seen[key] = true
					classpath = append(classpath, artifactPath)
				}
			} else {
				missingFiles = append(missingFiles, artifactPath)
			}
		}

		nativeRelativePath, ok := descriptor.tryNativePath()
		if !ok {
			continue
		}
		nativePath, ok := toAbsoluteLibraryPath(librariesDirectory, nativeRelativePath)
		if !ok {
			continue
		}
		if tools.FileExists(nativePath) {
			natives = append(natives, NativeLibrary{
				ArchivePath: nativePath,
				Exclusions:  descriptor.nativeExclusions(),
			})
		} else {
			missingFiles = append(missingFiles, nativePath)
		}
	}

	clientJar := filepath.Join(
		minecraftDirectory,
		"versions",
		profile.ClientJarVersionId,
		profile.ClientJarVersionId+".jar")
	if tools.FileExists(clientJar) {
		classpath = append(classpath, clientJar)
	} else {
		missingFiles = append(missingFiles, clientJar)
	}

	if len(missingFiles) > 0 {
		previewCount := len(missingFiles)
		if previewCount > 5 {
			previewCount = 5
		}
		preview := strings.Join(missingFiles[:previewCount], "\n")
		suffix := ""
		if len(missingFiles) > 5 {
			suffix = fmt.Sprintf("\n……另有 %d 个文件", len(missingFiles)-5)
		}
		return nil, newLaunchError(
			fmt.Sprintf("版本文件不完整，缺少以下依赖：\n%s%s", preview, suffix))
	}

	return &resolvedLibraries{Classpath: classpath, Natives: natives}, nil
}

// ExtractNatives 解压 native 依赖到 Minecraft 目录下的 .nya-natives
// （避开 Linux /tmp 的 noexec 挂载与临时目录泄漏），返回解压目录。
// minecraftDirectory 为空时回退系统临时目录以保持向后兼容。
func (MinecraftLibraryResolver) ExtractNatives(
	ctx context.Context,
	versionId string,
	nativeLibraries []NativeLibrary,
	minecraftDirectory string,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var safeVersionId strings.Builder
	for _, character := range versionId {
		if strings.ContainsRune(`<>:"/\|?*`, character) || character < 0x20 {
			safeVersionId.WriteRune('_')
		} else {
			safeVersionId.WriteRune(character)
		}
	}
	// 优先解压到 Minecraft 目录下；未提供目录时回退系统临时目录。
	var baseDirectory string
	if strings.TrimSpace(minecraftDirectory) != "" {
		baseDirectory = filepath.Join(minecraftDirectory, ".nya-natives")
	} else {
		baseDirectory = filepath.Join(os.TempDir(), "NekoLauncher", "natives")
	}
	cleanupStaleNativeDirectories(baseDirectory)
	nativeDirectory := filepath.Join(baseDirectory, fmt.Sprintf("%s-%s", safeVersionId.String(), randomHexIdentifier()))
	if err := os.MkdirAll(nativeDirectory, 0o755); err != nil {
		return "", err
	}

	for _, nativeLibrary := range nativeLibraries {
		if err := extractNativeArchive(nativeLibrary, nativeDirectory); err != nil {
			TryDeleteDirectory(nativeDirectory)
			return "", err
		}
	}
	return nativeDirectory, nil
}

// TryDeleteDirectory 尽力删除目录；游戏退出后的临时文件清理不应影响主流程。
func TryDeleteDirectory(directory string) {
	if directory == "" {
		return
	}
	_ = os.RemoveAll(directory)
}

// cleanupStaleNativeDirectories 清理超过 7 天未被修改的旧 natives 解压目录，
// 防止启动器被强制终止或断电时 .nya-natives 下无限累积 GUID 目录。
func cleanupStaleNativeDirectories(baseDirectory string) {
	if !tools.DirectoryExists(baseDirectory) {
		return
	}
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	entries, err := os.ReadDir(baseDirectory)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		fullPath := filepath.Join(baseDirectory, entry.Name())
		info, statErr := entry.Info()
		if statErr != nil || info.ModTime().After(cutoff) {
			continue
		}
		// 单个目录清理失败不影响其余
		_ = os.RemoveAll(fullPath)
	}
}

// extractNativeArchive 解压单个 native JAR；跳过 META-INF 与声明的排除前缀，
// 并防御 zip-slip 路径穿越。
func extractNativeArchive(nativeLibrary NativeLibrary, destinationRoot string) error {
	archive, err := zip.OpenReader(nativeLibrary.ArchivePath)
	if err != nil {
		return err
	}
	defer archive.Close()

	for _, entry := range archive.File {
		normalizedName := strings.ReplaceAll(entry.Name, "\\", "/")
		if entry.Name == "" ||
			strings.HasPrefix(strings.ToLower(normalizedName), "meta-inf/") ||
			hasExclusionPrefix(normalizedName, nativeLibrary.Exclusions) {
			continue
		}

		destinationPath, err := filepath.Abs(
			filepath.Join(destinationRoot, filepath.FromSlash(normalizedName)))
		if err != nil {
			return err
		}
		if err := ensureContainedPath(destinationRoot, destinationPath); err != nil {
			return err
		}

		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(destinationPath, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
			return err
		}
		if err := extractZipEntry(entry, destinationPath); err != nil {
			return err
		}
	}
	return nil
}

func hasExclusionPrefix(name string, exclusions []string) bool {
	for _, exclusion := range exclusions {
		if strings.HasPrefix(strings.ToLower(name), strings.ToLower(exclusion)) {
			return true
		}
	}
	return false
}

func extractZipEntry(entry *zip.File, destinationPath string) error {
	source, err := entry.Open()
	if err != nil {
		return err
	}
	defer source.Close()

	target, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer target.Close()
	_, err = io.Copy(target, source)
	return err
}

// ensureContainedPath 防御解压/库路径逃逸：目标必须位于 root 内部。
func ensureContainedPath(root, candidate string) error {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(absoluteRoot, candidate)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return newLaunchError("Native 依赖包含不安全的解压路径。")
	}
	return nil
}

// ---------------------------------------------------------------------------
// 版本 JSON 库条目描述
// ---------------------------------------------------------------------------

// libraryJSON 版本 JSON 的单个库条目（按需字段）。
type libraryJSON struct {
	Name      string                `json:"name"`
	Natives   map[string]string     `json:"natives"`
	Downloads *libraryDownloadsJSON `json:"downloads"`
	Extract   *libraryExtractJSON   `json:"extract"`
}

type libraryArtifactJSON struct {
	Path string `json:"path"`
}

type libraryDownloadsJSON struct {
	Artifact    *libraryArtifactJSON            `json:"artifact"`
	Classifiers map[string]*libraryArtifactJSON `json:"classifiers"`
}

type libraryExtractJSON struct {
	Exclude []string `json:"exclude"`
}

// tryArtifactPath 解析主 artifact 的相对路径：优先 downloads.artifact.path，
// 否则用 maven 坐标（name）拼装。
// natives-only 库（旧版本如 1.7.x 的 lwjgl-platform 只声明 natives classifier，
// 主 jar 从未发布也从不进 classpath）没有主构件，返回 false 跳过检查；
// 否则启动侧会永远报"文件不完整"，且下载/修复侧都不会补这个文件。
func (l *libraryJSON) tryArtifactPath() (string, bool) {
	if l.Downloads != nil && l.Downloads.Artifact != nil && l.Downloads.Artifact.Path != "" {
		return l.Downloads.Artifact.Path, true
	}
	if len(l.Natives) > 0 {
		return "", false
	}
	return mavenNameToRelativePath(l.Name)
}

// tryNativePath 解析当前平台 natives JAR 的相对路径。
func (l *libraryJSON) tryNativePath() (string, bool) {
	if l.Natives == nil {
		return "", false
	}
	classifierElement, ok := l.Natives[operatingSystemName()]
	if !ok {
		return "", false
	}

	// ${arch} 占位符按当前进程位数展开。
	// 只出现于老版本（1.7.10/1.8.9 的 twitch-platform 等），它们的 classifiers
	// 只有 natives-windows-32 / natives-windows-64：arm64 必须同样映射成 "64"，
	// 与下载侧（minecraft_version_installer / game_file_verifier）保持一致，
	// 否则 arm64 上会去找根本不存在的 natives-windows-arm64。
	var architecture string
	switch runtime.GOARCH {
	case "386":
		architecture = "32"
	default:
		architecture = "64"
	}
	classifier := strings.ReplaceAll(classifierElement, "${arch}", architecture)

	// 新版本：downloads.classifiers 提供精确路径
	if l.Downloads != nil && l.Downloads.Classifiers != nil {
		if nativeArtifact, ok := l.Downloads.Classifiers[classifier]; ok && nativeArtifact.Path != "" {
			return nativeArtifact.Path, true
		}
	}

	// 旧版本（1.7.x 及更早）：版本 JSON 无 downloads 字段，
	// 用 name + classifier 拼出 natives JAR 路径（org.lwjgl:lwjgl-platform:2.9.4 + natives-windows）
	return mavenNameToRelativePathWithClassifier(l.Name, classifier)
}

// nativeExclusions 读取 extract.exclude 列表（统一为 / 分隔）。
func (l *libraryJSON) nativeExclusions() []string {
	if l.Extract == nil {
		return nil
	}
	result := make([]string, 0, len(l.Extract.Exclude))
	for _, exclusion := range l.Extract.Exclude {
		result = append(result, strings.ReplaceAll(exclusion, "\\", "/"))
	}
	return result
}

// mavenNameToRelativePath maven 坐标 → 相对路径（默认 jar 扩展名）。
func mavenNameToRelativePath(name string) (string, bool) {
	extension := "jar"
	coordinate := name
	if index := strings.Index(name, "@"); index >= 0 {
		extension = name[index+1:]
		coordinate = name[:index]
	}
	parts := strings.Split(coordinate, ":")
	if len(parts) < 3 || len(parts) > 4 {
		return "", false
	}
	classifier := ""
	if len(parts) == 4 {
		classifier = "-" + parts[3]
	}
	fileName := fmt.Sprintf("%s-%s%s.%s", parts[1], parts[2], classifier, extension)
	relativePath := filepath.Join(
		strings.ReplaceAll(parts[0], ".", string(filepath.Separator)),
		parts[1],
		parts[2],
		fileName)
	return relativePath, true
}

// mavenNameToRelativePathWithClassifier 指定 classifier 的 natives JAR 相对路径。
func mavenNameToRelativePathWithClassifier(name, classifier string) (string, bool) {
	coordinate := name
	if index := strings.Index(name, "@"); index >= 0 {
		coordinate = name[:index]
	}
	parts := strings.Split(coordinate, ":")
	if len(parts) < 3 || len(parts) > 4 {
		return "", false
	}
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return "", false
		}
	}
	relativePath := filepath.Join(
		strings.ReplaceAll(parts[0], ".", string(filepath.Separator)),
		parts[1],
		parts[2],
		fmt.Sprintf("%s-%s-%s.jar", parts[1], parts[2], classifier))
	return relativePath, true
}

// toAbsoluteLibraryPath 库相对路径 → 受 libraryRoot 约束的绝对路径。
func toAbsoluteLibraryPath(libraryRoot, relativePath string) (string, bool) {
	normalized := filepath.FromSlash(relativePath)
	absolute, err := filepath.Abs(filepath.Join(libraryRoot, normalized))
	if err != nil {
		return "", false
	}
	if err := ensureContainedPath(libraryRoot, absolute); err != nil {
		return "", false
	}
	return absolute, true
}

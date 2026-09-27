// 残留进程清理：启动器异常退出后，此前启动的 Java 服务端会以孤儿进程形式
// 继续占用服务器目录（session.lock、logs/latest.log），导致下次启动因
// 文件被占用而失败。启动前先检测并结束这些进程。
package mcserver

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

// killProcessTree 结束进程及其全部子进程。直接 Kill 只会命中直接子进程，
// 若用户配置的是 Oracle javapath 之类的转发器（shim），真正的 JVM 是孙进程，
// 会活下来变成占用服务器目录的孤儿进程。
func killProcessTree(pid int) error {
	if runtime.GOOS == "windows" {
		// /T 连同子进程一起结束，/F 强制（此时已超过停止宽限期）
		if err := exec.Command("taskkill", "/PID", fmt.Sprint(pid), "/T", "/F").Run(); err == nil {
			return nil
		}
		// taskkill 不可用时退回普通 Kill，至少结束直接子进程
	}
	if p, err := process.NewProcess(int32(pid)); err == nil {
		return p.Kill()
	}

	return fmt.Errorf("进程 %d 已不存在", pid)
}

// processAlive 判断 pid 对应的进程是否还活着。
// 用 gopsutil 而不是 os.FindProcess + Signal(0)：后者在 Windows 上恒返回成功，
// 判断不了存活；这里也不依赖 Wait 收尸（强杀路径上没人 Wait）。
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := process.NewProcess(int32(pid))
	if err != nil {
		return false
	}
	alive, err := p.IsRunning()
	if err != nil {
		return false
	}

	return alive
}

// killOrphanServerProcesses 结束所有工作目录或命令行包含 dir 的进程，
// 返回给控制台展示的说明行。自家正常运行的实例由 Manager 状态保护，
// 本函数只在 StartServer 通过状态检查后调用（此时该 id 不应有存活进程）。
func killOrphanServerProcesses(dir string) []string {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil
	}
	var victims []*process.Process
	procs, err := process.Processes()
	if err != nil {
		return nil
	}
	for _, p := range procs {
		if matchesServerProcess(p, absDir) {
			victims = append(victims, p)
		}
	}
	if len(victims) == 0 {
		return nil
	}

	var notes []string
	for _, p := range victims {
		if err := p.Kill(); err != nil {
			notes = append(notes, fmt.Sprintf(
				"检测到占用服务器目录的残留进程 (PID %d) 结束失败：%v", p.Pid, err))
			continue
		}
		notes = append(notes, fmt.Sprintf(
			"已结束残留的服务器进程 (PID %d)，它仍在占用服务器目录", p.Pid))
	}

	// 等待句柄真正释放（session.lock/latest.log 可写），最多 5 秒。
	// 除进程表外还探测文件本身：Cwd/Cmdline 对提权进程可能读不到，
	// 但文件被占用是实打实的信号。
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !matchesServerProcessAny(absDir) && !serverFilesLocked(absDir) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	return notes
}

// serverFilesLocked 探测服务器目录是否仍被别的进程占用：
// 以独占写方式打开 logs/latest.log，失败说明还有进程持有它。
func serverFilesLocked(dir string) bool {
	path := filepath.Join(dir, "logs", "latest.log")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return true
	}
	_ = file.Close()

	return false
}

// matchesServerProcess 判断进程是否就是"跑在 absDir 里的服务端进程"。
//
// 两条证据，都要求路径是**完整的一段**，不能只做子串匹配：
//   - 工作目录恰好等于服务器目录（启动器拉起 Java 时用的就是它，最强证据）；
//   - 命令行里出现了以分隔符/引号/空白/串尾收束的服务器目录，且进程名像 JVM。
//
// 只写 strings.Contains 会误杀两类无辜进程：目录名互为前缀的兄弟服务器
// （启动 s1 把正在跑的 s10 一起杀了），以及"碰巧打开着服务器目录里某个文件"
// 的其它程序（编辑器等）。
func matchesServerProcess(p *process.Process, absDir string) bool {
	if cwd, err := p.Cwd(); err == nil && cwd != "" {
		if strings.EqualFold(cwd, absDir) {
			return true
		}
	}

	cmdline, err := p.Cmdline()
	if err != nil || cmdline == "" {
		return false
	}

	return containsPathSegment(cmdline, absDir) && looksLikeJVM(p)
}

// containsPathSegment 判断 cmdline 中 absDir 是否作为完整路径段出现：
// 命中位置的后一个字符必须是路径分隔符、引号、空白或字符串结尾。
func containsPathSegment(cmdline, absDir string) bool {
	haystack := strings.ToLower(cmdline)
	needle := strings.ToLower(absDir)
	if needle == "" {
		return false
	}

	for offset := 0; offset+len(needle) <= len(haystack); {
		index := strings.Index(haystack[offset:], needle)
		if index < 0 {
			return false
		}
		end := offset + index + len(needle)
		if end == len(haystack) {
			return true
		}
		switch haystack[end] {
		case '\\', '/', '"', '\'', ' ', '\t', '\r', '\n':
			return true
		}
		offset = offset + index + 1
	}

	return false
}

// looksLikeJVM 进程名是否像 JVM（java / javaw / java.exe，或路径里的 jdk/jre 可执行文件）。
func looksLikeJVM(p *process.Process) bool {
	name, err := p.Name()
	if err != nil {
		return false
	}

	return strings.Contains(strings.ToLower(name), "java")
}

func matchesServerProcessAny(absDir string) bool {
	procs, err := process.Processes()
	if err != nil {
		return false
	}
	for _, p := range procs {
		if matchesServerProcess(p, absDir) {
			return true
		}
	}

	return false
}

package launch

// launch_transform_provider.go 启动变换的来源接线。
//
// MinecraftLaunchTransform 本身是"宿主在解析元数据之后、渲染 Java 命令之前施加的
// 变换"（类路径前后插入、主类 / Java / 工作目录覆盖、环境变量注入）。它是从 C# 版
// 完整移植过来的能力，但宿主自己不用它——真正的使用者是**插件**：插件想换主类、
// 挂 javaagent、往 classpath 里塞自己的 jar，都靠这份变换。
//
// 这一层就是那个缺口：启动管线不依赖插件系统（bindings 反过来依赖 launch），
// 由绑定层注入提供者，把插件清单里的贡献解析成变换。

// LaunchTransformProvider 宿主注入的变换提供者：每次启动前调用一次。
// 返回 nil 或未注入时使用空变换（等价于"没有插件贡献"）。
var LaunchTransformProvider func() *MinecraftLaunchTransform

// CurrentLaunchTransform 取本次启动生效的变换（启动管线与需要自检的调用方共用）。
func CurrentLaunchTransform() *MinecraftLaunchTransform { return currentLaunchTransform() }

// currentLaunchTransform 取本次启动生效的变换。
func currentLaunchTransform() *MinecraftLaunchTransform {
	if LaunchTransformProvider == nil {
		return &MinecraftLaunchTransform{}
	}
	if transform := LaunchTransformProvider(); transform != nil {
		return transform
	}

	return &MinecraftLaunchTransform{}
}

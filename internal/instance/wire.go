// 包初始化：向 content 包注入外部实例识别钩子。
// content 不能反向 import instance（会造成循环依赖），
// 因此 ResolveInstanceVisual 所需的外部实例识别通过钩子注入（见各自 PORTING_NOTES.md）。
package instance

import "nekolauncher/internal/content"

func init() {
	content.ExternalInstanceResolver = func(sourcePath string) (content.ExternalInstanceLayout, bool) {
		external, ok := TryResolveExternalInstance(sourcePath)
		if !ok {
			return content.ExternalInstanceLayout{}, false
		}
		return content.ExternalInstanceLayout{
			InstanceId:        external.InstanceId,
			InstanceDirectory: external.InstanceDirectory,
			LauncherRoot:      external.LauncherRoot,
		}, true
	}
}

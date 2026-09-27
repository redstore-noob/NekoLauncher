/*
 * 资源搜索的纯函数辅助（X-3）。
 *
 * 为什么单独成模块：弹层组件（ResourceSearchDialog）里塞满 HeroUI / 绑定调用，
 * 不好做单测；而"项目类型 → 实例内容子目录"这种映射一旦写错（光影包装进
 * resourcepacks），游戏里就是直接看不到东西，必须由测试盯住。
 *
 * 映射口径与 Go 侧 models.SubDirectoryForProjectType 保持一致：新增类型要两边一起改。
 */

/** 项目类型 → 实例内容子目录（未知类型按 Mod 处理，mods 目录一定存在） */
export function subDirectoryForProjectType(projectType: string): string {
  switch (projectType) {
    case "shader":
    case "shaderpack":
      return "shaderpacks";
    case "resourcepack":
    case "resourcepacks":
      return "resourcepacks";
    default:
      return "mods";
  }
}

/** 结果列表里的来源标签（两个资源站混排时用得上） */
export function sourceLabel(source: string): string {
  return source === "curseforge" ? "CurseForge" : "Modrinth";
}

import type { instance } from "../../wailsjs/go/models";

// 选中项以及随之变化的隔离目录不改变实例列表，无需整页重载。
export function isInstanceSelectionChange(
  previous: instance.GameInstanceSnapshot | null,
  next: instance.GameInstanceSnapshot,
): boolean {
  return (
    previous !== null &&
    !previous.IsLoading &&
    !next.IsLoading &&
    previous.SourcePath === next.SourcePath &&
    previous.MinecraftDirectory === next.MinecraftDirectory &&
    previous.ErrorMessage === next.ErrorMessage &&
    previous.VersionIds.length === next.VersionIds.length &&
    previous.VersionIds.every((id, index) => id === next.VersionIds[index])
  );
}

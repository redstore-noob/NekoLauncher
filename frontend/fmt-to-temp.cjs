// 用 prettier 格式化指定文件，结果写入临时目录（工作区外的 temp 允许写），
// 之后由 harness 的 write 工具把内容搬回工作区。
const prettier = require("prettier");
const fs = require("fs");
const path = require("path");

(async () => {
  const files = process.argv.slice(2);
  const outDir = process.env.TEMP || "/tmp";

  for (const f of files) {
    const src = fs.readFileSync(f, "utf8");
    const formatted = await prettier.format(src, { filepath: f });
    const dest = path.join(outDir, "fmt-" + path.basename(f));
    fs.writeFileSync(dest, formatted, "utf8");
    const changed = formatted === src ? "无需改动" : "已格式化";
    console.log(`${f} -> ${dest} (${changed}, ${formatted.length} 字节)`);
  }
})().catch((e) => {
  console.error("ERR", e.message);
  process.exit(1);
});

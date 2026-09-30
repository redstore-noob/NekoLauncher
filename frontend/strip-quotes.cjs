// 把 locale 分片里「纯中文标识符 key」的多余引号去掉（prettier quote-props 规则）。
// 不直接写回源文件（沙盒禁止外部进程写工作区），只输出修复后的内容到 stdout 供核对。
const fs = require("fs");

const files = process.argv.slice(2);
// 合法 JS 标识符：中日韩文字 + 字母数字 _ $
const IDENT = /^[A-Za-z_$\u4e00-\u9fff\u3040-\u30ff\uac00-\ud7af][A-Za-z0-9_$\u4e00-\u9fff\u3040-\u30ff\uac00-\ud7af]*$/;
const QUOTED_KEY = /^(\s*)"([^"]+)":/;

for (const f of files) {
  const lines = fs.readFileSync(f, "utf8").split("\n");
  let n = 0;
  const out = lines.map((line) => {
    const m = line.match(QUOTED_KEY);
    if (!m) return line;
    const key = m[2];
    if (!IDENT.test(key)) return line;
    n++;
    return m[1] + key + ":" + line.slice(m[0].length);
  });
  const target = process.argv[2 + files.length] || null;
  if (target) {
    fs.writeFileSync(target, out.join("\n"), "utf8");
    console.log(`${f}: 去引号 ${n} 处 -> ${target}`);
  } else {
    process.stdout.write(out.join("\n"));
  }
}

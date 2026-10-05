"""把 frontend/build 打包成后端嵌入的 assets.zip（带断言，防白屏）。

为什么需要这个脚本（README 里那条"注释"变命令）：
  zip 的条目名必须是 assets/build/...（statics.go 按 //go:embed assets.zip
  内的 assets/build/index.html 取文件）—— 从 build/ 直接 zip 拿不到这个前缀，
  普通 zip 命令做不到改前缀。前缀错了的表现是【白屏】，而构建会一路成功。

两条断言（不过就非零退出、且【不覆盖】已有 assets.zip）：
  ① 新鲜度链：assets.zip mtime >= build/index.html mtime，
     build/index.html mtime >= frontend 源码树（src/public，排除 node_modules/build）
     —— 防"新后端 + 旧前端"（这个项目里最贵的一类静默失败）
  ② 结构：zip 必须含 assets/build/index.html，
     且【零】个前缀错误条目（防白屏）

用法（任意工作目录均可，路径按脚本自身位置推导）：
    python scripts/repack_assets.py repack
"""
import json
import os
import shutil
import sys
import time
import zipfile

sys.stdout.reconfigure(encoding="utf-8")

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
FRONTEND = os.path.join(REPO, "frontend")
FRONTEND_BUILD = os.path.join(FRONTEND, "build")
ASSETS_ZIP = os.path.join(REPO, "backend", "application", "statics", "assets.zip")
ZIP_PREFIX = "assets/build/"

# 前端源码树的新鲜度水位：排除构建/依赖/版本库
SRC_EXCLUDE = {"node_modules", "build", ".git", ".yarn"}


def source_watermark():
    newest = 0.0
    newest_file = ""
    for dirpath, dirnames, filenames in os.walk(FRONTEND):
        dirnames[:] = [d for d in dirnames if d not in SRC_EXCLUDE]
        for n in filenames:
            p = os.path.join(dirpath, n)
            try:
                m = os.path.getmtime(p)
            except OSError:
                continue
            if m > newest:
                newest, newest_file = m, os.path.relpath(p, FRONTEND)
    return newest, newest_file


def _ts(t):
    return time.strftime("%Y-%m-%d %H:%M:%S", time.localtime(t))


def repack():
    print("=" * 68)
    print("repack assets.zip（断言式）")
    print("=" * 68)

    if not os.path.isdir(FRONTEND_BUILD) or not os.path.isfile(
        os.path.join(FRONTEND_BUILD, "index.html")
    ):
        print("❌ 前端 build 缺失（先在 frontend/ 完成构建）")
        return 1

    # ---- 断言 ①：新鲜度链 ----
    idx = os.path.join(FRONTEND_BUILD, "index.html")
    src_m, src_file = source_watermark()
    idx_m = os.path.getmtime(idx)
    print(f"  源码最新: {_ts(src_m)}  ({src_file})")
    print(f"  build/index.html: {_ts(idx_m)}")
    if idx_m < src_m:
        print(f"❌ 前端产物比源码旧 —— 会打出『新后端 + 旧前端』（先重跑前端构建）")
        return 1
    if os.path.exists(ASSETS_ZIP):
        zip_m = os.path.getmtime(ASSETS_ZIP)
        print(f"  assets.zip: {_ts(zip_m)}")
        if zip_m < idx_m:
            print("❌ assets.zip 比前端产物旧 —— 请重跑本脚本")
            return 1

    # ---- 打包（所有条目加前缀）----
    tmp = ASSETS_ZIP + ".tmp"
    n = 0
    with zipfile.ZipFile(tmp, "w", zipfile.ZIP_DEFLATED, compresslevel=9) as z:
        for dirpath, dirnames, filenames in os.walk(FRONTEND_BUILD):
            dirnames.sort()
            for name in sorted(filenames):
                full = os.path.join(dirpath, name)
                rel = os.path.relpath(full, FRONTEND_BUILD).replace("\\", "/")
                z.write(full, ZIP_PREFIX + rel)
                n += 1

    # ---- 断言 ②：结构 ----
    with zipfile.ZipFile(tmp) as z:
        names = z.namelist()
        has_index = "assets/build/index.html" in names
        bare = [x for x in names if not x.startswith(ZIP_PREFIX)]
        print(f"  写入 {n} 个条目")
        print(f"  含 assets/build/index.html: {'✅' if has_index else '❌'}")
        print(f"  前缀错误条目: {len(bare)} {'✅' if not bare else '❌ ' + str(bare[:5])}")
        if not has_index or bare:
            os.remove(tmp)
            print("❌ zip 结构不对，已放弃（原 assets.zip 未被改动）")
            return 1

    if os.path.exists(ASSETS_ZIP):
        shutil.copy2(ASSETS_ZIP, ASSETS_ZIP + ".bak")
    os.replace(tmp, ASSETS_ZIP)
    print(f"✅ assets.zip 已更新（{os.path.getsize(ASSETS_ZIP) / 1024 / 1024:.2f} MB）")
    return 0


if __name__ == "__main__":
    cmd = sys.argv[1] if len(sys.argv) > 1 else "repack"
    if cmd == "repack":
        sys.exit(repack())
    print(__doc__)
    sys.exit(2)

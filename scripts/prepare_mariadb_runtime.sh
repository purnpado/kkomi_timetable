#!/usr/bin/env bash
set -euo pipefail

# 꼬미시간 macOS / Windows 공용 무설치 포터블 MariaDB 런타임 구성 스크립트
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CACHE_DIR="$ROOT_DIR/build/cache"
DARWIN_RT="$ROOT_DIR/bin/darwin/runtime/mariadb"
WIN_RT="$ROOT_DIR/bin/windows/runtime/mariadb"

mkdir -p "$CACHE_DIR" "$DARWIN_RT" "$WIN_RT"

MARIADB_MAC_TGZ="$CACHE_DIR/mariadb114-macos-x86_64.tar.gz"
PCRE2_MAC_TGZ="$CACHE_DIR/pcre2-macos-x86_64.tar.gz"
OPENSSL_MAC_TGZ="$CACHE_DIR/openssl3-macos-x86_64.tar.gz"
MARIADB_WIN_ZIP="$CACHE_DIR/mariadb-11.4.3-winx64.zip"

ensure_file() {
  local dest="$1"
  local url="$2"
  local auth_header="${3:-}"
  if [ -s "$dest" ]; then
    return 0
  fi
  echo ">>> 캐시 파일 다운로드 중: $(basename "$dest")"
  if [ -n "$auth_header" ]; then
    curl -fsSL -H "$auth_header" "$url" -o "$dest"
  else
    curl -fsSL "$url" -o "$dest"
  fi
}

ensure_file "$MARIADB_MAC_TGZ" "https://ghcr.io/v2/homebrew/core/mariadb/11.4/blobs/sha256:1f8822a19b350d0d0df6454cd079e5e01f5d6f188e848a3ab2bdc2f56cb8d35e" "Authorization: Bearer QQ=="
ensure_file "$PCRE2_MAC_TGZ" "https://ghcr.io/v2/homebrew/core/pcre2/blobs/sha256:167ef6d2b6337706884e23ee902cfc2ff8faeb455f2e07d23233e3061268867c" "Authorization: Bearer QQ=="
ensure_file "$OPENSSL_MAC_TGZ" "https://ghcr.io/v2/homebrew/core/openssl/3/blobs/sha256:5477285c4ebec45713873ae4002affece39e427c5f1b655c6a3df49c6b90f924" "Authorization: Bearer QQ=="
ensure_file "$MARIADB_WIN_ZIP" "https://archive.mariadb.org/mariadb-11.4.3/winx64-packages/mariadb-11.4.3-winx64.zip"

WORK_DIR="/tmp/kkomi_mariadb_prep_$$"
trap 'chmod -R u+w "$WORK_DIR" 2>/dev/null || true; rm -rf "$WORK_DIR"' EXIT
mkdir -p "$WORK_DIR/mac"

# 1. macOS 포터블 MariaDB 런타임 조립 (bin/darwin/runtime/mariadb)
echo ">>> [1/2] macOS x86_64 포터블 MariaDB 11.4 런타임 구성 중..."
tar -xzf "$MARIADB_MAC_TGZ" -C "$WORK_DIR/mac"
tar -xzf "$PCRE2_MAC_TGZ" -C "$WORK_DIR/mac"
tar -xzf "$OPENSSL_MAC_TGZ" -C "$WORK_DIR/mac"
chmod -R u+w "$WORK_DIR/mac"

MARIADB_SRC="$(find "$WORK_DIR/mac/mariadb@11.4" -mindepth 1 -maxdepth 1 -type d | head -n 1)"
PCRE2_SRC="$(find "$WORK_DIR/mac/pcre2" -mindepth 1 -maxdepth 1 -type d | head -n 1)"
OPENSSL_SRC="$(find "$WORK_DIR/mac/openssl@3" -mindepth 1 -maxdepth 1 -type d | head -n 1)"

chmod -R u+w "$DARWIN_RT" 2>/dev/null || true
rm -rf "$DARWIN_RT/bin" "$DARWIN_RT/lib" "$DARWIN_RT/share"
mkdir -p "$DARWIN_RT/bin" "$DARWIN_RT/lib/plugin" "$DARWIN_RT/share"

for bin_name in mariadbd mariadb-install-db my_print_defaults resolveip mariadb-admin; do
  if [ -f "$MARIADB_SRC/bin/$bin_name" ]; then
    cp "$MARIADB_SRC/bin/$bin_name" "$DARWIN_RT/bin/$bin_name"
    chmod 755 "$DARWIN_RT/bin/$bin_name"
  fi
done

cp "$PCRE2_SRC/lib/libpcre2-8.0.dylib" "$DARWIN_RT/lib/libpcre2-8.0.dylib"
cp "$OPENSSL_SRC/lib/libssl.3.dylib" "$DARWIN_RT/lib/libssl.3.dylib"
cp "$OPENSSL_SRC/lib/libcrypto.3.dylib" "$DARWIN_RT/lib/libcrypto.3.dylib"
chmod 755 "$DARWIN_RT/lib/"*.dylib

if [ -f "$MARIADB_SRC/lib/plugin/file_key_management.so" ]; then
  cp "$MARIADB_SRC/lib/plugin/file_key_management.so" "$DARWIN_RT/lib/plugin/"
  chmod 755 "$DARWIN_RT/lib/plugin/file_key_management.so"
fi

cp -R "$MARIADB_SRC/share/mysql" "$DARWIN_RT/share/mysql"
chmod -R u+w "$DARWIN_RT/share"

# mariadb-install-db 스크립트 내 고정 Homebrew 경로 제거 (실행 위치 독립성 확보)
python3 - "$DARWIN_RT/bin/mariadb-install-db" << 'PYEOF'
import re, sys
p = sys.argv[1]
txt = open(p, "r", encoding="utf-8").read()
txt = re.sub(r'^basedir="[^"]*"', 'basedir=""', txt, flags=re.M)
txt = re.sub(r'^ldata="[^"]*"', 'ldata="./data"', txt, flags=re.M)
txt = txt.replace("@@HOMEBREW_PREFIX@@", "")
open(p, "w", encoding="utf-8").write(txt)
PYEOF

# Mach-O 동적 라이브러리 경로(@rpath / @loader_path) 재배치
install_name_tool -id "@rpath/libcrypto.3.dylib" "$DARWIN_RT/lib/libcrypto.3.dylib"
install_name_tool -id "@rpath/libpcre2-8.0.dylib" "$DARWIN_RT/lib/libpcre2-8.0.dylib"
install_name_tool -id "@rpath/libssl.3.dylib" "$DARWIN_RT/lib/libssl.3.dylib"

python3 - "$DARWIN_RT" << 'PYEOF'
import os, subprocess, sys
rt = sys.argv[1]

def otool_deps(path):
    out = subprocess.check_output(["otool", "-L", path], text=True)
    return [line.strip().split(" ")[0] for line in out.splitlines()[1:] if line.strip()]

ssl_lib = os.path.join(rt, "lib", "libssl.3.dylib")
for dep in otool_deps(ssl_lib):
    if dep.endswith("libcrypto.3.dylib"):
        subprocess.check_call(["install_name_tool", "-change", dep, "@loader_path/libcrypto.3.dylib", ssl_lib])

for bname in ["mariadbd", "my_print_defaults", "resolveip", "mariadb-admin"]:
    bp = os.path.join(rt, "bin", bname)
    if not os.path.exists(bp):
        continue
    subprocess.run(["install_name_tool", "-add_rpath", "@loader_path/../lib", bp], check=False, stderr=subprocess.DEVNULL)
    for dep in otool_deps(bp):
        if "@@HOMEBREW_" in dep or "/opt/" in dep or "/Cellar/" in dep:
            base = os.path.basename(dep)
            if os.path.exists(os.path.join(rt, "lib", base)):
                subprocess.check_call(["install_name_tool", "-change", dep, f"@rpath/{base}", bp])
PYEOF

for f in "$DARWIN_RT/lib/"*.dylib "$DARWIN_RT/lib/plugin/"*.so "$DARWIN_RT/bin/mariadbd" "$DARWIN_RT/bin/my_print_defaults" "$DARWIN_RT/bin/resolveip" "$DARWIN_RT/bin/mariadb-admin"; do
  [ -f "$f" ] && codesign --force --sign - "$f" >/dev/null 2>&1 || true
done

echo ">>> macOS MariaDB 동작 확인: $("$DARWIN_RT/bin/mariadbd" --version)"

# 2. Windows x64 포터블 MariaDB 최소 런타임 배치 (bin/windows/runtime/mariadb)
echo ">>> [2/2] Windows x64 포터블 MariaDB 11.4 런타임 구성 중..."
chmod -R u+w "$WIN_RT" 2>/dev/null || true
rm -rf "$WIN_RT/bin" "$WIN_RT/lib" "$WIN_RT/share"
mkdir -p "$WIN_RT/bin" "$WIN_RT/lib/plugin" "$WIN_RT/share"

python3 - "$MARIADB_WIN_ZIP" "$WIN_RT" << 'PYEOF'
import os, sys, zipfile

zip_path, dest = sys.argv[1], sys.argv[2]
want_bin = {
    "mariadbd.exe",
    "mysqld.exe",
    "mariadb-install-db.exe",
    "mysql_install_db.exe",
    "my_print_defaults.exe",
    "mariadb-admin.exe",
    "server.dll",
}
want_plugins = {"file_key_management.dll"}

with zipfile.ZipFile(zip_path) as zf:
    for info in zf.infolist():
        if info.is_dir():
            continue
        parts = info.filename.split("/", 1)
        if len(parts) < 2:
            continue
        rel = parts[1]
        base = os.path.basename(rel)
        target = None
        if rel.startswith("bin/") and (base in want_bin or base.lower().endswith(".dll")):
            target = os.path.join(dest, rel)
        elif rel.startswith("lib/plugin/") and base in want_plugins:
            target = os.path.join(dest, rel)
        elif rel.startswith("share/"):
            # 언어 파일은 english, korean 및 charsets/SQL 스크립트만 경량 복사
            sub = rel[len("share/"):]
            if "/" not in sub or sub.startswith(("english/", "korean/", "charsets/")):
                target = os.path.join(dest, rel)
        if target:
            os.makedirs(os.path.dirname(target), exist_ok=True)
            with zf.open(info) as src, open(target, "wb") as out:
                out.write(src.read())
PYEOF

echo "✅ 포터블 MariaDB 런타임 준비 완료 (macOS + Windows)"

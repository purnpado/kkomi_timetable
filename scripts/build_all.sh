#!/usr/bin/env bash
set -euo pipefail

export PATH="/Users/kkomi/homebrew/bin:/Users/kkomi/go/bin:/opt/homebrew/bin:/usr/local/bin:$PATH"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${VERSION:-0.1.01}"
DO_RELEASE="${RELEASE:-0}"
if [ "${1:-}" = "--release" ]; then
  DO_RELEASE="1"
fi

cd "$ROOT_DIR"

echo "============================================================"
echo "🐾 꼬미시간 v${VERSION} 로컬 검증 및 멀티 OS 빌드 (릴리즈: ${DO_RELEASE})"
echo "============================================================"

# 0. 선(先) 포터블 MariaDB 런타임 준비 및 정적 분석·테스트 통과 강제 게이트
echo ">>> [0/5] 포터블 MariaDB 런타임 확인, 정적 분석(go vet) 및 전체 단위·E2E 테스트 검증..."
if [ ! -x "bin/darwin/runtime/mariadb/bin/mariadbd" ] || [ ! -f "bin/windows/runtime/mariadb/bin/mariadbd.exe" ]; then
  ./scripts/prepare_mariadb_runtime.sh
fi
go vet ./...
go test -v ./...

# 1. 프론트엔드 프로덕션 빌드 및 서버 내장 웹 자산 동기화
if [ "${SKIP_FRONTEND_BUILD:-0}" != "1" ]; then
  echo ">>> [1/5] React 프론트엔드 프로덕션 빌드..."
  (cd client/frontend && npm run build)
fi
rm -rf server/cmd/server/webdist/*
cp -R client/frontend/dist/* server/cmd/server/webdist/

# 2. 아이콘 및 Windows .syso 리소스 생성
echo ">>> [2/5] Wails v3 아이콘(.icns/.ico) 및 Windows 리소스(.syso) 생성..."
mkdir -p build/darwin build/windows bin/darwin bin/windows dist/release
wails3 generate icons \
  -input build/appicon.png \
  -macfilename build/darwin/icons.icns \
  -windowsfilename build/windows/icon.ico

wails3 generate syso \
  -arch amd64 \
  -icon build/windows/icon.ico \
  -manifest build/windows/wails.exe.manifest \
  -info build/windows/info.json \
  -out client/wails_windows_amd64.syso

wails3 generate syso \
  -arch amd64 \
  -icon build/windows/icon.ico \
  -manifest build/windows/wails.exe.manifest \
  -info build/windows/info.server.json \
  -out server/cmd/server/wails_windows_amd64.syso

# 3. macOS 빌드 (개발/검증용: 꼬미시간서버.app, 꼬미시간.app)
echo ">>> [3/5] macOS 바이너리 및 .app 번들 빌드..."
export CGO_CFLAGS="-mmacosx-version-min=12.0"
export CGO_LDFLAGS="-mmacosx-version-min=12.0"
export MACOSX_DEPLOYMENT_TARGET="12.0"

go build -trimpath -ldflags="-w -s" -o "bin/darwin/꼬미시간서버" ./server/cmd/server
go build -tags production -trimpath -ldflags="-w -s" -o "bin/darwin/꼬미시간" ./client

# macOS codesign 호환을 위해 .app 내부 실행 바이너리는 영문 식별자로 유지
rm -rf "bin/darwin/꼬미시간.app" "bin/darwin/꼬미시간서버.app"

mkdir -p "bin/darwin/꼬미시간.app/Contents/MacOS" "bin/darwin/꼬미시간.app/Contents/Resources"
cp "bin/darwin/꼬미시간" "bin/darwin/꼬미시간.app/Contents/MacOS/kkomi-timetable"
cp build/darwin/Info.plist "bin/darwin/꼬미시간.app/Contents/Info.plist"
cp build/darwin/icons.icns "bin/darwin/꼬미시간.app/Contents/Resources/icons.icns"
xattr -cr "bin/darwin/꼬미시간.app" || true
codesign --force --deep --sign - "bin/darwin/꼬미시간.app"

mkdir -p "bin/darwin/꼬미시간서버.app/Contents/MacOS" "bin/darwin/꼬미시간서버.app/Contents/Resources"
cp "bin/darwin/꼬미시간서버" "bin/darwin/꼬미시간서버.app/Contents/MacOS/kkomi-timetable-server"
cp build/darwin/Info.server.plist "bin/darwin/꼬미시간서버.app/Contents/Info.plist"
cp build/darwin/icons.icns "bin/darwin/꼬미시간서버.app/Contents/Resources/icons.icns"
xattr -cr "bin/darwin/꼬미시간서버.app" || true
codesign --force --deep --sign - "bin/darwin/꼬미시간서버.app"

# 4. Windows 교차 빌드 (서버는 CMD 콘솔창 출력, 클라이언트는 -H windowsgui 네이티브 GUI)
echo ">>> [4/5] Windows x86_64 실행 파일 크로스 컴파일..."
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags="-w -s" -o "bin/windows/꼬미시간서버.exe" ./server/cmd/server
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -tags production -trimpath -buildvcs=false -ldflags="-w -s -H windowsgui" -o "bin/windows/꼬미시간.exe" ./client

rm -f client/*.syso server/cmd/server/*.syso

if [ "$DO_RELEASE" != "1" ]; then
  echo "============================================================"
  echo "✅ macOS & Windows 테스트 빌드 완료! (릴리즈 패키징 생략됨)"
  echo "   릴리즈 생성 시: RELEASE=1 ./scripts/build_all.sh 또는 ./scripts/build_all.sh --release"
  ls -lh bin/darwin/ bin/windows/
  echo "============================================================"
  exit 0
fi

# 5. Windows 전용 릴리즈 패키지 생성 (dist/release/) — 사용자 승인 후 실행
echo ">>> [5/5] Windows 전용 릴리즈 패키징 (dist/release/)..."
mkdir -p dist/release
rm -rf dist/release/*
STAGE_DIR="/tmp/kkomi_release_stage_$$"
rm -rf "$STAGE_DIR"
mkdir -p "$STAGE_DIR/1_교무실_공용서버/runtime/mariadb" "$STAGE_DIR/1_교무실_공용서버/templates" "$STAGE_DIR/2_교사용_클라이언트"

cp "bin/windows/꼬미시간서버.exe" "$STAGE_DIR/1_교무실_공용서버/꼬미시간서버.exe"
if [ -d "bin/windows/runtime/mariadb/bin" ]; then
  cp -R bin/windows/runtime/mariadb/bin "$STAGE_DIR/1_교무실_공용서버/runtime/mariadb/"
  [ -d "bin/windows/runtime/mariadb/lib" ] && cp -R bin/windows/runtime/mariadb/lib "$STAGE_DIR/1_교무실_공용서버/runtime/mariadb/"
  [ -d "bin/windows/runtime/mariadb/share" ] && cp -R bin/windows/runtime/mariadb/share "$STAGE_DIR/1_교무실_공용서버/runtime/mariadb/"
fi
cp "scripts/setup_portable_mariadb.ps1" "$STAGE_DIR/1_교무실_공용서버/runtime/mariadb/README_포터블_MariaDB_안내_및_자동설치.ps1"
cp "bin/windows/꼬미시간.exe" "$STAGE_DIR/2_교사용_클라이언트/꼬미시간.exe"

# 기본 .hwpx 템플릿 생성 후 릴리즈에 포함
KKOMI_BASE_DIR="$STAGE_DIR/1_교무실_공용서버" "bin/darwin/꼬미시간서버" --init-template

cat << TXT > "$STAGE_DIR/사용설명서_빠른시작.txt"
[꼬미시간 v${VERSION} Windows 포터블 빠른 시작 안내]

1. 교무실 공용 PC (메인 서버)
   - "1_교무실_공용서버" 폴더를 공용 PC 원하는 위치(예: D:\꼬미시간서버)에 통째로 복사하세요.
   - 포터블 MariaDB(runtime\mariadb)가 기본 포함되어 별도 DB 설치 없이 꼬미시간서버.exe 실행 시 127.0.0.1:13306으로 자동 구동됩니다.
   - 꼬미시간서버.exe 를 실행하면 콘솔에 업무망 고정 IP 접속 주소(예: 192.168.x.x:18090)가 안내됩니다.

2. 교사 개인 PC (클라이언트)
   - "2_교사용_클라이언트\꼬미시간.exe" 를 복사하여 실행하세요.
   - 첫 화면에서 서버 IP(예: http://192.168.x.x:18090)를 입력하고 로그인하세요. (초기 체험 비밀번호: 1234)
TXT

cp "bin/windows/꼬미시간서버.exe" "dist/release/꼬미시간서버.exe"
cp "bin/windows/꼬미시간.exe" "dist/release/꼬미시간.exe"
cp "bin/windows/꼬미시간서버.exe" "dist/release/kkomi-timetable-server.exe"
cp "bin/windows/꼬미시간.exe" "dist/release/kkomi-timetable-client.exe"

python3 - <<PYEOF
import os, unicodedata, zipfile

stage = "$STAGE_DIR"
out_zip = "$ROOT_DIR/dist/release/꼬미시간_v${VERSION}_windows_portable.zip"
with zipfile.ZipFile(out_zip, "w", compression=zipfile.ZIP_DEFLATED) as zf:
    for root, dirs, files in os.walk(stage):
        for name in sorted(files):
            full = os.path.join(root, name)
            rel = unicodedata.normalize("NFC", os.path.relpath(full, stage)).replace(os.sep, "/")
            zi = zipfile.ZipInfo(rel)
            zi.flag_bits |= 0x800  # UTF-8 파일명 플래그 명시 (Windows 한글 호환)
            zi.compress_type = zipfile.ZIP_DEFLATED
            with open(full, "rb") as f:
                zf.writestr(zi, f.read())
PYEOF
cp "$ROOT_DIR/dist/release/꼬미시간_v${VERSION}_windows_portable.zip" "$ROOT_DIR/dist/release/kkomi-timetable_v${VERSION}.zip"
rm -rf "$STAGE_DIR"

echo "============================================================"
echo "✅ 빌드 및 Windows 릴리즈 완료!"
ls -lh bin/darwin/ bin/windows/ dist/release/
echo "============================================================"

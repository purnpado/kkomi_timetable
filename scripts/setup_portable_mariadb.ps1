# Windows 교무실 공용 PC용 포터블 MariaDB 자동 설치 및 배치 스크립트
# 실행 위치: 1_교무실_공용서버\runtime\mariadb\ 폴더 내부 또는 1_교무실_공용서버\ 상위 폴더
$ErrorActionPreference = "Stop"
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$targetDir = $scriptDir
if ((Split-Path -Leaf $scriptDir) -ne "mariadb") {
    $targetDir = Join-Path $scriptDir "runtime\mariadb"
}
New-Item -ItemType Directory -Force -Path $targetDir | Out-Null

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host "  꼬미시간 포터블 MariaDB(Windows x64) 런타임 설치 도우미" -ForegroundColor Green
Write-Host "  설치 경로: $targetDir" -ForegroundColor Yellow
Write-Host "==========================================================" -ForegroundColor Cyan

# MariaDB 11.4 LTS Windows x64 ZIP 다운로드 (업무망 PC에서 인터넷이 안 될 경우,
# 인터넷 가능한 PC에서 미리 받아 runtime\mariadb\bin 폴더만 복사해도 동일하게 작동합니다.)
$mariaVer = "11.4.3"
$zipUrl = "https://archive.mariadb.org/mariadb-$mariaVer/winx64-packages/mariadb-$mariaVer-winx64.zip"
$zipPath = Join-Path $env:TEMP "mariadb-$mariaVer-winx64.zip"
$extractTmp = Join-Path $env:TEMP "mariadb-extract"

try {
    Write-Host ">>> MariaDB $mariaVer 포터블 패키지 다운로드 중..." -ForegroundColor Cyan
    Invoke-WebRequest -Uri $zipUrl -OutFile $zipPath -UseBasicParsing

    if (Test-Path $extractTmp) { Remove-Item $extractTmp -Recurse -Force }
    Expand-Archive -Path $zipPath -DestinationPath $extractTmp -Force

    $rootFolder = Get-ChildItem -Path $extractTmp -Directory | Select-Object -First 1
    if ($null -eq $rootFolder) { throw "압축 해제 폴더를 찾을 수 없습니다." }

    Copy-Item -Path (Join-Path $rootFolder.FullName "bin") -Destination $targetDir -Recurse -Force
    if (Test-Path (Join-Path $rootFolder.FullName "lib")) {
        Copy-Item -Path (Join-Path $rootFolder.FullName "lib") -Destination $targetDir -Recurse -Force
    }
    if (Test-Path (Join-Path $rootFolder.FullName "share")) {
        Copy-Item -Path (Join-Path $rootFolder.FullName "share") -Destination $targetDir -Recurse -Force
    }

    Write-Host ">>> 포터블 MariaDB 배치 완료! (mariadbd.exe 준비됨)" -ForegroundColor Green
    Write-Host ">>> 이제 꼬미시간서버.exe를 실행하면 127.0.0.1:13306 로컬 전용 및 AES-256 TDE가 자동 구동됩니다." -ForegroundColor Yellow
} catch {
    Write-Host ">>> 자동 다운로드 실패 (폐쇄망 환경일 수 있습니다): $_" -ForegroundColor Yellow
    Write-Host ">>> 안내: 꼬미시간서버.exe는 포터블 MariaDB 전용으로 구동됩니다." -ForegroundColor Cyan
    Write-Host ">>>       외부 PC에서 mariadb-winx64.zip의 bin/lib/share 폴더를" -ForegroundColor Cyan
    Write-Host ">>>       $targetDir 폴더에 복사해 넣으시면 자동 인식됩니다." -ForegroundColor Cyan
}

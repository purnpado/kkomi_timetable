# 꼬미시간 (KkomiTimetable) 에이전트 및 개발 작업 수칙

이 문서는 저장소 작업을 이어받는 모든 개발자와 AI 에이전트가 반드시 준수해야 하는 절대 수칙입니다.

## 1. 핵심 개발 원칙 (추측 구현 절대 금지)
* **선(先) 로컬 테스트 후(後) 빌드:** 기능을 추측으로 작성하고 끝내지 않습니다. 코드 변경 후에는 반드시 로컬에서 `go vet ./...`, `go test -v ./...`(단위 + API/WebSocket E2E 테스트), 프론트엔드 `npm run build`를 실제로 실행해 통과한 뒤에만 `./scripts/build_all.sh`로 `bin/` 빌드를 생성합니다.
* **릴리즈 보류 원칙:** 현재 목표 버전은 **`v0.1.0`**입니다. 사용자가 명시적으로 **"릴리즈 해달라"고 지시하기 전에는 절대 정식 릴리즈(`--release`, Gitea Releases 업로드, 릴리즈 태그 푸시)를 실행하지 않습니다.**

## 2. 5대 절대 개발 수칙
1. **100% 한국어 소통 및 기록:** 대화, 코드 주석, 문서, Git 커밋 메시지는 반드시 한국어로 작성합니다.
2. **경로 독립성 (`os.Executable()`):** `cwd`에 의존하지 않고 [paths.go](pkg/pathutil/paths.go)의 `os.Executable()` 기반 절대 경로와 UTF-8 NFC 결합을 사용해 한글 폴더명·공백 경로·바로가기 실행 시에도 안전하게 작동합니다.
3. **크로스 컴파일 (macOS 개발 · Windows 릴리즈):** 로컬 검증 후 `bin/darwin/`(macOS `.app`)과 `bin/windows/`(Windows `꼬미시간서버.exe`, `꼬미시간.exe`)를 함께 빌드하며, 릴리즈는 Windows 타겟만 배포합니다.
4. **Git 및 보안 최우선:** `.env`, `*.db`, `*.sqlite`, `*.key`, `data/`, `backups/`, `runtime/mariadb/data/` 등 민감 파일은 `.gitignore`로 원천 차단합니다.
5. **파일당 500줄 이하 & 문서 최신화:** 모든 소스 파일(`.go`, `.jsx`, `.js`, `.css`)은 500줄 이하를 유지하며(`TestSourceFileLineLimit500` 자동 검증), 작업마다 `README.md`와 이 문서를 최신 상태로 유지합니다.

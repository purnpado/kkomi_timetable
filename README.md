# 🐾 꼬미시간 (KkomiTimetable)

**중·고등학교 학사 시간표, 내·외부 시험감독 및 주중·월중 행사 통합 관리 솔루션**

* **원본 저장소 (Gitea):** `https://gitea.gguk.link/purnpadosori/kkomi_timetable.git`

> 본 프로젝트는 교사용 업무망(LAN) 폐쇄 환경에서 **무설치 포터블(Portable)**로 즉시 동작하도록 설계된 서버-클라이언트 데스크톱 통합 업무 솔루션입니다.

---

## 1. 배포 및 실행 파일명 규칙

* **Windows 릴리즈 전용 (`dist/release/`):**
  * **메인 서버:** `꼬미시간서버.exe` (교무실 공용 PC 1대에 배치. 가벼운 CMD 콘솔 서버로 실행되어 업무망 고정 IP/포트 및 DB 상태를 안내하며, 모든 기초 시간표·교사 등록·운영 작업은 클라이언트에서 마스터/담당 권한으로 원격 수행)
  * **교사 클라이언트:** `꼬미시간.exe` (교사 개인 PC에 배치, Wails v3 GUI, 트레이 상주, 260px 사이드 도킹 위젯, 마스터/담당자용 학년별·학급별 기초 시간표 작성 및 교사 관리 지원)
  * **통합 포터블 배포판:** `꼬미시간_v0.1.0_windows_portable.zip` (영문 호환본 `kkomi-timetable_v0.1.0.zip`)

* **macOS 개발/검증 빌드 (`bin/darwin/`):**
  * `꼬미시간서버`, `꼬미시간서버.app`, `꼬미시간`, `꼬미시간.app` (Apple `codesign` 호환을 위해 `.app/Contents/MacOS/` 내부 실행 파일은 `kkomi-timetable-server`, `kkomi-timetable` 영문 식별자 사용)

> **운영 원칙 (선 테스트 후 빌드 & 릴리즈 보류):**
> 1. **추측 구현 금지 ([AGENTS.md](AGENTS.md)):** 모든 변경은 로컬에서 `go vet ./...`, `go test -v ./...`(단위 + API/WebSocket E2E) 및 프론트엔드 빌드로 실제 검증을 통과한 뒤에만 `bin/` 빌드를 실행합니다.
> 2. **릴리즈 보류:** 현재 목표 버전은 **`v0.1.0`**이며, 사용자가 명시적으로 **"릴리즈 해달라"고 요청하기 전에는 절대 정식 릴리즈(Gitea Releases 업로드 및 릴리즈 태그 발행)를 수행하지 않고 빌드(`bin/`)까지만 실행**합니다.

---

## 2. 5대 절대 개발 수칙 (영구 보존 맥락)

작업 공간이 유실되어 저장소를 다시 Clone 하더라도 아래 5대 수칙을 즉시 이어받아 개발합니다.

1. **100% 한국어 소통 및 문서화:** 모든 대화, 주석, 문서, Git 커밋 메시지는 반드시 한국어로 작성합니다.
2. **절대 경로 독립성 (`os.Executable()`):** 프로그램이 상대경로나 바로가기로 실행되더라도 DB·백업·템플릿 경로를 잃어버리지 않도록 `cwd` 대신 `pkg/pathutil/paths.go`의 `os.Executable()` 기준 절대 경로(`ExecutableDir()`, `BaseDir()`)를 사용합니다. 또한 `golang.org/x/text/unicode/norm` NFC 정규화로 한글 폴더명·공백 경로에서도 안전합니다.
3. **크로스 컴파일 (macOS 개발 · Windows 릴리즈):** 개발 및 테스트는 macOS에서 진행하고 빌드는 macOS와 Windows 둘 다 수행하며, 최종 릴리즈는 Windows x86_64(`.exe`) 타겟(`CGO_ENABLED=0 GOOS=windows GOARCH=amd64`)으로 생성합니다.
4. **형상 관리(Git) 및 보안 최우선:** `.gitignore`를 통해 `*.db`, `*.sqlite`, `*.key`, `.env`, `data/`, `backups/`, `updates/`, `runtime/mariadb/data/` 등 민감 파일 유출을 원천 차단합니다.
5. **파일당 500줄 이하 & 문서 최신화:** 모든 소스 파일(`.go`, `.jsx`, `.js`, `.css`)은 **500줄 이하** 원칙을 엄격히 준수하며(`TestSourceFileLineLimit500`으로 자동 검증), 작업 후 이 `README.md`를 최신 상태로 유지합니다.

---

## 3. 핵심 아키텍처 및 도메인 기능

### 3.1. 포터블 MariaDB(TDE) 단일 DB 및 이벤트 기반 아키텍처 (EDA)
* **무설치 포터블 MariaDB(`mariadbd` / `mariadbd.exe` + AES-256 암호화):** macOS(`bin/darwin/runtime/mariadb/`) 및 Windows(`bin/windows/runtime/mariadb/`) 모두 포터블 MariaDB 바이너리가 번들되어 `127.0.0.1:13306` 로컬 루프백 전용으로 자동 초기화·기동·종료 제어됩니다. (행 단위 잠금 Row-level Locking 및 UTF8MB4 완벽 지원)
* **중앙 도메인 이벤트 버스 (`server/internal/eventbus`):** `TimetableApproved`, `ExamUnsealed`, `ExternalExamConfirmed`, `SchoolEventPublished`, `BellScheduleChanged` 이벤트를 발행하여 모듈 간 느슨한 결합(Zero-Coupling)과 WebSocket 실시간 브로드캐스트를 구현합니다.

### 3.2. 주요 업무 모듈
1. **업무분장 기반 6종 RBAC 권한 분리 (`server/internal/modules/auth`):**
   * `시스템 총괄(MASTER)`, `시간표 담당(TIMETABLE_ADMIN)`, `교내감독 담당(INTERNAL_EXAM_ADMIN)`, `외부감독 담당(EXTERNAL_EXAM_ADMIN)`, `행사 담당(EVENT_ADMIN)`, `일반 교사(TEACHER)`. 1인 다중 겸임 및 학년도 업무분장 인계(승계) 지원.
2. **시수표·교과교실제 기반 정규·일일 시간표 & 대기열(STAGING) → 최종 선택(Commit) (`timetable`):**
   * **학교 기본사항·요일별 기준시수·교사별 시수표 매트릭스(`curriculum.go`):** 1~3학년 학급수(가상반 포함), 요일별 기준 시수, 교사별·학급별 주당 배당 시수 일치성 검사(`일치/부족/초과`), 동시수업·연속수업·교과교실(특별실) 동시간대 중복 차단 기반 자동 시간표 편성.
   * **엑셀(`.xlsx`) 일괄 입출력(`xlsx_io.go`):** 교사 명렬표 및 컴시간 표준 형식 교사별 시수표 엑셀 다운로드·일괄 업로드 지원.
   * **다단계 연쇄 교환 & 교과교실 열람:** 60fps HTML5 Canvas(HiDPI, 드래그 고스트, 🟢 이동/맞교환 · 🟡 삼각(3단계)/사각(4단계) 순환 연쇄교환 1~4단계 상세 과정 시각화 · 🔴 충돌사유 신호등, PNG 클립보드 복사) 및 교과교실/특별실별 주간 시간표 조회.
   * 일반 교사 신청은 `STAGING`에 머물고, 시간표 담당교사가 Radix Dialog 전/후 비교 모달에서 **`[최종 선택(서버 반영)]`**을 눌러야만 마스터 DB에 반영됩니다. 나이스(NEIS) 입력 순서 정렬 및 과목코드 원클릭 복사 지원.
   * **동명이인 구분 간편 로그인(`LoginView.jsx`):** 서버의 교사/과목 디렉토리를 연동해 `담당과목 선택 → 교사 선택 → 비밀번호` 또는 `과목·성명·비밀번호` 직접 입력으로 동명이인을 정확히 구분하여 로그인합니다.
3. **교내 지필고사 시험감독 배정 & 보안 봉인 (`internal_exam`):**
   * **담임교사 자기 반 감독 원천 제외** (상피제에 따른 교사 자녀 학년 배제는 제외).
   * **담당 과목 시험 교시 처리:** 해당 교시 시험 과목 교사는 고사장(정·부감독)에서 100% 배제하고, 기본 설정(`CORRIDOR_FIRST`)으로 **해당 학년 층 복도감독으로 최우선 배치**합니다. (`평가본부 대기` 선택 가능)
   * 과목·교시별 **1인 단독 / 2인 정·부감독** 선택 및 **학부모 명예감독 On/Off** (활성화 시 자녀 학년/반 배제).
   * **사전 봉인 & 당일 아침 일괄 공개:** 봉인(`IsSealed`) 중에는 서버 API 응답 레벨에서 고사장을 `🔒 당일 08:20 공개`로 마스킹하여 패킷 감청까지 차단하며, `08:20` 스케줄러 또는 담당교사의 **`[오늘 시험 고사장 발표하기]`** 클릭 시 전 교원 PC에 일괄 공개됩니다. 당일 아침 결원 시 비담임·공강 교사 원클릭 긴급 대타 추천을 제공합니다.
4. **외부 시험감독 지원 및 다단계 차출 조합기 (`external_exam`):**
   * **계좌번호·은행명·수당 정산 기능 전면 제거** (금융정보 노출 걱정 없는 원클릭 지원).
   * 학교마다 다른 인사내규에 대응하는 **다단계 차출 우선순위 조합기**: 1순위(`자발적 지원자`) 고정 후, `방식 A(혼합형)`, `방식 B(순번/쿨타임형)`, `방식 C(최근 전입자 우선형)` 및 `직접 순서 조합(전입일·마지막 파견일·누적횟수·비담임·비부장)` 지원. 면제 대상자(임산부/수험생 자녀/원로교사) 후순위 제외 및 투명한 산정 근거 배지 표시.
   * 평일 시험 파견 확정 시 시간표 담당교사 결보강 대기열(`STAGING`)로 자동 이관됩니다.
5. **주중·월중 행사 취합 & 다중 출력 (`event_calendar`, `exporter`):**
   * `[단순안내]`, `[학년/전교 수업블록]`, `[특정교사 출장/결보강]`, `[시종시간 변경]` 4종 속성 연동.
   * 국가표준 한글(`.hwpx` ZIP/XML 치환), 엑셀(`.xlsx` `excelize/v2`), B4/A3 고대비 흑백 현황판 출력 지원.
6. **현장 밀착형 UI/UX (`client/frontend`):**
   * 시종시간표 연동 **수업 중 방해 금지 모드**(쉬는 시간 일괄 브리핑 큐), `Ctrl+K` 커맨드 팔레트, 우측 **260px 사이드 도킹 위젯**, 무중단 원격 자가 업데이트(`Self-Replace`).

---

## 4. 프로젝트 디렉토리 구조

```text
kkomi-timetable/
├── pkg/
│   ├── pathutil/paths.go               # os.Executable() 절대 경로 및 UTF-8 NFC 안전 결합
│   └── protocol/                       # 공용 DTO(models.go) 및 이벤트(events.go)
├── server/
│   ├── cmd/server/main.go              # 꼬미시간서버 진입점 및 내장 웹 에셋 서빙
│   └── internal/
│       ├── api/                        # REST API & WebSocket 라우터/핸들러
│       ├── database/                   # 포터블 MariaDB(TDE) 단일 엔진 제어, 암호화 백업, 초기화, 시드
│       ├── eventbus/                   # 중앙 도메인 이벤트 버스 (Pub/Sub)
│       ├── exporter/                   # .hwpx, .xlsx, 고대비 인쇄 HTML 생성기
│       ├── modules/                    # auth, timetable, internal_exam, external_exam, event_calendar
│       └── ws/                         # WebSocket 허브 및 무중단 자가 업데이트
├── client/
│   ├── main.go                         # 꼬미시간 Wails v3 앱, 트레이, 멀티 윈도우 제어
│   ├── internal/                       # bridge(데스크톱 제어), netclient(WS 자동재연결/자가패치)
│   └── frontend/                       # React + Radix UI + HTML5 Canvas (7대 화면 모듈)
├── build/                              # Wails v3 macOS/Windows 아이콘 및 매니페스트
└── scripts/
    ├── build_all.sh                    # macOS + Windows 동시 빌드 및 Windows 전용 릴리즈 패키징
    ├── prepare_mariadb_runtime.sh      # macOS/Windows 포터블 MariaDB 바이너리 런타임 자동 구성
    └── setup_portable_mariadb.ps1      # Windows 공용 PC 포터블 MariaDB 수동/재설치 도우미
```

---

## 5. 테스트, 빌드 및 실행 방법

### 5.1. 단위·통합 테스트 및 500줄 제한 검사
```bash
go test -v ./...
```

### 5.2. macOS · Windows 동시 빌드 및 테스트 실행
```bash
# 릴리즈 없이 macOS / Windows 테스트 빌드만 수행 (기본값)
./scripts/build_all.sh

# 사용자가 릴리즈를 명시적으로 요청했을 때만 Windows 전용 릴리즈 패키지 생성
./scripts/build_all.sh --release
```
* **테스트 빌드 산출물 (`bin/`):**
  * macOS: `bin/darwin/꼬미시간서버.app`, `bin/darwin/꼬미시간.app`
  * Windows: `bin/windows/꼬미시간서버.exe`, `bin/windows/꼬미시간.exe`
* **로컬/현장 테스트 시 파일 이동 안내:**
  * 본 프로그램은 `os.Executable()` 기준 절대 경로를 사용하는 **완전 포터블** 구조이므로, `bin/` 폴더 안의 실행 파일(`꼬미시간서버.exe`, `꼬미시간.exe` 또는 `꼬미시간서버.app`, `꼬미시간.app`)을 **다른 테스트 폴더(예: `바탕화면\테스트서버`, `바탕화면\테스트클라이언트`)로 그대로 복사하여 실행**하면 소스 폴더를 오염시키지 않고 깔끔하게 테스트할 수 있습니다. (단, macOS에서 `꼬미시간서버.app` / `꼬미시간.app`은 번들이므로 내부 파일만 빼지 말고 `.app` 폴더 전체를 복사해야 합니다.)
* **Windows 정식 릴리즈 산출물 (`dist/release/`, `--release` 실행 시 생성):**
  * `dist/release/꼬미시간서버.exe`, `dist/release/꼬미시간.exe`, `dist/release/꼬미시간_v0.1.0_windows_portable.zip`

### 5.3. 빠른 체험 계정 (초기 비밀번호: `1234`, 최초 로그인 시 즉시 변경 가능)
| 아이디 | 이름 | 담당 권한 (RBAC) |
| :--- | :--- | :--- |
| `admin` | 김교무 | 시스템 총괄 (마스터) + 전체 권한 |
| `timetable` | 이수업 | 시간표 담당교사 (수업계) |
| `internal` | 박평가 | 교내 시험감독 담당교사 (평가계) |
| `external` | 최외부 | 외부 시험감독 담당교사 (대외계) |
| `event` | 정행사 | 행사 담당교사 (행사계) |
| `t06` | 강국어 | 일반 교사 (1-2 담임) |

---

## 6. 현재 개발 상태
* [x] **1단계:** 프로젝트 초기화, 절대경로 유틸(`os.Executable()`), 공용 프로토콜 정의
* [x] **2단계:** 포터블 MariaDB 제어기 + AES-256 백업 + 이벤트 버스 + WebSocket 허브 + 자가 업데이트 구현
* [x] **3단계:** 5대 도메인 모듈(시간표, 교내감독 복도우선/봉인, 외부감독 다단계 차출, 행사 연동) 및 `.hwpx`/`.xlsx` 출력 구현
* [x] **4단계:** Wails v3 트레이/260px 사이드 위젯 + React/Radix/HTML5 Canvas 7대 화면 완성
* [x] **5단계:** 500줄 제한 전수 검사 및 도메인 통합 테스트 통과
* [x] **6단계:** macOS/Windows 동시 빌드 및 Windows 전용 릴리즈 패키지 생성

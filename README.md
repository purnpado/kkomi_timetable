# 🐾 꼬미시간 (KkomiTimetable)

**중·고등학교 학사 시간표, 내·외부 시험감독 및 주중·월중 행사 통합 관리 솔루션**

* **원본 저장소 (Gitea):** `https://gitea.gguk.link/purnpadosori/kkomi_timetable.git`
* **공개 릴리즈 · 자동 업데이트 저장소 (GitHub):** `https://github.com/purnpado/kkomi_timetable.git`

> 본 프로젝트는 교사용 업무망(LAN) 폐쇄 환경에서 **무설치 포터블(Portable)**로 즉시 동작하도록 설계된 서버-클라이언트 데스크톱 통합 업무 솔루션입니다.

---

## 1. 배포 및 실행 파일명 규칙

* **Windows 릴리즈 전용 (`dist/release/`):**
  * **메인 서버:** `꼬미시간서버.exe` (교무실 공용 PC 1대에 배치. 가벼운 CMD 콘솔 서버로 실행되어 업무망 고정 IP/포트 및 DB 상태를 안내하며, 모든 기초 시간표·교사 등록·운영 작업은 클라이언트에서 마스터/담당 권한으로 원격 수행)
  * **교사 클라이언트:** `꼬미시간.exe` (교사 개인 PC에 배치, Wails v3 GUI, 트레이 상주, 260px 사이드 도킹 위젯, 마스터/담당자용 학년별·학급별 기초 시간표 작성 및 교사 관리 지원)
  * **통합 포터블 배포판:** `꼬미시간_v0.1.01_windows_portable.zip` (영문 호환본 `kkomi-timetable_v0.1.01.zip`)

* **macOS 개발/검증 빌드 (`bin/darwin/`):**
  * `꼬미시간서버`, `꼬미시간서버.app`, `꼬미시간`, `꼬미시간.app` (Apple `codesign` 호환을 위해 `.app/Contents/MacOS/` 내부 실행 파일은 `kkomi-timetable-server`, `kkomi-timetable` 영문 식별자 사용)

> **운영 원칙 (선 테스트 후 빌드 & GitHub Releases 기반 자동 업데이트):**
> 1. **추측 구현 금지 ([AGENTS.md](AGENTS.md)):** 모든 변경은 로컬에서 `go vet ./...`, `go test -v ./...`(단위 + API/WebSocket E2E) 및 프론트엔드 빌드로 실제 검증을 통과한 뒤에만 `bin/` 및 릴리즈 빌드를 실행합니다.
> 2. **현재 정식 릴리즈 버전 (`v0.1.01`):** `시스템·초기화·권한` 화면 또는 클라이언트 패치 기능에서 **GitHub Releases(`purnpado/kkomi_timetable`)** 최신 버전을 조회하고 서버·클라이언트를 원클릭으로 자동 업데이트할 수 있습니다.

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
* **중앙 도메인 이벤트 버스 (`server/internal/eventbus`):** `TimetableApproved`, `TimetableCellsUpdated`, `TeacherConstraintUpdated`, `ExamUnsealed`, `ExternalExamConfirmed`, `SchoolEventPublished`, `BellScheduleChanged` 이벤트를 발행하여 모듈 간 느슨한 결합(Zero-Coupling)과 WebSocket 실시간 브로드캐스트를 구현합니다.

### 3.2. 주요 업무 모듈
1. **업무분장 기반 6종 RBAC 권한 분리 (`server/internal/modules/auth`):**
   * `최고관리자(MASTER)`, `시간표 담당(TIMETABLE_ADMIN)`, `교내감독 담당(INTERNAL_EXAM_ADMIN)`, `외부감독 담당(EXTERNAL_EXAM_ADMIN)`, `행사 담당(EVENT_ADMIN)`, `일반 교사(TEACHER)`. 1인 다중 겸임 및 학년도 업무분장 인계(승계) 지원.
   * 서버 최초 기동·초기화용 기본 `admin`(`최고관리자`) 계정은 수업 및 내·외부 시험감독 자동 배정에서 제외되며 삭제·권한해제로부터 보호됩니다. 실제 담당교사에게도 `최고관리자` 권한을 부여·겸임·인계할 수 있습니다.
2. **시수표·교과교실제 기반 정규·일일 시간표 & 대기열(STAGING) → 최종 선택(Commit) (`timetable`):**
   * **학교 기본사항·요일별 기준시수·교사별 시수표 매트릭스(`curriculum.go`):** 1~3학년 학급수(가상반 포함), 요일별 기준 시수, 교사별·학급별 주당 배당 시수 일치성 검사(`일치/부족/초과`), 동시수업·연속수업·교과교실(특별실) 동시간대 중복 차단 기반 자동 시간표 편성.
   * **엑셀(`.xlsx`) 일괄 입출력(`xlsx_io.go`):** 교사 명렬표 및 컴시간 표준 형식 교사별 시수표 엑셀 다운로드·일괄 업로드 지원.
   * **다주간(이번 주 · 다음 주 · 다다음 주) 시간표 & 주중 미출근 일괄 대책 (`multiweek.go`):** 정규 기본 시간표를 건드리지 않고 특정 일자/주차에만 적용되는 `CROSS_WEEK_SWAP`(주차 간 맞교환) 및 `DATE_ONLY` 일일 보강 오버레이를 지원하며, 주중 갑작스러운 병가·경조사·출장 시 다음주·다다음주 교환 후보와 당일 공강 교사 대타(동일교과·담임·누적횟수 순)를 자동 추천해 일괄 신청/확정할 수 있습니다.
   * **다단계 연쇄 교환 · 공강/기피 제약 · A/B/C 시나리오 & 20단계 Undo/Redo:** 60fps HTML5 Canvas(HiDPI, 주차별 날짜 헤더, 변동 배지, 드래그 고스트, 🟢 이동/맞교환 · 🟡 삼각(3단계)/사각(4단계) 순환 연쇄교환 1~4단계 상세 과정 시각화 · 🔴 충돌사유 신호등, PNG 클립보드 복사)와 교사별 고정 공강/기피 교시 제약(`constraints.go` · 부장회의·교과협의회 일괄 등록 포함), A/B/C안 품질 비교·채택(`scenarios.go`), 20단계 실행취소·다시실행(`history.go` · `Ctrl+Z`/`Ctrl+Y`) 및 교과교실/특별실별 주간 시간표 조회를 지원합니다.
   * 일반 교사 신청은 `STAGING`에 머물고, 시간표 담당교사가 Radix Dialog 전/후 비교 모달에서 **`[최종 선택(서버 반영)]`**을 눌러야만 마스터 DB에 반영됩니다. 나이스(NEIS) 입력 순서 정렬 및 과목코드 원클릭 복사 지원.
   * **동명이인 구분 간편 로그인(`LoginView.jsx`):** 서버의 교사/과목 디렉토리를 연동해 `담당과목 선택 → 교사 선택 → 비밀번호` 또는 `과목·성명·비밀번호` 직접 입력으로 동명이인을 정확히 구분하여 로그인합니다.
3. **교내 지필고사 시험감독 배정 & 보안 봉인 (`internal_exam`):**
   * **담임교사 자기 반 감독 원천 제외** (상피제에 따른 교사 자녀 학년 배제는 제외).
   * **담당 과목 시험 교시 처리:** 해당 교시 시험 과목 교사는 고사장(정·부감독)에서 100% 배제하고, 기본 설정(`CORRIDOR_FIRST`)으로 **해당 학년 층 복도감독으로 최우선 배치**합니다. (`평가본부 대기` 선택 가능)
   * 과목·교시별 **1인 단독(정감독) / 2인 정·부감독** 선택, **학부모 명예감독 On/Off** 및 **고교선택과목 분반 · 교과교실(특별실) · 선택적 별도고사실(독감·감염병 — 없을 시 미개설, 개설 시 일반교사 1인 순환감독)** 편성 지원.
   * **1년 전교원 감독 시수 균등 배정 · 다중일(3~5일) 통합 편성 & 연강 방지 (`assigner.go`, `fairness.go`, `multiday.go`):** 특수반 등 지정 제외 교원은 원천 분리하고(별도고사실은 일반 교사 순환 배정으로 연간 시수 합산), 3~5일 연속 지필고사 기간 통합 배정·동일 일자 연강(연속 교시) 감독 방지·1일 최대 감독 상한·일자별 순회/부재 필터링과 함께 연간 총 횟수 편차 및 정감독 횟수 편차가 0~1회 이내가 되도록 평탄화합니다.
   * **사전 봉인 & 일자별·일괄 공개 (`UnsealExamDate`):** 봉인(`IsSealed`) 중에는 서버 API 및 `.hwpx`/`.xlsx` 출력 시 고사장을 `🔒 당일 08:20 공개`로 마스킹하여 패킷 감청까지 차단하며, `08:20` 스케줄러 또는 담당교사의 일자별·전체 발표 클릭 시 해당 일자 고사장을 즉시 공개합니다. 당일 아침 결원 시 연강·일일상한 회피·비담임·공강 교사 원클릭 긴급 대타 추천을 제공합니다.
4. **외부 시험감독 지원 및 다단계 차출 조합기 (`external_exam`):**
   * **계좌번호·은행명·수당 정산 기능 전면 제거** (금융정보 노출 걱정 없는 원클릭 지원).
   * 학교마다 다른 인사내규에 대응하는 **다단계 차출 우선순위 조합기**: 1순위(`자발적 지원자`) 고정 후, `방식 A(혼합형)`, `방식 B(순번/쿨타임형)`, `방식 C(최근 전입자 우선형)` 및 `직접 순서 조합(전입일·마지막 파견일·누적횟수·비담임·비부장)` 지원. 면제 대상자(임산부/수험생 자녀/원로교사) 후순위 제외 및 투명한 산정 근거 배지 표시.
   * 평일 시험 파견 확정 시 시간표 담당교사 결보강 대기열(`STAGING`)로 자동 이관됩니다.
5. **주중·월중 행사 취합 · 월별/학기별 통합 정산 & 다중 출력 (`event_calendar`, `stats`, `exporter`):**
   * `[단순안내]`, `[학년/전교 수업블록]`, `[특정교사 출장/결보강]`, `[시종시간 변경]` 4종 속성 연동.
   * 월별·1학기·2학기·학년도·직접지정 기간별 결보강/수업교환/내외부 시험감독/행사 실적을 교원별로 집계하고(`stats/service.go`), 국가표준 한글(`.hwpx` 통합 정산 보고서·결보강 확인서·시험감독 배정표), 엑셀(`.xlsx` `excelize/v2` 4시트 통합 정산표·2시트 감독 시수표), B4/A3 고대비 흑백 현황판 출력(`settlement_exporter.go`)을 지원합니다.
6. **현장 밀착형 UI/UX & 깨끗한 초기 기동 (`client/frontend`, `database/reset.go`):**
   * 시종시간표 연동 **수업 중 방해 금지 모드**(쉬는 시간 일괄 브리핑 큐), `Ctrl+K` 커맨드 팔레트, 우측 **260px 사이드 도킹 위젯**, 무중단 원격 자가 업데이트(`Self-Replace`).
   * **Radix UI 7종 통합 및 디자인 마감:** `Dialog`, `Toast`, `Tabs`, `ContextMenu`(기초 시간표 우클릭 잠금·공강 지정), `AlertDialog`(초기화·복원·삭제 안전 확인), `Tooltip`(단축키 안내), `DropdownMenu`(문서 출력 메뉴)와 `prefers-reduced-motion` 접근성 지원.
   * **깨끗한 실학교 초기 기동 (`EnsureCleanInit`):** 기본 실행 시 시연용 가짜 데이터 없이 `admin` 최고관리자 계정과 기본 일과·학교설정만 초기화하고 대시보드 시작 가이드를 제공하며, 필요 시 `--seed-demo` 플래그로 예시 데이터를 별도 로드할 수 있습니다.

---

## 4. 프로젝트 디렉토리 구조

```text
kkomi-timetable/
├── ROADMAP.md                          # 구현 완료 · 진행 중 · 향후 구현 로드맵
├── pkg/
│   ├── pathutil/paths.go               # os.Executable() 절대 경로 및 UTF-8 NFC 안전 결합
│   └── protocol/                       # 공용 DTO(models.go) 및 이벤트(events.go)
├── server/
│   ├── cmd/server/main.go              # 꼬미시간서버 진입점, macOS 터미널/Windows CMD 안내 및 웹 서빙
│   └── internal/
│       ├── api/                        # REST API & WebSocket 라우터/핸들러
│       ├── database/                   # 포터블 MariaDB(TDE) 단일 엔진 제어, 자동 복원, 백업, 초기화, 시드
│       ├── eventbus/                   # 중앙 도메인 이벤트 버스 (Pub/Sub)
│       ├── exporter/                   # .hwpx, .xlsx, 고대비 인쇄 HTML 생성기
│       ├── modules/                    # auth, timetable, internal_exam, external_exam, event_calendar
│       └── ws/                         # WebSocket 허브, GitHub Releases 업데이트 및 OS 헤더 검증
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
  * `dist/release/꼬미시간서버.exe`, `dist/release/꼬미시간.exe`, `dist/release/꼬미시간_v0.1.01_windows_portable.zip`, `dist/release/kkomi-timetable_v0.1.01.zip`

### 5.3. 초기 최고관리자 접속 및 개발용 예시 계정 (`--seed-demo` 옵션 실행 시)
> * **기본 실사용 기동 (`꼬미시간서버`):** `admin` (`최고관리자`, 초기 비밀번호 `1234`) 1개 계정과 기본 일과·학교설정만 깨끗하게 초기화됩니다. 로그인 후 비밀번호를 변경하고 교사 명렬표 및 시수표를 등록하세요.
> * **예시 데이터 체험 기동 (`--seed-demo`):** 개발·확인 목적으로 아래 예시 계정이 함께 생성됩니다.

| 아이디 | 이름 | 담당 권한 (RBAC) |
| :--- | :--- | :--- |
| `admin` | 최고관리자 | 최고관리자 (시스템 전용 · 수업/시험감독 배정 제외) + 전체 권한 |
| `kim` | 김교무 | 교무부장 · 최고관리자 겸임 + 시간표 담당 (`국어`) |
| `timetable` | 이수업 | 시간표 담당교사 (수업계) |
| `internal` | 박평가 | 교내 시험감독 담당교사 (평가계) |
| `external` | 최외부 | 외부 시험감독 담당교사 (대외계) |
| `event` | 정행사 | 행사 담당교사 (행사계) |
| `t06` | 강국어 | 일반 교사 (1-2 담임) |

---

## 6. 현재 개발 상태 · 로드맵 ([ROADMAP.md](ROADMAP.md))

* **✅ 구현 완료 (`v0.1.01` ~ `v0.2.x` 1~5단계):**
  * [x] 1~6단계 코어 인프라, 포터블 MariaDB(TDE), 6종 RBAC(`최고관리자` 기본 계정 분리·권한 위임), 정규·다주간 시간표 & 1~4단계 연쇄교환, 교내·외부 시험감독, 행사 4종 연동, `.hwpx`/`.xlsx`/고대비 출력 완성
  * [x] GitHub Releases(`purnpado/kkomi_timetable`) `v0.1.01` 자동 업데이트(OS 바이너리 헤더 검증 포함), macOS/Windows 실행 안정화 및 1~2단계 보안·동시성·도메인·문서 정합성 강화 완료
  * [x] **`v0.2.x` 3단계 완료:** 교사별 절대 금지·회의 공강(부장/교과 일괄)·기피 교시 제약(`constraints.go`), 시간표 A/B/C안 품질 비교·채택(`scenarios.go`), 서버 측 20단계 실행취소/다시실행(`history.go`)
  * [x] **`v0.2.x` 4단계 완료:** 다중일(3~5일) 지필고사 통합 편성·연강 감독 방지·일자별 부분 공개(`multiday.go`), 월별/학기별 교무업무 통합 정산·결보강 확인서 `.hwpx`/`.xlsx` 출력(`stats/service.go`, `settlement_exporter.go`)
  * [x] **`v0.2.x` 5단계 완료:** 시작·관리 화면 테스트/시연 문구 및 데모 ID 정리, 깨끗한 초기 기동(`EnsureCleanInit` + `--seed-demo` 분리), Radix UI 7종 전면 적용(`Tabs`, `ContextMenu`, `AlertDialog`, `Tooltip`, `DropdownMenu`, `Dialog`, `Toast`), 모션·접근성 마감 완료
* **📋 앞으로 구현해야 할 항목 (상세 목록: [ROADMAP.md](ROADMAP.md)):**
  * [ ] Windows/macOS 실제 교무실 네트워크 현장 교차 구동 및 `v0.2.0` 릴리즈 배포 준비
  * [ ] `v0.3.x` ~ `v1.0` 중·장기: 고교학점제 학생 이동수업 시간표, 나이스 양방향 검증 확대, 교무실 TV 전광판 모드, macOS GitHub 릴리즈 자동 업데이트 및 신학년도 이관 마법사

# 🗺️ 꼬미시간 (KkomiTimetable) 개발 현황 및 로드맵

> 기준 버전: **`v0.1.01`** (최종 동기화: `2026-11-22` · `v0.2.x` 5단계 + DB 자가복구 + 차기 6단계 고도화 기획 확정 반영)  
> 이 문서는 **구현 완료 기능**, **현재 진행 중(현장 안정화) 작업**, 그리고 **앞으로 구현해야 할 기능 로드맵**을 한눈에 파악하기 위해 작성되었습니다.

---

## 1. ✅ 구현 완료 (`v0.1.01` 현재 동작 기능)

### 1.1. 코어 인프라 · 무설치 포터블 실행 · 보안 · 자동 업데이트
- **무설치 포터블 MariaDB 11.4 단일 DB + AES-256 TDE (`server/internal/database/`):**
  - macOS(`bin/darwin/runtime/mariadb/`) 및 Windows(`bin/windows/runtime/mariadb/`)에 MariaDB 바이너리(`mariadbd`, `mysqld.exe`, `mariadb-install-db.exe` 포함)를 번들하여 `127.0.0.1:13306` 로컬 루프백 전용으로 자동 초기화·기동·종료 제어
  - InnoDB 행 단위 잠금(Row-level Locking), `UTF8MB4` 한글 완벽 지원, 파일 키 관리 플러그인(`file_key_management`) AES-256 암호화 적용
  - 릴리즈 압축 파일 동봉 실행 시 `runtime/mariadb` 자동 압축 복원(`tryExtractPortableRuntimeFromZip`) 및 macOS 유실 런타임 자동 복구(`tryRunDarwinRuntimePrepareScript`)
  - 이전 실행의 비정상 종료 잔여 PID 및 `aria_log_control` 잠금 충돌(`error: 35`) 자동 정리(`cleanupStalePidProcess`)
- **경로 독립성 및 한글 폴더·공백 안전성 (`pkg/pathutil/paths.go`):**
  - `cwd` 대신 `os.Executable()` 기준 절대 경로(`ExecutableDir`, `BaseDir`) 및 UTF-8 NFC 정규화(`SafeJoin`) 적용
- **암호화 백업 · 3종 시스템 초기화 (`server/internal/database/reset.go`, `crypto.go`):**
  - 초기화 또는 업데이트 직전 `backups/` 폴더에 AES-256 암호화 백업 자동 생성
  - `ACADEMIC_TERM_RESET`(학기 초기화: 교사·일과표 유지), `FULL_FACTORY_RESET`(전체 완전 초기화: `admin`/`1234`만 유지), `DEMO_SEED_RESET`(시연용 샘플 복원) 3종 지원
- **GitHub Releases 연동 무중단 자가 업데이트 (`server/internal/ws/github_release.go`, `updater.go`, `client/internal/netclient/client.go`):**
  - `purnpado/kkomi_timetable` 최신 릴리즈 조회, 버전 비교(`ParseVersionTuple`, `IsNewerVersion`), 영문 에셋명 및 한글 라벨 동시 인식
  - 교체 직전 실행 파일 OS 매직 헤더(`Mach-O` / Windows `MZ`) 검증으로 OS 불일치 바이너리 덮어쓰기 원천 차단 및 macOS `.app` 번들 `codesign` 자동 재서명
- **운영체제별 실행 편의성 (`server/cmd/server/main.go`):**
  - macOS `꼬미시간서버.app` 더블클릭 시 Dock 점프 방지(`LSUIElement`) 및 `Terminal.app` 자동 실행(`relaunchInDarwinTerminal`)
  - Windows CMD 콘솔 창 유지 + 오류 발생 시 원인·해결 안내, `server-error.log` 기록 및 `[Enter]` 키 대기 일시정지(`failAndPause`)
- **개발 품질 게이트 (`server/internal/modules/`):**
  - 모든 소스 파일 500줄 이하 제한(`TestSourceFileLineLimit500`) 및 18개 단위·API·WebSocket E2E 테스트 구축

### 1.2. 인증 · 6종 업무분장(`RBAC`) · 계정 분리
- **시스템 기본 `최고관리자`(`admin`) 계정 분리 및 보호 (`pkg/protocol/models.go`, `seed.go`, `auth/service.go`):**
  - 초기·초기화 계정의 이름·직위를 **`최고관리자`**로 통일하고, 시간표·교내/외부 시험감독 자동 배정 및 연간 시수 통계에서 자동 제외(`IsSystemAdminAccount`)
  - 기본 `admin` 계정의 삭제 및 `최고관리자(MASTER)` 권한 해제 방지, 레거시 DB 자동 마이그레이션(`upgradeLegacyAdminIfNeeded`) 지원
- **담당교사 `최고관리자` 권한 겸임·위임 및 RBAC:**
  - 실제 교사(예: `kim` 김교무 교무부장)에게도 `최고관리자`, `시간표 담당`, `교내감독 담당`, `외부감독 담당`, `행사 담당`, `일반 교사` 다중 권한 부여 및 학년도 인계 가능
- **동명이인 구분 로그인 & 최초 비밀번호 강제 변경 (`LoginView.jsx`, `PasswordChangeModal.jsx`):**
  - `담당과목 선택 → 교사 선택 → 비밀번호` 및 직접 입력 로그인 지원, 초기 비밀번호(`1234`) 로그인 시 즉시 변경 유도

### 1.3. 정규·일일 시간표 · 시수표 · 교과교실제 (`server/internal/modules/timetable/`, `TimetableView.jsx`)
- **학교 기본설정 · 일과시간 · 교과별 시수표 매트릭스 (`curriculum.go`):**
  - 1~3학년 학급수·가상학급수·요일별 기준 시수, 교사별 시수 배당 일치성 검사(`일치/부족/초과`), 동시수업·연속블록·교과교실(특별실) 중복 차단 기반 자동 편성
- **엑셀(`.xlsx`) 일괄 입출력 (`xlsx_io.go`):**
  - 교사 명렬표 및 컴시간 호환 교사별 시수표 엑셀 다운로드·일괄 업로드
- **다주간(이번 주 · 다음 주 · 다다음 주) 시간표 & 주중 미출근 일괄 대책 (`multiweek.go`):**
  - 정규 시간표를 보존한 채 특정 주차·일자에만 `CROSS_WEEK_SWAP`(주차 간 맞교환) 및 `DATE_ONLY` 일일 보강 오버레이 적용
  - 주중 돌발 병가·출장·경조사 시 다주간 교환 후보 + 당일 공강 교사 대타 자동 추천 및 일괄 신청/확정
- **다단계 연쇄교환 시각화 & `STAGING` → `COMMIT` 승인:**
  - HTML5 Canvas 기반 드래그 이동, 🟢 즉시교환 · 🟡 삼각(3단계)/사각(4단계) 연쇄교환 1~4단계 과정 시각화 · 🔴 충돌사유 표시, PNG 클립보드 복사, 교과교실제 특별실별 시간표 열람
  - 일반 교사 신청은 `STAGING` 대기열에 적재되고 시간표 담당자가 전/후 비교 승인 모달에서 최종 확정, 나이스(NEIS) 입력 순서 정렬 출력 지원

### 1.4. 교내 시험감독 · 외부 시험감독 (`internal_exam/`, `external_exam/`)
- **교내 지필고사 감독 배정 & 보안 봉인:**
  - 과목별 상이한 시험시간(시/종 시각 및 분 단위) 설정, 담임 자기 반 배제, 담당 과목 교사 고사장 제외 및 복도감독 우선 배치(`CORRIDOR_FIRST` / 평가본부 대기 선택)
  - 과목·교시별 1인 정감독 단독 및 정·부감독 동시 운영, 학부모 명예감독 On/Off, 고교선택과목·교과교실·선택적 별도고사실(독감·감염병) 편성
  - 특수반 등 지정 제외 교사를 분리하고 전교원 연간 총 감독 횟수 및 정감독 횟수 편차 0~1회 이내 평탄화(`assigner.go`, `fairness.go`)
  - 사전 봉인 중 패킷 레벨 고사장 마스킹(`🔒 당일 08:20 공개`), 지정 시각 스케줄러 및 수동 일괄 공개, 당일 결원 시 긴급 대타 추천
- **외부 시험감독 지원 & 다단계 차출 조합기:**
  - 계좌번호·수당 등 금융정보 완전 배제, 원클릭 자발적 지원/취소
  - 학교 내규별 차출 기준 프리셋(`방식 A 혼합형`, `방식 B 순번/쿨타임형`, `방식 C 최근 전입자 우선형`, 커스텀 조합) 및 면제 대상 후순위/제외 처리
  - 평일 파견 확정 시 시간표 담당자 결보강 대기열(`STAGING`) 자동 이관

### 1.5. 학사일정 연동 · 문서 출력 · 데스크톱 UI/UX
- **주중·월중 행사 취합 및 4종 연동 (`event_calendar`):** `[단순안내]`, `[학년/전교 수업블록]`, `[특정교사 출장/결보강]`, `[시종시간 변경]` 연동
- **다중 문서 출력 (`server/internal/exporter/`):** 한글(`.hwpx`), 엑셀(`.xlsx`), 고대비 B4/A3 인쇄용 현황판 지원
- **현장 밀착형 Wails v3 데스크톱 UX (`client/`):** 수업 중 방해 금지 알림 큐, `Ctrl+K` 통합 검색 커맨드 팔레트, 우측 260px 사이드 도킹 위젯, 시스템 트레이 상주

---

## 2. 🔄 진행 중 (`v0.1.x` 현장 검증 및 안정화)

- [ ] **macOS · Windows 교무실 실제 환경 교차 실사용 테스트 검증:**
  - Windows 공용 서버(`꼬미시간서버.exe`) + 교사 PC(`꼬미시간.exe`) 다중 동시 접속, 방화벽/백신 환경에서의 포터블 MariaDB(`mysqld.exe`) 최초 초기화 및 장시간 구동 검증
  - macOS Finder 실행 및 클라이언트의 서버 접속/권한 전환/데이터 영속성 실사용 검증
- [ ] **학교 현장 실데이터 엑셀 양식 반입 예외 처리 보강:**
  - 학교마다 다른 교사 명렬표·컴시간 시수표(`.xlsx`) 헤더 변형·병합 셀·공백 처리 케이스 추가 보완
- [ ] **GitHub Releases 기반 자동 업데이트 실운영 검증:**
  - Windows 클라이언트(`꼬미시간.exe`) 및 서버(`꼬미시간서버.exe`)가 `v0.1.01` 이후 차기 버전(`v0.1.02+`) 출시 시 무중단 교체·재기동되는 전체 흐름 실지 점검

---

## 3. 📋 앞으로 구현해야 할 항목 (향후 개발 로드맵)

### 3.1. 단기 목표 (`v0.2.x` — 시간표·시험감독 실무 편의성 심화)
- [x] **시간표 편성 다중 시나리오(A/B/C안) 스냅샷 저장 · 비교 및 실행 취소(Undo/Redo):** *(3단계 구현 완료)*
  - `scenarios.go` · `ScenarioComparePanel.jsx`: 현재 시간표 A/B/C/D안 스냅샷 저장, 충돌·금지위반·기피교시·3/4연강·전일공강·종합점수 비교 및 원클릭 채택
  - `history.go` · `TimetableView.jsx`: 단일 InnoDB 트랜잭션 내 변경 전/후 스냅샷 기록(최대 20단계), 툴바 버튼 및 `Ctrl+Z` / `Ctrl+Y` 실행취소·다시실행
- [x] **교사별 회피 교시 · 희망 공강 · 부장회의 고정 공강 제약 조건 설정기:** *(3단계 구현 완료)*
  - `constraints.go` · `ConstraintManager.jsx`: `HARD_AVOID`(절대 금지)·`MEETING_FREE`(부장회의/교과협의회 일괄 공강)·`SOFT_AVOID`(기피 교시) 설정 및 기초/시수표 자동 편성·Canvas 신호등·미출근 대타 추천·엑셀 임포트 전반 반영
- [x] **다중일 지필고사(3~5일 연속 시험기간) 통합 편성 및 연속 교시(연강) 감독 방지:** *(4단계 구현 완료)*
  - `multiday.go` · `assigner.go` · `MultiDayExamEditor.jsx`: 3~5일 평일 시험기간 통합 편성, 일자별 부재 필터링, 동일 일자 연강 감독 방지(`AvoidConsecutive`), 1일 최대 감독 교시 제한, 일자별 부분 공개(`UnsealExamDate`) 구현
- [x] **결보강 · 외부감독 · 행사 실적 월별/학기별 누적 통계 및 결재용 출력물 강화:** *(4단계 구현 완료)*
  - `stats/service.go` · `settlement_exporter.go` · `ReportsSettlementView.jsx`: 월별/1·2학기/학년도/지정기간 교원별 실적 집계 및 4시트 정산표(`.xlsx`)·통합 정산 보고서(`.hwpx`)·결보강 확인서(`.hwpx`)·시험감독 배정표(`.hwpx`/`.xlsx`) 출력 구현
- [x] **실학교 기준 초기 화면·관리 UI 정돈 및 Radix UI 전면 적용 · 디자인 마감:** *(5단계 구현 완료)*
  - `EnsureCleanInit`(`reset.go`) · `--seed-demo`(`main.go`): 최초 실행 시 `admin` 계정·기본 일과·학교설정만 깨끗하게 초기화하며, 대시보드 초기 설정 가이드(`DashboardView.jsx`) 및 `LoginView.jsx`/`AdminRemoteView.jsx` 시연 문구·버튼 정리
  - **Radix UI 7종 적용:** `@radix-ui/react-tabs`(로그인·시간표·교내시험), `@radix-ui/react-context-menu`(`BaseTimetableBuilder` 우클릭 잠금/고정공강/삭제), `@radix-ui/react-alert-dialog`(`ConfirmDialog`), `@radix-ui/react-tooltip`(`AppTooltip` + `TooltipProvider`), `@radix-ui/react-dropdown-menu`(`ActionMenu`) + `radix.css` 모션/접근성(`prefers-reduced-motion`) 마감
- [x] **포터블 MariaDB `ibdata1` 0바이트 초기화 중단 자동 복구:** *(`mariadb_bootstrap.go` · `mariadb_portable.go`)*
  - `InspectMariaDBDataDir`로 미완료 초기화(`kkomi_timetable/` 부재 + `ibdata1` 0바이트) 감지 시 자동 재초기화, 실운영 DB 존재 시 자동 삭제 차단 및 기동 타임아웃 45초 확대

### 3.1-B. 차기 확정 마스터 플랜 (`v0.2.x ~ v0.3.x` 6단계 통합 고도화 계획 — 2026-11-22 논의 확정)
> 새 대화창에서 이어서 구현할 **실무 정밀 보강 · 스마트 시각화 · 서버 GUI 전환** 우선순위 계획입니다.

1. **[1단계] 핵심 버그 수정 & 기반 정비:** *(구현 완료)*
   - [x] `admin` 시스템 계정 프론트엔드 6곳 완전 격리 (`CurriculumLoadManager.jsx`, `TimetableView.jsx`, `MultiWeekAbsenceView.jsx`, `CalendarView.jsx`, `CommandPalette.jsx`, `TeacherManagerCard.jsx` + `AdminRemoteView.jsx` 시스템 전용 영역 분리)
   - [x] 비교과 교사(특수·보건·전문상담·사서·영양·기타) 유형 분리 및 수업 유무 / 결보강 대타 제외 속성 추가 (`pkg/protocol/models.go`, `auth/service.go`, `absence.go`, `base_builder.go`, `xlsx_teachers.go`)
   - [x] 특수교사 `특수학급고사실` 전담 배정 로직 구현 (`internal_exam/special_class.go`, `assigner.go`, `availability.go` — 특수학급고사실 개설 시 특수교사 최우선 배정, 부재 시 일반교사 폴백 및 대타 추천 1순위)
2. **[2단계] 시간표 엔진 & 실무 프로세스 고도화:** *(다음 착수 대상)*
   - **일회성(주차 한정) vs 정규 시간표 변경** 기본 분리 (`TimetableView.jsx` Canvas 드래그 수정 포함)
   - **수업 교체 모든 경우의 수 전수 탐색 + 품질 정렬 모달** (`solver.go`: 1:1 맞교환 · 삼각 3단계 · 사각 4단계 · 다주간 +1~4주+ 후보 전수 탐색, 기피교시/연강 점수 정렬 및 사각 단계별 경로 표시)
   - FET 방식 재귀적 교환(Ejection Chain) + 불가능 시수 역추적 진단기 보강 (`curriculum.go`)
   - 창체(자율·진로·봉사·동아리) 동시/고정 편성, 교과부장 시수 제출/취합 및 가편성 피드백 → 최종 확정(Lock) 워크플로우
   - 교과교실제 On/Off 및 특별실별 동시 수용 학급수(예: 체육관/운동장 2학급), 고교학점제 선택블록 통째 이동 · 2연강 보호 · 3인+ 코티칭
   - **학기당 과목별 필수 시수 확보 원장 & 미보충 수업 부채 추적**, 교사 간 사전합의/동의 보고 + 일과계 즉시 교체 옵션
3. **[3단계] 교내 · 외부 시험감독 고도화:**
   - **교내:** 동일 고사실 중복 배정 금지, 사회적 약자(임산부·원로교사 등)·보직 차등 감면(최대 N회 상한·경감·연강 절대 금지), 해당 학년/출제 교사만 배제 옵션, 감염병 분리고사실 1·2·3실 복수 개설, 고교학점제 다과목 동시 이동시험 및 공강 학생 자습실 감독, 자동 배정 후 수동 맞교환, 개인별 감독 시간표
   - **외부:** 기본 3종(`검정고시`, `수능감독`, `임용고사`) + 담당자 자유 추가, 종목별 가중치 점수제 또는 별도 순번 트랙, 전입순 양방향(최근 전입자 우선 / 장기근속 우선) + 전입 1년차 유예, 시험 종류별 제외 규칙(수능 고3 담임·수험생자녀·기간제 등), 동점자 공정 처리(연령/호봉/가나다/공정 추첨), 지원자 초과 시 선발 기준, 학교 기본 내규 프리셋 저장, 공고 수정/삭제 UI 연결
4. **[4단계] 주중 · 월중 행사 고도화 (`event_calendar`, `CalendarView.jsx`):**
   - `월간 달력` · `주간업무표(월~금 교무회의/공문용)` · `취합 대기함` 3종 뷰 전환
   - 다일 연속 행사(`EndDate`) 및 공휴일·재량휴업일·개교기념일 유형 지원(학기 필수 시수 원장에서 휴업일 자동 차감 연동)
   - 단축수업(`40분`/`45분`/오전행사) 시종시간 변경(`BellOverride`) 원클릭 프리셋 UI 연결
   - 전교/복수 학년/특정 반 및 복수 인솔교사 선택, 행사 장소(강당·시청각실 등)·동일 학년 교시 충돌 경고, 행사 수정·삭제·부서별 필터·반복 일정
5. **[5단계] 전 페이지 UI/UX 스마트 시각화 (`방식 ①: Radix UI 확장 + 데이터 시각화`):**
   - `@radix-ui/react-switch`, `collapsible`, `popover`, `progress` 추가 적용 및 이모지 → Lucide SVG/상태 도트 통일
   - 긴 설명문/상시 노출 폼 다이어트(접이식/모달 등록), 반응형 `TimetableCanvas`, 대시보드 현재 교시 타임라인 바
   - 클릭형 5×7 공강 히트맵(`ConstraintManager`), 학급 시수 게이지(`CurriculumLoadManager`), 정/부/복도 스택드 막대 차트(`ExamFairnessPanel`), D-Day 모집 카드(`ExternalExamView`), 월별 SVG 막대 차트·인라인 데이터 바(`ReportsSettlementView`), 시스템 관리 4탭·권한 체크 매트릭스(`AdminRemoteView`)
6. **[6단계] `꼬미시간서버` Wails v3 GUI 컨트롤 센터 & 트레이 전환 (`방식 A`):**
   - CMD 콘솔 창 대신 전용 미니 상태 대시보드(520×640px) + 닫기(`X`) 클릭 시 시스템 트레이 최소화(실수 종료 차단)
   - 최초 포터블 MariaDB 초기화 진행률/경과 시간 화면, 원클릭 LAN 접속 주소 복사, 실시간 접속 교사 수, 즉시 암호화 백업/폴더 열기/로그 접이식 뷰어 제공 (`--headless`, `--seed-demo`, `--init-template` CLI 플래그 호환 유지)

### 3.2. 중기 목표 (`v0.3.x` — 고교학점제 · 나이스 연계 · 교무실 환경 확장)
- [ ] **고교학점제 학생 개인별 · 이동수업 반별 안내 시간표 생성 및 배포:**
  - 선택과목(A/B/C 블록) 이동 교실 및 반별 분반 안내표 출력 기능
- [ ] **나이스(NEIS) 학사일정 · 기초시간표 CSV/XLSX 양방향 호환 및 검증기 확대:**
  - 나이스 업로드용 양식 정합성(시수 합계, 과목코드, 교사 중복) 자동 점검 강화
- [ ] **교무실 대형 모니터/TV 전광판(키오스크) 전용 읽기 전용 대시보드 모드:**
  - 오늘의 시종시간, 일일 보강/수업 변경 내역, 당일 행사·시험 안내를 교무실 TV 화면에 크게 실시간 띄우는 전용 뷰

### 3.3. 장기 목표 (`v1.0` — 멀티 OS 배포 자동화 · 학년도 전환 완성)
- [ ] **GitHub Releases macOS 전용 에셋(`.app.zip`/`.dmg`) 동시 배포 및 macOS 자동 업데이트 지원:**
  - 현재 Windows 바이너리 중심의 GitHub 릴리즈 에셋에 macOS용 압축 번들을 함께 배포하고 `ApplyClientSelfUpdate`가 macOS에서도 자동 패치 가능하도록 확장
- [ ] **Windows 공용 서버 부팅 시 자동 실행(작업 스케줄러/시작프로그램) 원클릭 등록 도우미:**
  - 교무실 공용 PC 재부팅 후에도 담당자가 따로 켜지 않아도 `꼬미시간서버.exe`가 자동 기동되도록 지원
- [ ] **학년도 마감 아카이브 및 신학년도 이관 마법사:**
  - 전년도 데이터는 읽기 전용 암호화 보관함으로 분리하고, 교원 인사이동(전출·전입·신규) 명렬표를 반영해 새 학년도를 5분 만에 세팅하는 단계별 마법사

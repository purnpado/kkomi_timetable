package modules_test

import (
	"testing"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
	"kkomi-timetable/server/internal/eventbus"
	"kkomi-timetable/server/internal/modules/auth"
)

// TestPhase5EnsureCleanInitAdminOnly는 실학교 최초 기동 시 시연용 가짜 데이터 대신
// 최고관리자(admin) 계정, 기본 일과시간표, 학교 기본설정만 깨끗하게 생성되는지 검증합니다.
func TestPhase5EnsureCleanInitAdminOnly(t *testing.T) {
	t.Setenv("KKOMI_BASE_DIR", t.TempDir())

	store, err := database.Open()
	if err != nil {
		t.Fatalf("깨끗한 테스트 DB 열기 실패: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := database.EnsureCleanInit(store); err != nil {
		t.Fatalf("EnsureCleanInit 실패: %v", err)
	}

	// 1. 교사는 최고관리자(admin) 1명만 존재해야 함
	teachers, err := database.LoadCollection[protocol.Teacher](store, "teachers")
	if err != nil {
		t.Fatalf("교사 목록 조회 실패: %v", err)
	}
	if len(teachers) != 1 {
		t.Fatalf("최초 기동 시 교사는 1명(admin)만 있어야 합니다: got=%d", len(teachers))
	}
	if teachers[0].Username != "admin" || teachers[0].Name != "최고관리자" || !teachers[0].MustChangePassword {
		t.Fatalf("기본 admin 계정 속성이 올바르지 않습니다: %+v", teachers[0])
	}

	// 2. 시연 모드가 비활성화되어 있어야 함
	authSvc := auth.NewService(store, eventbus.New())
	if authSvc.IsDemoMode() {
		t.Fatalf("EnsureCleanInit 이후 IsDemoMode()는 false여야 합니다")
	}

	// 3. 기본 일과시간표 7건 및 학교 기본설정 1건이 존재해야 함
	bells, err := database.LoadCollection[protocol.BellPeriod](store, "bell_schedule")
	if err != nil || len(bells) != 7 {
		t.Fatalf("기본 일과시간표 7건이 준비되지 않았습니다: len=%d, err=%v", len(bells), err)
	}
	configs, err := database.LoadCollection[protocol.SchoolConfig](store, "school_configs")
	if err != nil || len(configs) != 1 {
		t.Fatalf("기본 학교설정이 준비되지 않았습니다: len=%d, err=%v", len(configs), err)
	}

	// 4. 시간표·시수·행사 데이터는 비어 있어야 함
	cells, _ := database.LoadCollection[protocol.TimetableCell](store, "timetable_cells")
	loads, _ := database.LoadCollection[protocol.TeachingLoad](store, "teaching_loads")
	if len(cells) != 0 || len(loads) != 0 {
		t.Fatalf("최초 기동 시 시간표/시수 데이터는 비어 있어야 합니다: cells=%d, loads=%d", len(cells), len(loads))
	}

	// 5. 초기 admin/1234 로그인 성공 확인
	loggedIn, err := authSvc.Authenticate("admin", "1234")
	if err != nil || loggedIn.Username != "admin" {
		t.Fatalf("초기 admin 계정 로그인 실패: %v", err)
	}

	// 6. 멱등성: EnsureCleanInit 재호출 시에도 교사 수(1명)가 그대로 유지되어야 함
	if err := database.EnsureCleanInit(store); err != nil {
		t.Fatalf("EnsureCleanInit 재호출 실패: %v", err)
	}
	teachersAgain, _ := database.LoadCollection[protocol.Teacher](store, "teachers")
	if len(teachersAgain) != 1 {
		t.Fatalf("EnsureCleanInit 재호출 시 교사 수가 변했습니다: got=%d", len(teachersAgain))
	}
}

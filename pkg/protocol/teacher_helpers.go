package protocol

import "strings"

// IsSystemAdminAccount는 실제 교원이 아닌 서버 초기 구동·비상 관리용 기본 '최고관리자(admin)' 시스템 계정인지 판별합니다.
func IsSystemAdminAccount(t Teacher) bool {
	return strings.EqualFold(strings.TrimSpace(t.Username), "admin") &&
		(t.Name == "최고관리자" || t.Name == "시스템 관리자" || t.Subject == "시스템" || t.Subject == "행정/총괄")
}

// BoolPtr는 불리언 값의 포인터를 반환합니다.
func BoolPtr(v bool) *bool {
	return &v
}

// NormalizeTeacherType은 영문 코드 또는 한글 교원 유형 문자열을 정규 TeacherType 상수로 변환합니다.
func NormalizeTeacherType(raw TeacherType) TeacherType {
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return ""
	}
	upper := strings.ToUpper(s)
	switch TeacherType(upper) {
	case TeacherTypeSubject, TeacherTypeSpecial, TeacherTypeHealth, TeacherTypeCounselor, TeacherTypeLibrarian, TeacherTypeNutrition, TeacherTypeOther:
		return TeacherType(upper)
	}
	lower := strings.ToLower(s)
	switch {
	case strings.Contains(lower, "특수"):
		return TeacherTypeSpecial
	case strings.Contains(lower, "보건"):
		return TeacherTypeHealth
	case strings.Contains(lower, "상담") || strings.Contains(lower, "위클래스") || strings.Contains(lower, "wee"):
		return TeacherTypeCounselor
	case strings.Contains(lower, "사서") || strings.Contains(lower, "도서관"):
		return TeacherTypeLibrarian
	case strings.Contains(lower, "영양"):
		return TeacherTypeNutrition
	case strings.Contains(lower, "기타") || strings.Contains(lower, "비교과"):
		return TeacherTypeOther
	case strings.Contains(lower, "교과") || strings.Contains(lower, "일반"):
		return TeacherTypeSubject
	default:
		return ""
	}
}

// EffectiveTeacherType은 명시된 유형 또는 직위·과목 키워드로부터 교원의 실효 유형을 판정합니다.
func EffectiveTeacherType(t Teacher) TeacherType {
	if norm := NormalizeTeacherType(t.TeacherType); norm != "" {
		return norm
	}
	if IsSystemAdminAccount(t) || t.IsParentHonorary {
		return TeacherTypeOther
	}
	joined := strings.ToLower(strings.Join([]string{t.Position, t.Subject}, " "))
	switch {
	case strings.Contains(joined, "특수"):
		return TeacherTypeSpecial
	case strings.Contains(joined, "보건"):
		return TeacherTypeHealth
	case strings.Contains(joined, "상담") || strings.Contains(joined, "위클래스") || strings.Contains(joined, "wee"):
		return TeacherTypeCounselor
	case strings.Contains(joined, "사서") || strings.Contains(joined, "도서관"):
		return TeacherTypeLibrarian
	case strings.Contains(joined, "영양"):
		return TeacherTypeNutrition
	default:
		return TeacherTypeSubject
	}
}

// IsNonSubjectTeacher는 시스템 관리자·학부모 명예감독을 제외한 비교과 교원인지 여부를 반환합니다.
func IsNonSubjectTeacher(t Teacher) bool {
	return !IsSystemAdminAccount(t) && !t.IsParentHonorary && EffectiveTeacherType(t) != TeacherTypeSubject
}

// IsSpecialEducationTeacher는 특수교사 유형인지 판별합니다.
func IsSpecialEducationTeacher(t Teacher) bool {
	return !IsSystemAdminAccount(t) && !t.IsParentHonorary && EffectiveTeacherType(t) == TeacherTypeSpecial
}

// HasRegularClasses는 교원이 정규 시간표 수업 담당 대상인지 여부를 반환합니다.
func HasRegularClasses(t Teacher) bool {
	if IsSystemAdminAccount(t) || t.IsParentHonorary {
		return false
	}
	if t.HasClasses != nil {
		return *t.HasClasses
	}
	return EffectiveTeacherType(t) == TeacherTypeSubject
}

// IsSubstituteExcluded는 교원이 결보강 대타 후보에서 제외되어야 하는지 여부를 반환합니다.
func IsSubstituteExcluded(t Teacher) bool {
	if IsSystemAdminAccount(t) || t.IsParentHonorary {
		return true
	}
	if t.SubstituteExempt != nil {
		return *t.SubstituteExempt
	}
	return EffectiveTeacherType(t) != TeacherTypeSubject
}

// PopulateTeacherDefaults는 교원 유형, 수업 유무, 결보강 대타 제외 필드의 기본값을 채워 넣습니다.
func PopulateTeacherDefaults(t *Teacher) {
	if t == nil {
		return
	}
	if IsSystemAdminAccount(*t) {
		t.TeacherType = TeacherTypeOther
		t.HasClasses = BoolPtr(false)
		t.SubstituteExempt = BoolPtr(true)
		return
	}
	if t.IsParentHonorary {
		t.TeacherType = TeacherTypeOther
		t.HasClasses = BoolPtr(false)
		t.SubstituteExempt = BoolPtr(true)
		return
	}
	effType := EffectiveTeacherType(*t)
	hasCls := HasRegularClasses(*t)
	subEx := IsSubstituteExcluded(*t)
	t.TeacherType = effType
	t.HasClasses = BoolPtr(hasCls)
	t.SubstituteExempt = BoolPtr(subEx)
}

// TeacherTypeLabel은 교원 유형의 한글 라벨을 반환합니다.
func TeacherTypeLabel(tt TeacherType) string {
	switch NormalizeTeacherType(tt) {
	case TeacherTypeSpecial:
		return "특수"
	case TeacherTypeHealth:
		return "보건"
	case TeacherTypeCounselor:
		return "상담"
	case TeacherTypeLibrarian:
		return "사서"
	case TeacherTypeNutrition:
		return "영양"
	case TeacherTypeOther:
		return "기타"
	default:
		return "교과"
	}
}

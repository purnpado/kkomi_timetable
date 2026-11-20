package exporter

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"kkomi-timetable/pkg/pathutil"
)

// HWPXDocumentData는 한글(.hwpx) 보고서 생성을 위한 표 데이터 구조입니다.
type HWPXDocumentData struct {
	SchoolName string
	DocTitle   string
	PeriodText string
	Headers    []string
	Rows       [][]string
}

var reLinesegArray = regexp.MustCompile(`(?s)<hp:linesegarray\b[^>]*>.*?</hp:linesegarray>`)

// escapeXML은 한글 OWPML XML 깨짐을 방지하기 위해 특수문자를 이스케이프합니다.
func escapeXML(s string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

// BuildHWPXBytes는 KS X 6101 OWPML 표준 규격을 준수하는 국가표준 한글(.hwpx) 문서를 바이트 배열로 생성합니다.
// templates 폴더에 학교표준_학사보고서_템플릿.hwpx 파일이 존재하면 내부 태그 치환 및 <hp:linesegarray> 제거를 적용합니다.
func BuildHWPXBytes(data HWPXDocumentData) ([]byte, error) {
	if strings.TrimSpace(data.SchoolName) == "" {
		data.SchoolName = "꼬미중·고등학교"
	}
	tplDir, err := pathutil.TemplateDir()
	if err == nil {
		tplPath := pathutil.SafeJoin(tplDir, "학교표준_학사보고서_템플릿.hwpx")
		if _, statErr := os.Stat(tplPath); statErr == nil {
			if replaced, rErr := replaceExistingHWPXTemplate(tplPath, data); rErr == nil {
				return replaced, nil
			}
		}
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	// 1. mimetype은 OCF 규격상 첫 번째 엔트리이며 Store(무압축) 방식으로 기록
	mh := &zip.FileHeader{Name: "mimetype", Method: zip.Store}
	mw, err := zw.CreateHeader(mh)
	if err != nil {
		return nil, err
	}
	if _, err := mw.Write([]byte("application/hwp+zip")); err != nil {
		return nil, err
	}

	entries := []struct {
		name    string
		content string
	}{
		{"version.xml", buildVersionXML()},
		{"settings.xml", buildSettingsXML()},
		{"META-INF/container.xml", buildContainerXML()},
		{"META-INF/manifest.xml", buildManifestXML()},
		{"Contents/content.hpf", buildContentHPF(data)},
		{"Contents/header.xml", buildHeaderXML()},
		{"Contents/section0.xml", buildSectionXML(data)},
	}
	for _, ent := range entries {
		w, createErr := zw.Create(ent.name)
		if createErr != nil {
			return nil, createErr
		}
		if _, writeErr := w.Write([]byte(ent.content)); writeErr != nil {
			return nil, writeErr
		}
	}

	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// EnsureDefaultHWPXTemplate은 templates 폴더에 기본 .hwpx 템플릿 파일이 없을 때 자동 생성합니다.
func EnsureDefaultHWPXTemplate() (string, error) {
	tplDir, err := pathutil.TemplateDir()
	if err != nil {
		return "", err
	}
	tplPath := pathutil.SafeJoin(tplDir, "학교표준_학사보고서_템플릿.hwpx")
	if _, statErr := os.Stat(tplPath); statErr == nil {
		return tplPath, nil
	}
	raw, err := BuildHWPXBytes(HWPXDocumentData{
		SchoolName: "{{SCHOOL_NAME}}",
		DocTitle:   "{{DOC_TITLE}}",
		PeriodText: "{{PERIOD_TEXT}}",
		Headers:    []string{"순번", "일자/교시", "구분", "내용", "담당/비고"},
		Rows: [][]string{
			{"1", "{{ROW_DATE}}", "{{ROW_TYPE}}", "{{ROW_CONTENT}}", "{{ROW_NOTE}}"},
		},
	})
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(tplPath, raw, 0o644); err != nil {
		return "", err
	}
	return tplPath, nil
}

func replaceExistingHWPXTemplate(tplPath string, data HWPXDocumentData) ([]byte, error) {
	raw, err := os.ReadFile(tplPath)
	if err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, err
	}

	// mimetype이 항상 첫 번째 Store 엔트리가 되도록 정렬
	files := append([]*zip.File(nil), zr.File...)
	sort.SliceStable(files, func(i, j int) bool {
		if files[i].Name == "mimetype" {
			return true
		}
		if files[j].Name == "mimetype" {
			return false
		}
		return i < j
	})

	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, f := range files {
		rc, openErr := f.Open()
		if openErr != nil {
			return nil, openErr
		}
		var contentBuf bytes.Buffer
		_, _ = contentBuf.ReadFrom(rc)
		_ = rc.Close()

		body := contentBuf.Bytes()
		if strings.HasPrefix(f.Name, "Contents/section") && strings.HasSuffix(f.Name, ".xml") {
			if f.Name == "Contents/section0.xml" && bytes.Contains(body, []byte("{{ROW_CONTENT}}")) {
				body = []byte(buildSectionXML(data))
			} else {
				text := string(body)
				text = strings.ReplaceAll(text, "{{SCHOOL_NAME}}", escapeXML(data.SchoolName))
				text = strings.ReplaceAll(text, "{{DOC_TITLE}}", escapeXML(data.DocTitle))
				text = strings.ReplaceAll(text, "{{PERIOD_TEXT}}", escapeXML(data.PeriodText))
				text = strings.ReplaceAll(text, "{{CREATED_DATE}}", escapeXML(time.Now().Format("2006-01-02")))
				text = reLinesegArray.ReplaceAllString(text, "")
				body = []byte(text)
			}
		}

		method := f.Method
		if f.Name == "mimetype" {
			method = zip.Store
		}
		hdr := &zip.FileHeader{Name: f.Name, Method: method}
		w, cErr := zw.CreateHeader(hdr)
		if cErr != nil {
			return nil, cErr
		}
		if _, wErr := w.Write(body); wErr != nil {
			return nil, wErr
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

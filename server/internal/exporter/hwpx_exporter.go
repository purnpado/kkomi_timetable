package exporter

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
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

// escapeXML은 한글 OWPML XML 깨짐을 방지하기 위해 특수문자를 이스케이프합니다.
func escapeXML(s string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

// BuildHWPXBytes는 유효한 OWPML 구조의 국가표준 한글(.hwpx) 문서를 바이트 배열로 생성합니다.
// templates/school_template.hwpx 파일이 존재하면 해당 템플릿 내부 XML의 치환 태그를 대체합니다.
func BuildHWPXBytes(data HWPXDocumentData) ([]byte, error) {
	if data.SchoolName == "" {
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

	writeZipFile := func(name, content string) error {
		w, createErr := zw.Create(name)
		if createErr != nil {
			return createErr
		}
		_, writeErr := w.Write([]byte(content))
		return writeErr
	}

	if err := writeZipFile("version.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><hv:HCFVersion xmlns:hv="http://www.hancom.co.kr/hwpml/2011/version" tagetApplication="WORDPROCESSOR" major="5" minor="1" micro="0" buildNumber="1" os="1" xmlVersion="1.5" application="KkomiTimetable" appVersion="1.0.0"/>`); err != nil {
		return nil, err
	}
	if err := writeZipFile("META-INF/container.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><ocf:container xmlns:ocf="urn:oasis:names:tc:opendocument:xmlns:container"><ocf:rootfiles><ocf:rootfile full-path="Contents/content.hpf" media-type="application/hwpml-package+xml"/></ocf:rootfiles></ocf:container>`); err != nil {
		return nil, err
	}
	if err := writeZipFile("META-INF/manifest.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><manifest:manifest xmlns:manifest="urn:oasis:names:tc:opendocument:xmlns:manifest:1.0"/>`); err != nil {
		return nil, err
	}
	if err := writeZipFile("Contents/content.hpf", buildContentHPF(data)); err != nil {
		return nil, err
	}
	if err := writeZipFile("Contents/header.xml", buildHeaderXML()); err != nil {
		return nil, err
	}
	if err := writeZipFile("Contents/section0.xml", buildSectionXML(data)); err != nil {
		return nil, err
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
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, f := range zr.File {
		rc, openErr := f.Open()
		if openErr != nil {
			return nil, openErr
		}
		var contentBuf bytes.Buffer
		_, _ = contentBuf.ReadFrom(rc)
		_ = rc.Close()

		body := contentBuf.Bytes()
		if f.Name == "Contents/section0.xml" {
			// 커스텀 템플릿이 기본 플레이스홀더를 포함할 경우 전체 섹션 표로 안전 치환
			if bytes.Contains(body, []byte("{{DOC_TITLE}}")) || bytes.Contains(body, []byte("{{ROW_CONTENT}}")) {
				body = []byte(buildSectionXML(data))
			} else {
				text := string(body)
				text = strings.ReplaceAll(text, "{{SCHOOL_NAME}}", escapeXML(data.SchoolName))
				text = strings.ReplaceAll(text, "{{CREATED_DATE}}", escapeXML(time.Now().Format("2006-01-02")))
				body = []byte(text)
			}
		}

		hdr := &zip.FileHeader{Name: f.Name, Method: f.Method}
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

func buildContentHPF(data HWPXDocumentData) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<opf:package xmlns:opf="http://www.idpf.org/2007/opf/" version="" unique-identifier="" id="">
  <opf:metadata>
    <opf:title>%s</opf:title>
    <opf:language>ko</opf:language>
  </opf:metadata>
  <opf:manifest>
    <opf:item id="header" href="Contents/header.xml" media-type="application/xml"/>
    <opf:item id="section0" href="Contents/section0.xml" media-type="application/xml"/>
  </opf:manifest>
  <opf:spine>
    <opf:itemref idref="header" linear="yes"/>
    <opf:itemref idref="section0" linear="yes"/>
  </opf:spine>
</opf:package>`, escapeXML(data.DocTitle))
}

func buildHeaderXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<hh:head xmlns:hh="http://www.hancom.co.kr/hwpml/2011/head" version="1.5" secCnt="1">
  <hh:beginNum page="1" footnote="1" endnote="1" pic="1" tbl="1" equation="1"/>
  <hh:refList>
    <hh:fontfaces itemCnt="1">
      <hh:fontface lang="HANGUL" fontCnt="1">
        <hh:font id="0" face="맑은 고딕" type="TTF" isEmbedded="0"/>
      </hh:fontface>
    </hh:fontfaces>
  </hh:refList>
</hh:head>`
}

func buildSectionXML(data HWPXDocumentData) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	sb.WriteString(`<hs:sec xmlns:hs="http://www.hancom.co.kr/hwpml/2011/section" xmlns:hp="http://www.hancom.co.kr/hwpml/2011/paragraph">`)
	sb.WriteString(fmt.Sprintf(`<hp:p><hp:run><hp:t>[%s] %s (%s)</hp:t></hp:run></hp:p>`,
		escapeXML(data.SchoolName), escapeXML(data.DocTitle), escapeXML(data.PeriodText)))
	sb.WriteString(fmt.Sprintf(`<hp:p><hp:run><hp:t>출력일시: %s | 결재: [ 담당 ] ── [ 부장 ] ── [ 교감 ] ── [ 교장 ]</hp:t></hp:run></hp:p>`,
		time.Now().Format("2006-01-02 15:04")))

	rowCnt := 1 + len(data.Rows)
	colCnt := len(data.Headers)
	if colCnt == 0 {
		colCnt = 1
	}
	sb.WriteString(fmt.Sprintf(`<hp:tbl id="1" rowCnt="%d" colCnt="%d">`, rowCnt, colCnt))
	// 헤더 행
	sb.WriteString(`<hp:tr>`)
	for _, h := range data.Headers {
		sb.WriteString(fmt.Sprintf(`<hp:tc><hp:subList><hp:p><hp:run><hp:t>%s</hp:t></hp:run></hp:p></hp:subList></hp:tc>`, escapeXML(h)))
	}
	sb.WriteString(`</hp:tr>`)
	// 본문 행
	for _, row := range data.Rows {
		sb.WriteString(`<hp:tr>`)
		for i := 0; i < colCnt; i++ {
			val := ""
			if i < len(row) {
				val = row[i]
			}
			sb.WriteString(fmt.Sprintf(`<hp:tc><hp:subList><hp:p><hp:run><hp:t>%s</hp:t></hp:run></hp:p></hp:subList></hp:tc>`, escapeXML(val)))
		}
		sb.WriteString(`</hp:tr>`)
	}
	sb.WriteString(`</hp:tbl></hs:sec>`)
	return sb.String()
}

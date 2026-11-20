package exporter

import (
	"fmt"
	"strings"
	"time"
)

func buildVersionXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<hv:HCFVersion xmlns:hv="http://www.hancom.co.kr/hwpml/2011/version" tagetApplication="WORDPROCESSOR" major="5" minor="1" micro="0" buildNumber="1" os="1" xmlVersion="1.5" application="KkomiTimetable" appVersion="0.1.01"/>`
}

func buildSettingsXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<ha:HWPApplicationSetting xmlns:ha="http://www.hancom.co.kr/hwpml/2011/app" xmlns:config="urn:oasis:names:tc:opendocument:xmlns:config:1.0">` +
		`<ha:CaretPosition listIDRef="0" paraIDRef="0" pos="0"/>` +
		`</ha:HWPApplicationSetting>`
}

func buildContainerXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<ocf:container xmlns:ocf="urn:oasis:names:tc:opendocument:xmlns:container">` +
		`<ocf:rootfiles>` +
		`<ocf:rootfile full-path="Contents/content.hpf" media-type="application/hwpml-package+xml"/>` +
		`</ocf:rootfiles>` +
		`</ocf:container>`
}

func buildManifestXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<manifest:manifest xmlns:manifest="urn:oasis:names:tc:opendocument:xmlns:manifest:1.0"/>`
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
	langs := []string{"HANGUL", "LATIN", "HANJA", "JAPANESE", "OTHER", "SYMBOL", "USER"}
	var fontBuf strings.Builder
	for _, lang := range langs {
		fontBuf.WriteString(fmt.Sprintf(`<hh:fontface lang="%s" fontCnt="1"><hh:font id="0" face="맑은 고딕" type="TTF" isEmbedded="0"/></hh:fontface>`, lang))
	}

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<hh:head xmlns:hh="http://www.hancom.co.kr/hwpml/2011/head" xmlns:hc="http://www.hancom.co.kr/hwpml/2011/core" version="1.5" secCnt="1">
  <hh:beginNum page="1" footnote="1" endnote="1" pic="1" tbl="1" equation="1"/>
  <hh:refList>
    <hh:fontfaces itemCnt="%d">%s</hh:fontfaces>
    <hh:borderFills itemCnt="4">
      <hh:borderFill id="1" threeD="0" shadow="0" centerLine="NONE" breakCellSeparateLine="0">
        <hh:slash type="NONE" Crooked="0" isCounter="0"/>
        <hh:backSlash type="NONE" Crooked="0" isCounter="0"/>
        <hh:leftBorder type="NONE" width="0.1 mm" color="#000000"/>
        <hh:rightBorder type="NONE" width="0.1 mm" color="#000000"/>
        <hh:topBorder type="NONE" width="0.1 mm" color="#000000"/>
        <hh:bottomBorder type="NONE" width="0.1 mm" color="#000000"/>
      </hh:borderFill>
      <hh:borderFill id="2" threeD="0" shadow="0" centerLine="NONE" breakCellSeparateLine="0">
        <hh:slash type="NONE" Crooked="0" isCounter="0"/>
        <hh:backSlash type="NONE" Crooked="0" isCounter="0"/>
        <hh:leftBorder type="SOLID" width="0.12 mm" color="#94A3B8"/>
        <hh:rightBorder type="SOLID" width="0.12 mm" color="#94A3B8"/>
        <hh:topBorder type="SOLID" width="0.12 mm" color="#94A3B8"/>
        <hh:bottomBorder type="SOLID" width="0.12 mm" color="#94A3B8"/>
      </hh:borderFill>
      <hh:borderFill id="3" threeD="0" shadow="0" centerLine="NONE" breakCellSeparateLine="0">
        <hh:slash type="NONE" Crooked="0" isCounter="0"/>
        <hh:backSlash type="NONE" Crooked="0" isCounter="0"/>
        <hh:leftBorder type="SOLID" width="0.12 mm" color="#64748B"/>
        <hh:rightBorder type="SOLID" width="0.12 mm" color="#64748B"/>
        <hh:topBorder type="SOLID" width="0.12 mm" color="#64748B"/>
        <hh:bottomBorder type="SOLID" width="0.12 mm" color="#64748B"/>
        <hc:fillBrush><hc:winBrush faceColor="#E8EFF8" hatchColor="#E8EFF8" alpha="0"/></hc:fillBrush>
      </hh:borderFill>
      <hh:borderFill id="4" threeD="0" shadow="0" centerLine="NONE" breakCellSeparateLine="0">
        <hh:slash type="NONE" Crooked="0" isCounter="0"/>
        <hh:backSlash type="NONE" Crooked="0" isCounter="0"/>
        <hh:leftBorder type="SOLID" width="0.12 mm" color="#CBD5E1"/>
        <hh:rightBorder type="SOLID" width="0.12 mm" color="#CBD5E1"/>
        <hh:topBorder type="SOLID" width="0.12 mm" color="#CBD5E1"/>
        <hh:bottomBorder type="SOLID" width="0.12 mm" color="#CBD5E1"/>
        <hc:fillBrush><hc:winBrush faceColor="#F8FAFC" hatchColor="#F8FAFC" alpha="0"/></hc:fillBrush>
      </hh:borderFill>
    </hh:borderFills>
    <hh:charProperties itemCnt="4">
      <hh:charPr id="0" height="1000" textColor="#111827" shadeColor="none" useFontSpace="0" useKerning="0" symMark="NONE" borderFillIDRef="1">
        <hh:fontRef hangul="0" latin="0" hanja="0" japanese="0" other="0" symbol="0" user="0"/>
      </hh:charPr>
      <hh:charPr id="1" height="1500" textColor="#0F172A" shadeColor="none" useFontSpace="0" useKerning="0" symMark="NONE" borderFillIDRef="1">
        <hh:fontRef hangul="0" latin="0" hanja="0" japanese="0" other="0" symbol="0" user="0"/>
        <hh:bold/>
      </hh:charPr>
      <hh:charPr id="2" height="900" textColor="#475569" shadeColor="none" useFontSpace="0" useKerning="0" symMark="NONE" borderFillIDRef="1">
        <hh:fontRef hangul="0" latin="0" hanja="0" japanese="0" other="0" symbol="0" user="0"/>
      </hh:charPr>
      <hh:charPr id="3" height="1000" textColor="#1E293B" shadeColor="none" useFontSpace="0" useKerning="0" symMark="NONE" borderFillIDRef="1">
        <hh:fontRef hangul="0" latin="0" hanja="0" japanese="0" other="0" symbol="0" user="0"/>
        <hh:bold/>
      </hh:charPr>
    </hh:charProperties>
    <hh:tabProperties itemCnt="1">
      <hh:tabPr id="0" autoTabLeft="0" autoTabRight="0"/>
    </hh:tabProperties>
    <hh:paraProperties itemCnt="3">
      <hh:paraPr id="0" tabPrIDRef="0" condense="0" fontLineHeight="0" snapToGrid="1" suppressLineNumbers="0" checked="0">
        <hh:align horizontal="LEFT" vertical="BASELINE"/>
        <hh:heading type="NONE" idRef="0" level="0"/>
        <hh:breakSetting breakLatinWord="KEEP_WORD" breakNonLatinWord="KEEP_WORD" widowOrphan="0" keepWithNext="0" keepLines="0" pageBreakBefore="0" lineWrap="BREAK"/>
        <hh:autoSpacing eAsianEng="0" eAsianNum="0"/>
        <hh:margin><hc:intent value="0" unit="HWPUNIT"/><hc:left value="0" unit="HWPUNIT"/><hc:right value="0" unit="HWPUNIT"/><hc:prev value="0" unit="HWPUNIT"/><hc:next value="0" unit="HWPUNIT"/></hh:margin>
        <hh:lineSpacing type="PERCENT" value="130" unit="HWPUNIT"/>
        <hh:border borderFillIDRef="1" offsetLeft="0" offsetRight="0" offsetTop="0" offsetBottom="0" connect="0" ignoreMargin="0"/>
      </hh:paraPr>
      <hh:paraPr id="1" tabPrIDRef="0" condense="0" fontLineHeight="0" snapToGrid="1" suppressLineNumbers="0" checked="0">
        <hh:align horizontal="CENTER" vertical="BASELINE"/>
        <hh:heading type="NONE" idRef="0" level="0"/>
        <hh:breakSetting breakLatinWord="KEEP_WORD" breakNonLatinWord="KEEP_WORD" widowOrphan="0" keepWithNext="0" keepLines="0" pageBreakBefore="0" lineWrap="BREAK"/>
        <hh:autoSpacing eAsianEng="0" eAsianNum="0"/>
        <hh:margin><hc:intent value="0" unit="HWPUNIT"/><hc:left value="0" unit="HWPUNIT"/><hc:right value="0" unit="HWPUNIT"/><hc:prev value="0" unit="HWPUNIT"/><hc:next value="0" unit="HWPUNIT"/></hh:margin>
        <hh:lineSpacing type="PERCENT" value="130" unit="HWPUNIT"/>
        <hh:border borderFillIDRef="1" offsetLeft="0" offsetRight="0" offsetTop="0" offsetBottom="0" connect="0" ignoreMargin="0"/>
      </hh:paraPr>
      <hh:paraPr id="2" tabPrIDRef="0" condense="0" fontLineHeight="0" snapToGrid="1" suppressLineNumbers="0" checked="0">
        <hh:align horizontal="RIGHT" vertical="BASELINE"/>
        <hh:heading type="NONE" idRef="0" level="0"/>
        <hh:breakSetting breakLatinWord="KEEP_WORD" breakNonLatinWord="KEEP_WORD" widowOrphan="0" keepWithNext="0" keepLines="0" pageBreakBefore="0" lineWrap="BREAK"/>
        <hh:autoSpacing eAsianEng="0" eAsianNum="0"/>
        <hh:margin><hc:intent value="0" unit="HWPUNIT"/><hc:left value="0" unit="HWPUNIT"/><hc:right value="0" unit="HWPUNIT"/><hc:prev value="0" unit="HWPUNIT"/><hc:next value="0" unit="HWPUNIT"/></hh:margin>
        <hh:lineSpacing type="PERCENT" value="130" unit="HWPUNIT"/>
        <hh:border borderFillIDRef="1" offsetLeft="0" offsetRight="0" offsetTop="0" offsetBottom="0" connect="0" ignoreMargin="0"/>
      </hh:paraPr>
    </hh:paraProperties>
    <hh:styles itemCnt="1">
      <hh:style id="0" type="PARA" name="바탕글" engName="Normal" paraPrIDRef="0" charPrIDRef="0" nextStyleIDRef="0" langID="1042" lockForm="0"/>
    </hh:styles>
  </hh:refList>
</hh:head>`, len(langs), fontBuf.String())
}

func buildSectionXML(data HWPXDocumentData) string {
	colCnt := len(data.Headers)
	if colCnt <= 0 {
		colCnt = 1
	}
	rows := data.Rows
	if len(rows) == 0 {
		emptyRow := make([]string, colCnt)
		emptyRow[0] = "해당 조건의 기록이 없습니다."
		rows = [][]string{emptyRow}
	}
	rowCnt := 1 + len(rows)
	const totalWidth = 48190 // A4(59528) - 좌우 여백(5669 * 2)
	colWidths := make([]int, colCnt)
	baseW := totalWidth / colCnt
	for i := 0; i < colCnt; i++ {
		colWidths[i] = baseW
	}
	colWidths[colCnt-1] += totalWidth - baseW*colCnt

	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	sb.WriteString(`<hs:sec xmlns:hs="http://www.hancom.co.kr/hwpml/2011/section" xmlns:hp="http://www.hancom.co.kr/hwpml/2011/paragraph" xmlns:hc="http://www.hancom.co.kr/hwpml/2011/core">`)

	// 첫 문단: A4 세로 페이지 정의(<hp:secPr>) + 다단(<hp:colPr>) + 문서 제목
	sb.WriteString(`<hp:p id="1" paraPrIDRef="1" styleIDRef="0" pageBreak="0" columnBreak="0" merged="0"><hp:run charPrIDRef="1">`)
	sb.WriteString(`<hp:secPr id="" textDirection="HORIZONTAL" spaceColumns="1134" tabStop="8000" tabStopVal="4000" tabStopUnit="HWPUNIT" outlineShapeIDRef="1" memoShapeIDRef="0" textVerticalWidthHead="0" masterPageCnt="0">`)
	sb.WriteString(`<hp:grid lineGrid="0" charGrid="0" wonggojiFormat="0"/>`)
	sb.WriteString(`<hp:startNum pageStartsOn="BOTH" page="0" pic="0" tbl="0" equation="0"/>`)
	sb.WriteString(`<hp:visibility hideFirstHeader="0" hideFirstFooter="0" hideFirstMasterPage="0" border="SHOW_ALL" fill="SHOW_ALL" hideFirstPageNum="0" hideFirstEmptyLine="0" showLineNumber="0"/>`)
	sb.WriteString(`<hp:pagePr landscape="WIDELY" width="59528" height="84186" gutterType="LEFT_ONLY"><hp:margin header="2834" footer="2834" gutter="0" left="5669" right="5669" top="5668" bottom="4252"/></hp:pagePr>`)
	sb.WriteString(`</hp:secPr>`)
	sb.WriteString(`<hp:ctrl><hp:colPr id="" type="NEWSPAPER" layout="LEFT" colCount="1" sameSz="1" sameGap="0"/></hp:ctrl>`)
	sb.WriteString(fmt.Sprintf(`<hp:t>[%s] %s</hp:t></hp:run></hp:p>`, escapeXML(data.SchoolName), escapeXML(data.DocTitle)))

	// 둘째 문단: 기준 기간·출력일시·결재란 요약
	sb.WriteString(fmt.Sprintf(`<hp:p id="2" paraPrIDRef="2" styleIDRef="0" pageBreak="0" columnBreak="0" merged="0"><hp:run charPrIDRef="2"><hp:t>%s | 출력: %s | 결재: [ 담당 ] ── [ 부장 ] ── [ 교감 ] ── [ 교장 ]</hp:t></hp:run></hp:p>`,
		escapeXML(data.PeriodText), time.Now().Format("2006-01-02 15:04")))

	// 본문 데이터 표 (<hp:tbl>)
	tblHeight := rowCnt * 1000
	sb.WriteString(fmt.Sprintf(`<hp:p id="3" paraPrIDRef="0" styleIDRef="0" pageBreak="0" columnBreak="0" merged="0"><hp:run charPrIDRef="0">`))
	sb.WriteString(fmt.Sprintf(`<hp:tbl id="10" zOrder="0" numberingType="TABLE" textWrap="TOP_AND_BOTTOM" textFlow="BOTH_SIDES" lock="0" dropcapstyle="None" pageBreak="CELL" repeatHeader="1" rowCnt="%d" colCnt="%d" cellSpacing="0" borderFillIDRef="2" noAdjust="0">`, rowCnt, colCnt))
	sb.WriteString(fmt.Sprintf(`<hp:sz width="%d" widthRelTo="ABSOLUTE" height="%d" heightRelTo="ABSOLUTE" protect="0"/>`, totalWidth, tblHeight))
	sb.WriteString(`<hp:pos treatAsChar="1" affectLSpacing="0" flowWithText="1" allowOverlap="0" holdAnchorAndSO="0" vertRelTo="PARA" horzRelTo="COLUMN" vertAlign="TOP" horzAlign="LEFT" vertOffset="0" horzOffset="0"/>`)
	sb.WriteString(`<hp:outMargin left="0" right="0" top="142" bottom="142"/>`)
	sb.WriteString(`<hp:inMargin left="283" right="283" top="142" bottom="142"/>`)

	nextPID := 100
	writeRow := func(rIdx int, cells []string, isHeader bool) {
		sb.WriteString(`<hp:tr>`)
		borderRef, charRef, paraRef, hdrFlag := 2, 0, 0, 0
		if isHeader {
			borderRef, charRef, paraRef, hdrFlag = 3, 3, 1, 1
		}
		for cIdx := 0; cIdx < colCnt; cIdx++ {
			val := ""
			if cIdx < len(cells) {
				val = cells[cIdx]
			}
			sb.WriteString(fmt.Sprintf(`<hp:tc name="" header="%d" hasMargin="1" protect="0" editable="0" dirty="0" borderFillIDRef="%d">`, hdrFlag, borderRef))
			sb.WriteString(fmt.Sprintf(`<hp:subList id="" textDirection="HORIZONTAL" lineWrap="BREAK" vertAlign="CENTER" linkListIDRef="0" linkListNextIDRef="0" textWidth="0" textHeight="0" hasTextRef="0" hasNumRef="0"><hp:p id="%d" paraPrIDRef="%d" styleIDRef="0" pageBreak="0" columnBreak="0" merged="0"><hp:run charPrIDRef="%d"><hp:t>%s</hp:t></hp:run></hp:p></hp:subList>`,
				nextPID, paraRef, charRef, escapeXML(val)))
			nextPID++
			sb.WriteString(fmt.Sprintf(`<hp:cellAddr colAddr="%d" rowAddr="%d"/><hp:cellSpan colSpan="1" rowSpan="1"/><hp:cellSz width="%d" height="1000"/><hp:cellMargin left="283" right="283" top="142" bottom="142"/>`,
				cIdx, rIdx, colWidths[cIdx]))
			sb.WriteString(`</hp:tc>`)
		}
		sb.WriteString(`</hp:tr>`)
	}

	hdrs := data.Headers
	if len(hdrs) == 0 {
		hdrs = []string{"내용"}
	}
	writeRow(0, hdrs, true)
	for rIdx, row := range rows {
		writeRow(rIdx+1, row, false)
	}
	sb.WriteString(`</hp:tbl><hp:t/></hp:run></hp:p></hs:sec>`)
	return sb.String()
}

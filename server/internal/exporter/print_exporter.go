package exporter

import (
	"fmt"
	"html"
	"strings"
	"time"
)

// BuildHighContrastPrintHTML은 교무실 대형 현황판(B4/A3 고대비 흑백 인쇄 및 PDF 변환용) HTML을 생성합니다.
func BuildHighContrastPrintHTML(title string, headers []string, rows [][]string) string {
	var sb strings.Builder
	sb.WriteString(`<!DOCTYPE html><html lang="ko"><head><meta charset="utf-8"><title>`)
	sb.WriteString(html.EscapeString(title))
	sb.WriteString(`</title><style>
@page { size: B4 landscape; margin: 10mm; }
body { font-family: "Malgun Gothic", "Apple SD Gothic Neo", sans-serif; color: #000; background: #fff; margin: 0; padding: 16px; }
h1 { font-size: 20px; border-bottom: 3px solid #000; padding-bottom: 6px; margin-bottom: 8px; }
.meta { font-size: 12px; margin-bottom: 12px; display: flex; justify-content: space-between; }
table { width: 100%; border-collapse: collapse; font-size: 13px; }
th, td { border: 1.5px solid #000; padding: 6px 8px; text-align: left; }
th { background: #000; color: #fff; font-weight: 700; }
tr:nth-child(even) td { background: #f2f2f2; }
@media print { .no-print { display: none; } }
</style></head><body>`)
	sb.WriteString(fmt.Sprintf(`<h1>%s</h1>`, html.EscapeString(title)))
	sb.WriteString(fmt.Sprintf(`<div class="meta"><span>출력 일시: %s</span><span>[담당] _______  [부장] _______  [교감] _______  [교장] _______</span><button class="no-print" onclick="window.print()">B4/A3 인쇄 / PDF 저장</button></div>`, time.Now().Format("2006-01-02 15:04")))
	sb.WriteString(`<table><thead><tr>`)
	for _, h := range headers {
		sb.WriteString(fmt.Sprintf(`<th>%s</th>`, html.EscapeString(h)))
	}
	sb.WriteString(`</tr></thead><tbody>`)
	for _, r := range rows {
		sb.WriteString(`<tr>`)
		for i := 0; i < len(headers); i++ {
			val := ""
			if i < len(r) {
				val = r[i]
			}
			sb.WriteString(fmt.Sprintf(`<td>%s</td>`, html.EscapeString(val)))
		}
		sb.WriteString(`</tr>`)
	}
	sb.WriteString(`</tbody></table></body></html>`)
	return sb.String()
}

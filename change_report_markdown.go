package forward

import (
	"fmt"
	"sort"
	"strings"
)

var changeReportTitles = map[string]string{
	"checks": "Checks", "flows": "Flows", "state": "State", "connectivity": "Connectivity", "routing": "Routing", "config": "Config",
	"devices": "Devices", "security_rules": "Security rules", "vulnerabilities": "Vulnerabilities", "diff_counts": "Diff counts",
	"predict": "Predict", "ai_summary": "AI summary",
}

// renderChangeReportMarkdown follows testdata/change_report/RULES.md, "Markdown": it reads only the report, blocks are separated by one
// blank line, and the text ends with exactly one newline.
func renderChangeReportMarkdown(doc map[string]any) string {
	var blocks []string
	meta := asMap(doc["meta"])
	blocks = append(blocks, "# Change report")
	intro := fmt.Sprintf("Base `%s` → after `%s` (%s), network `%s`", cell(meta["base_snapshot_id"]), cell(meta["after_snapshot_id"]), cell(meta["after_kind"]), cell(meta["network_id"]))
	if id := meta["change_set_id"]; id != nil {
		intro += fmt.Sprintf(", change set `%s`", cell(id))
	}
	blocks = append(blocks, intro+".")

	verdict := asMap(doc["verdict"])
	blocks = append(blocks, fmt.Sprintf("**Verdict: %s**", asString(verdict["result"])))
	var reasons []string
	for _, raw := range asList(verdict["reasons"]) {
		r := asMap(raw)
		reasons = append(reasons, fmt.Sprintf("- %s (%s): %s", asString(r["code"]), asString(r["section"]), asString(r["detail"])))
	}
	if len(reasons) != 0 {
		blocks = append(blocks, strings.Join(reasons, "\n"))
	}

	sections := asMap(doc["sections"])
	var footer []string
	for _, name := range changeReportSectionOrder {
		section := asMap(sections[name])
		title := changeReportTitles[name]
		switch asString(section["status"]) {
		case "ok":
			blocks = append(blocks, "## "+title)
			blocks = append(blocks, renderSectionBody(name, section)...)
			if section["truncated"] == true {
				blocks = append(blocks, fmt.Sprintf("Showing %d of %d.", shownItems(name, section), asInt(section["total"])))
			}
		case "unavailable":
			reason := asMap(section["reason"])
			footer = append(footer, fmt.Sprintf("- %s: unavailable (%s: %s)", title, asString(reason["kind"]), asString(reason["message"])))
		case "not_applicable":
			footer = append(footer, fmt.Sprintf("- %s: not applicable", title))
		case "not_requested":
			footer = append(footer, fmt.Sprintf("- %s: not requested", title))
		}
	}
	blocks = append(blocks, "---")
	if len(footer) != 0 {
		blocks = append(blocks, strings.Join(footer, "\n"))
	}
	sdk := asMap(meta["sdk"])
	blocks = append(blocks, fmt.Sprintf("Built from Forward's API by %s %s; not the Predict UI's report.", asString(sdk["name"]), asString(sdk["version"])))
	return strings.Join(blocks, "\n\n") + "\n"
}

func shownItems(name string, section map[string]any) int {
	switch name {
	case "devices":
		return len(asList(section["added"])) + len(asList(section["removed"])) + len(asList(section["modified"]))
	case "connectivity":
		pairs := asMap(section["location_pairs"])
		return len(asList(pairs["ADDED"])) + len(asList(pairs["DELETED"])) + len(asList(pairs["MODIFIED"]))
	}
	return len(asList(section["items"]))
}

// cell renders a value for a table cell or a line: null is "-", everything else as plain text, with "|" escaped for tables by the caller.
func cell(v any) string {
	switch t := v.(type) {
	case nil:
		return "-"
	case string:
		return t
	case bool:
		return fmt.Sprint(t)
	default:
		var b strings.Builder
		fmt.Fprint(&b, asIntOrText(t))
		return b.String()
	}
}

func asIntOrText(v any) any {
	switch t := v.(type) {
	case int, int64:
		return t
	}
	return fmt.Sprint(v)
}

func escapeCell(s string) string { return strings.ReplaceAll(s, "|", `\|`) }

func table(header []string, rows [][]string) string {
	var b strings.Builder
	b.WriteString("| " + strings.Join(header, " | ") + " |\n")
	sep := make([]string, len(header))
	for i := range sep {
		sep[i] = "---"
	}
	b.WriteString("| " + strings.Join(sep, " | ") + " |")
	for _, row := range rows {
		escaped := make([]string, len(row))
		for i, c := range row {
			escaped[i] = escapeCell(c)
		}
		b.WriteString("\n| " + strings.Join(escaped, " | ") + " |")
	}
	return b.String()
}

func rankIndex(order []string, v string) int {
	for i, o := range order {
		if o == v {
			return i
		}
	}
	return len(order)
}

func renderSectionBody(name string, section map[string]any) []string {
	items := asList(section["items"])
	switch name {
	case "checks":
		order := []string{"regressed", "other", "removed", "added", "fixed"}
		var rows []map[string]any
		for _, raw := range items {
			m := asMap(raw)
			if asString(m["change"]) != "unchanged" {
				rows = append(rows, m)
			}
		}
		sort.SliceStable(rows, func(i, j int) bool {
			ri, rj := rankIndex(order, asString(rows[i]["change"])), rankIndex(order, asString(rows[j]["change"]))
			if ri != rj {
				return ri < rj
			}
			return asString(rows[i]["check_id"]) < asString(rows[j]["check_id"])
		})
		var body []string
		if len(rows) == 0 {
			body = append(body, "No changes.")
		} else {
			var out [][]string
			for _, m := range rows {
				out = append(out, []string{cell(m["check_id"]), cell(m["name"]), cell(m["type"]), cell(m["base_status"]), cell(m["after_status"]), cell(m["change"])})
			}
			body = append(body, table([]string{"Check", "Name", "Type", "Base", "After", "Change"}, out))
		}
		c := asMap(section["counts"])
		body = append(body, fmt.Sprintf("Regressed %d, fixed %d, added %d, removed %d, unchanged %d, other %d; not evaluated %d, skipped %d, unlicensed %d.",
			asInt(c["regressed"]), asInt(c["fixed"]), asInt(c["added"]), asInt(c["removed"]), asInt(c["unchanged"]), asInt(c["other"]),
			asInt(c["not_evaluated"]), asInt(c["skipped"]), asInt(c["unlicensed"])))
		return body
	case "flows":
		order := []string{"lost_valid", "unavailable", "gained_valid", "counts_changed", "unchanged"}
		rows := make([]map[string]any, 0, len(items))
		for _, raw := range items {
			rows = append(rows, asMap(raw))
		}
		sort.SliceStable(rows, func(i, j int) bool {
			ri, rj := rankIndex(order, asString(rows[i]["change"])), rankIndex(order, asString(rows[j]["change"]))
			if ri != rj {
				return ri < rj
			}
			return asString(rows[i]["name"]) < asString(rows[j]["name"])
		})
		if len(rows) == 0 {
			return []string{"No changes."}
		}
		var out [][]string
		for _, m := range rows {
			out = append(out, []string{cell(m["name"]), cell(m["expectation"]), validCount(m["base"]), validCount(m["after"]), cell(m["change"])})
		}
		return []string{table([]string{"Flow", "Expectation", "Base valid", "After valid", "Change"}, out)}
	case "state":
		if len(items) == 0 {
			return []string{"No changes."}
		}
		order := []string{"lost", "changed", "gained"}
		rows := make([]map[string]any, 0, len(items))
		for _, raw := range items {
			rows = append(rows, asMap(raw))
		}
		sort.SliceStable(rows, func(i, j int) bool {
			ri, rj := rankIndex(order, asString(rows[i]["change"])), rankIndex(order, asString(rows[j]["change"]))
			if ri != rj {
				return ri < rj
			}
			if ki, kj := asString(rows[i]["kind"]), asString(rows[j]["kind"]); ki != kj {
				return ki < kj
			}
			return lessKey(identityKey(rows[i]["identity"]), identityKey(rows[j]["identity"]))
		})
		var out [][]string
		for _, m := range rows {
			expected := "no"
			if m["expected"] == true {
				expected = "yes"
			}
			out = append(out, []string{cell(m["kind"]), strings.Join(identityKey(m["identity"]), ", "), cell(m["base"]), cell(m["after"]), cell(m["change"]), expected})
		}
		return []string{table([]string{"Kind", "Identity", "Base", "After", "Change", "Expected"}, out)}
	case "connectivity":
		var body []string
		if section["no_forwarding_change"] == true {
			body = append(body, "No forwarding change.")
		} else {
			body = append(body, fmt.Sprintf("Subnet pairs evaluated: %d of %d.", asInt(section["evaluated_subnet_pairs"]), asInt(section["total_subnet_pairs"])))
		}
		pairs := asMap(section["location_pairs"])
		var out [][]string
		for _, kind := range []string{"DELETED", "MODIFIED", "ADDED"} {
			for _, raw := range asList(pairs[kind]) {
				m := asMap(raw)
				out = append(out, []string{kind, cell(m["src"]), cell(m["dst"]), cell(m["count"])})
			}
		}
		if len(out) == 0 {
			return append(body, "No location pair changes.")
		}
		return append(body, table([]string{"Change", "From", "To", "Subnet pairs"}, out))
	case "routing":
		if len(items) == 0 {
			return []string{"No changes."}
		}
		var out [][]string
		for _, raw := range items {
			m := asMap(raw)
			out = append(out, []string{cell(m["device"]), cell(m["added"]), cell(m["removed"]), cell(m["modified"])})
		}
		return []string{table([]string{"Device", "Added", "Removed", "Modified"}, out)}
	case "config":
		if len(items) == 0 {
			return []string{"No changes."}
		}
		var out [][]string
		for _, raw := range items {
			m := asMap(raw)
			var files []string
			for _, f := range asList(m["files_changed"]) {
				files = append(files, asString(f))
			}
			out = append(out, []string{cell(m["device"]), strings.Join(files, ", ")})
		}
		return []string{table([]string{"Device", "Files changed"}, out)}
	case "devices":
		line := func(label, key string) string {
			var names []string
			for _, v := range asList(section[key]) {
				names = append(names, asString(v))
			}
			if len(names) == 0 {
				return label + ": none."
			}
			return label + ": " + strings.Join(names, ", ") + "."
		}
		return []string{strings.Join([]string{line("Added", "added"), line("Removed", "removed"), line("Modified", "modified")}, "\n")}
	case "security_rules":
		if len(items) == 0 {
			return []string{"No changes."}
		}
		var out [][]string
		for _, raw := range items {
			m := asMap(raw)
			out = append(out, []string{cell(m["device"]), cell(m["rule"]), cell(m["change"])})
		}
		return []string{table([]string{"Device", "Rule", "Change"}, out)}
	case "vulnerabilities":
		return []string{fmt.Sprintf("Introduced %s, resolved %s, exposed devices %s.", cell(section["introduced"]), cell(section["resolved"]), cell(section["exposed_devices"]))}
	case "diff_counts":
		families := asMap(section["families"])
		if len(families) == 0 {
			return []string{"No changes."}
		}
		names := make([]string, 0, len(families))
		for n := range families {
			names = append(names, n)
		}
		sort.Strings(names)
		var out [][]string
		for _, n := range names {
			f := asMap(families[n])
			complete := "no (lower bound)"
			if f["complete"] == true {
				complete = "yes"
			}
			out = append(out, []string{n, cell(f["count"]), complete})
		}
		return []string{table([]string{"Family", "Count", "Complete"}, out)}
	case "predict":
		body := []string{fmt.Sprintf("Processing: %s.", cell(section["processing_state"]))}
		for _, list := range []struct{ label, key string }{{"Failures", "failures"}, {"Modeling problems", "modeling_problems"}} {
			rows := asList(section[list.key])
			if len(rows) == 0 {
				body = append(body, list.label+": none.")
				continue
			}
			var out [][]string
			for _, raw := range rows {
				m := asMap(raw)
				out = append(out, []string{cell(m["device"]), cell(m["message"])})
			}
			body = append(body, list.label+":", table([]string{"Device", "Message"}, out))
		}
		return body
	case "ai_summary":
		return []string{asString(section["text"])}
	}
	return nil
}

func validCount(v any) string {
	m := asMap(v)
	if m == nil {
		return "-"
	}
	return cell(m["valid"])
}

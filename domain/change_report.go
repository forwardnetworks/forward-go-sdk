package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ChangeReport is a report comparing two snapshots of a network, a base and an after: a predicted snapshot (Predict) or a later
// collection. It is assembled from reads this SDK already has, never from a route of its own, and it makes no claim to match the Predict UI's
// report. The shape and the rules that build it are shared with forward-python-sdk: testdata/change_report/RULES.md and schema.json (schema_version
// "1"), pinned by testdata/change_report/MANIFEST.sha256 and tested byte for byte against the golden files there. Preview.
//
// AssembleChangeReport is the PURE step: it takes the section data already read (and what happened when a section could not be read) and
// applies the rules. Reading the sections from Forward is separate.
type ChangeReport struct {
	doc map[string]any
}

// Change report verdicts.
const (
	ChangeReportRegression   = "regression"
	ChangeReportNoRegression = "no_regression"
	ChangeReportIncomplete   = "incomplete"
)

// Verdict is "regression", "no_regression" or "incomplete" (a read that drives the verdict could not be completed).
func (r *ChangeReport) Verdict() string {
	if r == nil {
		return ""
	}
	return asString(asMap(r.doc["verdict"])["result"])
}

// ChangeReportReason is one reason behind a verdict.
type ChangeReportReason struct {
	Code    string `json:"code"`
	Section string `json:"section"`
	Detail  string `json:"detail"`
}

// Reasons lists the reasons behind the verdict, sorted by section, code and detail.
func (r *ChangeReport) Reasons() []ChangeReportReason {
	if r == nil {
		return nil
	}
	var out []ChangeReportReason
	for _, item := range asList(asMap(r.doc["verdict"])["reasons"]) {
		m := asMap(item)
		out = append(out, ChangeReportReason{Code: asString(m["code"]), Section: asString(m["section"]), Detail: asString(m["detail"])})
	}
	return out
}

// JSON is the report as JSON: keys sorted, two-space indent, UTF-8 with only the JSON-required escapes, a final newline. The same input
// gives the same bytes in both SDKs.
func (r *ChangeReport) JSON() []byte {
	var buf bytes.Buffer
	writeSortedJSON(&buf, r.doc, 0)
	buf.WriteByte('\n')
	return buf.Bytes()
}

// Markdown renders the report for a pull request comment or a ticket, from the report alone (see RULES.md, "Markdown").
func (r *ChangeReport) Markdown() string { return renderChangeReportMarkdown(r.doc) }

// ParseChangeReport reads a report written by JSON back, for rendering it again.
func ParseChangeReport(data []byte) (*ChangeReport, error) {
	doc, err := decodeReportTree(data)
	if err != nil {
		return nil, err
	}
	m, ok := doc.(map[string]any)
	if !ok {
		return nil, errors.New("forward: a change report is a JSON object")
	}
	return &ChangeReport{doc: m}, nil
}

func decodeReportTree(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var out any
	if err := decoder.Decode(&out); err != nil {
		return nil, fmt.Errorf("forward: decode change report JSON: %w", err)
	}
	if _, err := decoder.Token(); err == nil {
		return nil, errors.New("forward: unexpected data after the change report JSON")
	}
	return out, nil
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func asInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return 0
}

func writeSortedJSON(buf *bytes.Buffer, v any, depth int) {
	pad := strings.Repeat("  ", depth+1)
	end := strings.Repeat("  ", depth)
	switch t := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		buf.WriteString(strconv.FormatBool(t))
	case string:
		writeJSONString(buf, t)
	case int:
		buf.WriteString(strconv.Itoa(t))
	case int64:
		buf.WriteString(strconv.FormatInt(t, 10))
	case json.Number:
		buf.WriteString(t.String())
	case []string:
		list := make([]any, len(t))
		for i, s := range t {
			list[i] = s
		}
		writeSortedJSON(buf, list, depth)
	case []any:
		if len(t) == 0 {
			buf.WriteString("[]")
			return
		}
		buf.WriteString("[\n")
		for i, item := range t {
			buf.WriteString(pad)
			writeSortedJSON(buf, item, depth+1)
			if i < len(t)-1 {
				buf.WriteByte(',')
			}
			buf.WriteByte('\n')
		}
		buf.WriteString(end + "]")
	case map[string]any:
		if len(t) == 0 {
			buf.WriteString("{}")
			return
		}
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf.WriteString("{\n")
		for i, k := range keys {
			buf.WriteString(pad)
			writeJSONString(buf, k)
			buf.WriteString(": ")
			writeSortedJSON(buf, t[k], depth+1)
			if i < len(keys)-1 {
				buf.WriteByte(',')
			}
			buf.WriteByte('\n')
		}
		buf.WriteString(end + "}")
	default:
		panic(fmt.Sprintf("forward: change report holds an unsupported value of type %T", v))
	}
}

// writeJSONString escapes only what JSON requires: the quote, the backslash and the control characters (short forms for \b \f \n \r \t,
// \u00xx lower-case otherwise). Everything else, including non-ASCII text and the characters Go's encoder escapes for HTML, is written
// as UTF-8, so the bytes match a serializer that does the same.
func writeJSONString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			buf.WriteString(`\"`)
		case r == '\\':
			buf.WriteString(`\\`)
		case r == '\n':
			buf.WriteString(`\n`)
		case r == '\r':
			buf.WriteString(`\r`)
		case r == '\t':
			buf.WriteString(`\t`)
		case r == '\b':
			buf.WriteString(`\b`)
		case r == '\f':
			buf.WriteString(`\f`)
		case r < 0x20:
			fmt.Fprintf(buf, `\u%04x`, r)
		case r == utf8.RuneError:
			buf.WriteRune(r)
		default:
			buf.WriteRune(r)
		}
	}
	buf.WriteByte('"')
}

var changeReportSectionOrder = []string{"checks", "flows", "state", "connectivity", "routing", "config", "devices", "security_rules", "vulnerabilities", "diff_counts", "predict", "ai_summary"}

var changeReportOptIn = map[string]bool{"state": true, "flows": true, "ai_summary": true}

var changeReportPredictedOnly = map[string]bool{"predict": true, "security_rules": true}

// AssembleChangeReport applies RULES.md to input, a JSON document with "meta", "options" and "sections" (see testdata/change_report/<case>/input.json),
// and returns the report. It never reads Forward. An input that breaks the rules, such as a check in a non-terminal status or a missing required
// section, is an error rather than a report.
func AssembleChangeReport(input []byte) (*ChangeReport, error) {
	tree, err := decodeReportTree(input)
	if err != nil {
		return nil, err
	}
	in := asMap(tree)
	if in == nil {
		return nil, errors.New("forward: a change report input is a JSON object")
	}
	meta, options, inSections := asMap(in["meta"]), asMap(in["options"]), asMap(in["sections"])
	if meta == nil || options == nil || inSections == nil {
		return nil, errors.New("forward: a change report input needs meta, options and sections")
	}
	maxItems := 100
	if v, ok := options["max_items"]; ok {
		maxItems = asInt(v)
		if maxItems < 1 {
			return nil, errors.New("forward: options.max_items must be at least 1")
		}
	}
	sdk := asMap(options["sdk"])
	if sdk == nil || asString(sdk["name"]) == "" {
		return nil, errors.New("forward: options.sdk (name, version) is required")
	}
	afterKind := asString(meta["after_kind"])
	if afterKind != "predicted" && afterKind != "collected" {
		return nil, fmt.Errorf("forward: meta.after_kind %q must be predicted or collected", afterKind)
	}

	expectedRemoved := map[string]bool{}
	for _, v := range asList(asMap(options["expectations"])["state_expected_removed"]) {
		expectedRemoved[asString(v)] = true
	}

	a := &assembler{maxItems: maxItems, expectedRemoved: expectedRemoved}
	sections := map[string]any{}
	for _, name := range changeReportSectionOrder {
		raw, present := inSections[name]
		switch {
		case !present && changeReportOptIn[name]:
			sections[name] = map[string]any{"status": "not_requested"}
			continue
		case !present && changeReportPredictedOnly[name] && afterKind == "collected":
			sections[name] = map[string]any{"status": "not_applicable"}
			continue
		case !present:
			return nil, fmt.Errorf("forward: the input has no %s section", name)
		}
		section := asMap(raw)
		if section == nil {
			return nil, fmt.Errorf("forward: section %s is not an object", name)
		}
		if changeReportPredictedOnly[name] && afterKind == "collected" {
			return nil, fmt.Errorf("forward: section %s is for a predicted after snapshot only", name)
		}
		status := asString(section["status"])
		if status == "unavailable" {
			reason := asMap(section["reason"])
			if reason == nil || asString(reason["kind"]) == "" {
				return nil, fmt.Errorf("forward: unavailable section %s needs a reason with a kind", name)
			}
			sections[name] = map[string]any{"status": "unavailable", "reason": map[string]any{"kind": asString(reason["kind"]), "message": asString(reason["message"])}}
			if name == "checks" || (changeReportOptIn[name] && name != "ai_summary") {
				a.reasons = append(a.reasons, reasonRow("section_unavailable", name, asString(reason["kind"])))
			}
			continue
		}
		if status != "ok" {
			return nil, fmt.Errorf("forward: section %s has status %q in the input", name, status)
		}
		built, err := a.build(name, section)
		if err != nil {
			return nil, fmt.Errorf("forward: section %s: %w", name, err)
		}
		sections[name] = built
	}

	result := ChangeReportNoRegression
	reasons := a.reasons
	hasIncomplete, hasRegression := false, false
	for _, r := range reasons {
		switch asString(r["code"]) {
		case "section_unavailable", "check_not_evaluated", "flow_unavailable":
			hasIncomplete = true
		default:
			hasRegression = true
		}
	}
	switch {
	case hasIncomplete:
		result = ChangeReportIncomplete
	case hasRegression:
		result = ChangeReportRegression
	}
	sort.SliceStable(reasons, func(i, j int) bool {
		x, y := reasons[i], reasons[j]
		if sx, sy := asString(x["section"]), asString(y["section"]); sx != sy {
			return sx < sy
		}
		if cx, cy := asString(x["code"]), asString(y["code"]); cx != cy {
			return cx < cy
		}
		return asString(x["detail"]) < asString(y["detail"])
	})
	reasonList := make([]any, len(reasons))
	for i, r := range reasons {
		reasonList[i] = r
	}

	doc := map[string]any{
		"meta": map[string]any{
			"schema_version":    "1",
			"base_snapshot_id":  meta["base_snapshot_id"],
			"after_snapshot_id": meta["after_snapshot_id"],
			"after_kind":        afterKind,
			"change_set_id":     meta["change_set_id"],
			"network_id":        meta["network_id"],
			"created_at":        options["created_at"],
			"sdk":               map[string]any{"name": sdk["name"], "version": sdk["version"]},
		},
		"sections": sections,
		"verdict":  map[string]any{"result": result, "reasons": reasonList},
	}
	return &ChangeReport{doc: doc}, nil
}

func reasonRow(code, section, detail string) map[string]any {
	return map[string]any{"code": code, "section": section, "detail": detail}
}

type assembler struct {
	maxItems        int
	expectedRemoved map[string]bool
	reasons         []map[string]any
}

// cap keeps the first max items of an already sorted list and says whether anything was dropped.
func (a *assembler) cap(items []any) ([]any, bool) {
	if len(items) > a.maxItems {
		return items[:a.maxItems], true
	}
	if items == nil {
		items = []any{}
	}
	return items, false
}

func (a *assembler) build(name string, in map[string]any) (map[string]any, error) {
	switch name {
	case "checks":
		return a.checks(in)
	case "flows":
		return a.flows(in)
	case "state":
		return a.state(in)
	case "connectivity":
		return a.connectivity(in)
	case "routing":
		return a.sortedItems(in, func(m map[string]any) []string { return []string{asString(m["device"])} })
	case "config":
		out, err := a.sortedItems(in, func(m map[string]any) []string { return []string{asString(m["device"])} })
		if err != nil {
			return nil, err
		}
		for _, raw := range asList(out["items"]) {
			row := asMap(raw)
			files := make([]string, 0)
			for _, f := range asList(row["files_changed"]) {
				files = append(files, asString(f))
			}
			sort.Strings(files)
			list := make([]any, len(files))
			for i, f := range files {
				list[i] = f
			}
			row["files_changed"] = list
		}
		return out, nil
	case "security_rules":
		return a.sortedItems(in, func(m map[string]any) []string { return []string{asString(m["device"]), asString(m["rule"])} })
	case "devices":
		return a.devices(in)
	case "diff_counts":
		return map[string]any{"status": "ok", "families": nonNilMap(in["families"])}, nil
	case "vulnerabilities":
		return map[string]any{"status": "ok", "introduced": in["introduced"], "resolved": in["resolved"], "exposed_devices": in["exposed_devices"]}, nil
	case "predict":
		return map[string]any{"status": "ok", "processing_state": in["processing_state"], "failures": nonNilList(in["failures"]), "modeling_problems": nonNilList(in["modeling_problems"])}, nil
	case "ai_summary":
		text := asString(in["text"])
		if text == "" || strings.HasSuffix(text, "\n") {
			return nil, errors.New("text must be non-empty and must not end with a newline")
		}
		return map[string]any{"status": "ok", "text": in["text"]}, nil
	}
	return nil, errors.New("unknown section")
}

func nonNilMap(v any) map[string]any {
	if m := asMap(v); m != nil {
		return m
	}
	return map[string]any{}
}

func nonNilList(v any) []any {
	if l := asList(v); l != nil {
		return l
	}
	return []any{}
}

// sortedItems sorts the section's items bytewise by key, caps them, and adds total and truncated.
func (a *assembler) sortedItems(in map[string]any, key func(map[string]any) []string) (map[string]any, error) {
	items := append([]any(nil), asList(in["items"])...)
	sort.SliceStable(items, func(i, j int) bool { return lessKey(key(asMap(items[i])), key(asMap(items[j]))) })
	if err := rejectDuplicates(items, key); err != nil {
		return nil, err
	}
	total := len(items)
	kept, truncated := a.cap(items)
	return map[string]any{"status": "ok", "items": kept, "total": total, "truncated": truncated}, nil
}

// rejectDuplicates makes two items with one identity an input error: the order of ties would otherwise depend on the sort.
func rejectDuplicates(sorted []any, key func(map[string]any) []string) error {
	for i := 1; i < len(sorted); i++ {
		a, b := key(asMap(sorted[i-1])), key(asMap(sorted[i]))
		if !lessKey(a, b) && !lessKey(b, a) {
			return fmt.Errorf("duplicate identity %s", strings.Join(b, "|"))
		}
	}
	return nil
}

func rankOf(order []string, v string) int {
	for i, o := range order {
		if o == v {
			return i
		}
	}
	return len(order)
}

func lessKey(x, y []string) bool {
	for i := 0; i < len(x) && i < len(y); i++ {
		if x[i] != y[i] {
			return x[i] < y[i]
		}
	}
	return len(x) < len(y)
}

// checkInputStatuses are the check statuses assemble accepts. Only PROCESSING is still computing and is an input error: the reader waits for it
// or marks the section unavailable. NONE and REQUIRES_ADDITIONAL_SNAPSHOT_PROCESSING are judged per check (RULES.md, "Checks"): the second needs
// advanced reachability that may never run (on-demand mode), so waiting does not clear it.
var checkInputStatuses = map[string]bool{
	"PASS": true, "FAIL": true, "ERROR": true, "TIMEOUT": true, "UNLICENSED": true, "SKIPPED": true,
	"NONE": true, "REQUIRES_ADDITIONAL_SNAPSHOT_PROCESSING": true,
}

// checkNotEvaluated reports an after status that means the check could not be evaluated.
func checkNotEvaluated(status string) bool {
	return status == "TIMEOUT" || status == "REQUIRES_ADDITIONAL_SNAPSHOT_PROCESSING" || status == "NONE"
}

func (a *assembler) checks(in map[string]any) (map[string]any, error) {
	counts := map[string]int{"regressed": 0, "fixed": 0, "added": 0, "removed": 0, "unchanged": 0, "other": 0, "not_evaluated": 0, "skipped": 0, "unlicensed": 0}
	items := make([]any, 0)
	for _, raw := range asList(in["items"]) {
		row := asMap(raw)
		base, after := row["base_status"], row["after_status"]
		for _, s := range []any{base, after} {
			if s != nil && !checkInputStatuses[asString(s)] {
				return nil, fmt.Errorf("check %s has the status %q, which assemble does not accept (PROCESSING is still computing: the reader waits or marks the section unavailable)", asString(row["check_id"]), asString(s))
			}
		}
		if base == nil && after == nil {
			return nil, fmt.Errorf("check %s has neither a base nor an after status", asString(row["check_id"]))
		}
		bs, as := asString(base), asString(after)
		change := "other"
		failed := func(s string) bool { return s == "FAIL" || s == "ERROR" }
		switch {
		case base == nil:
			change = "added"
		case after == nil:
			change = "removed"
		case bs == as:
			change = "unchanged"
		case bs == "PASS" && failed(as):
			change = "regressed"
		case failed(bs) && as == "PASS":
			change = "fixed"
		}
		counts[change]++
		if checkNotEvaluated(as) {
			counts["not_evaluated"]++
		}
		switch as {
		case "SKIPPED":
			counts["skipped"]++
		case "UNLICENSED":
			counts["unlicensed"]++
		}
		id := asString(row["check_id"])
		if change == "regressed" {
			a.reasons = append(a.reasons, reasonRow("check_regressed", "checks", id))
		}
		if bs == "PASS" && checkNotEvaluated(as) {
			a.reasons = append(a.reasons, reasonRow("check_not_evaluated", "checks", id))
		}
		items = append(items, map[string]any{"check_id": id, "name": row["name"], "type": row["type"], "base_status": base, "after_status": after, "change": change})
	}
	checkID := func(m map[string]any) []string { return []string{asString(m["check_id"])} }
	sort.SliceStable(items, func(i, j int) bool { return lessKey(checkID(asMap(items[i])), checkID(asMap(items[j]))) })
	if err := rejectDuplicates(items, checkID); err != nil {
		return nil, err
	}
	checkRank := []string{"regressed", "other", "removed", "added", "fixed", "unchanged"}
	sort.SliceStable(items, func(i, j int) bool {
		return rankOf(checkRank, asString(asMap(items[i])["change"])) < rankOf(checkRank, asString(asMap(items[j])["change"]))
	})
	total := len(items)
	kept, truncated := a.cap(items)
	countOut := map[string]any{}
	for k, v := range counts {
		countOut[k] = v
	}
	return map[string]any{"status": "ok", "items": kept, "counts": countOut, "total": total, "truncated": truncated}, nil
}

func (a *assembler) flows(in map[string]any) (map[string]any, error) {
	items := make([]any, 0)
	for _, raw := range asList(in["items"]) {
		row := asMap(raw)
		name := asString(row["name"])
		switch asString(row["expectation"]) {
		case "valid", "none", "any":
		default:
			return nil, fmt.Errorf("flow %s: expectation %q must be valid, none or any", name, asString(row["expectation"]))
		}
		out := map[string]any{"name": name, "expectation": row["expectation"], "base": nil, "after": nil, "unavailable": nil, "change": "unchanged"}
		if un := asMap(row["unavailable"]); un != nil {
			out["unavailable"] = map[string]any{"kind": asString(un["kind"]), "message": asString(un["message"])}
			out["change"] = "unavailable"
			a.reasons = append(a.reasons, reasonRow("flow_unavailable", "flows", name))
			items = append(items, out)
			continue
		}
		base, after := asMap(row["base"]), asMap(row["after"])
		if base == nil || after == nil {
			return nil, fmt.Errorf("flow %s needs base and after counts or an unavailable reason", name)
		}
		out["base"], out["after"] = base, after
		bv, av := asInt(base["valid"]), asInt(after["valid"])
		switch {
		case bv > 0 && av == 0:
			out["change"] = "lost_valid"
			if asString(row["expectation"]) == "valid" {
				a.reasons = append(a.reasons, reasonRow("flow_lost_valid_path", "flows", name))
			}
		case bv == 0 && av > 0:
			out["change"] = "gained_valid"
		default:
			for _, bucket := range []string{"valid", "dropped", "blackhole", "unreachable", "loop", "other"} {
				if asInt(base[bucket]) != asInt(after[bucket]) {
					out["change"] = "counts_changed"
					break
				}
			}
		}
		items = append(items, out)
	}
	flowName := func(m map[string]any) []string { return []string{asString(m["name"])} }
	sort.SliceStable(items, func(i, j int) bool { return lessKey(flowName(asMap(items[i])), flowName(asMap(items[j]))) })
	if err := rejectDuplicates(items, flowName); err != nil {
		return nil, err
	}
	flowRank := []string{"lost_valid", "unavailable", "gained_valid", "counts_changed", "unchanged"}
	sort.SliceStable(items, func(i, j int) bool {
		return rankOf(flowRank, asString(asMap(items[i])["change"])) < rankOf(flowRank, asString(asMap(items[j])["change"]))
	})
	total := len(items)
	kept, truncated := a.cap(items)
	return map[string]any{"status": "ok", "items": kept, "total": total, "truncated": truncated}, nil
}

func identityKey(identity any) []string {
	var out []string
	for _, v := range asList(identity) {
		out = append(out, asString(v))
	}
	return out
}

func (a *assembler) state(in map[string]any) (map[string]any, error) {
	type entry struct {
		kind     string
		identity []any
		value    any
	}
	index := func(list any) (map[string]entry, error) {
		out := map[string]entry{}
		for _, raw := range asList(list) {
			row := asMap(raw)
			e := entry{kind: asString(row["kind"]), identity: asList(row["identity"]), value: row["value"]}
			key := e.kind + ":" + strings.Join(identityKey(e.identity), "|")
			if _, dup := out[key]; dup {
				return nil, fmt.Errorf("state item %s appears twice", key)
			}
			out[key] = e
		}
		return out, nil
	}
	base, err := index(in["base"])
	if err != nil {
		return nil, err
	}
	after, err := index(in["after"])
	if err != nil {
		return nil, err
	}
	keys := map[string]bool{}
	for k := range base {
		keys[k] = true
	}
	for k := range after {
		keys[k] = true
	}
	items := make([]any, 0)
	for key := range keys {
		b, hadBase := base[key]
		af, hasAfter := after[key]
		item := map[string]any{"base": nil, "after": nil, "expected": a.expectedRemoved[key]}
		kind, identity := b.kind, b.identity
		if !hadBase {
			kind, identity = af.kind, af.identity
		}
		if hadBase {
			item["base"] = b.value
		}
		if hasAfter {
			item["after"] = af.value
		}
		// Classification order (RULES.md): lost by the kind's own rule, then gained (absent on base), then changed (any other difference).
		var change string
		switch {
		case hadBase && lostByKind(kind, b.value, hasAfter, af.value):
			change = "lost"
		case !hadBase:
			change = "gained"
		case hasAfter && sameValue(b.value, af.value):
			continue
		default:
			change = "changed"
		}
		item["change"], item["kind"], item["identity"] = change, kind, identity
		if change == "lost" && !a.expectedRemoved[key] {
			a.reasons = append(a.reasons, reasonRow("state_lost", "state", key))
		}
		items = append(items, item)
	}
	stateRank := []string{"lost", "changed", "gained"}
	sort.SliceStable(items, func(i, j int) bool {
		x, y := asMap(items[i]), asMap(items[j])
		if rx, ry := rankOf(stateRank, asString(x["change"])), rankOf(stateRank, asString(y["change"])); rx != ry {
			return rx < ry
		}
		if kx, ky := asString(x["kind"]), asString(y["kind"]); kx != ky {
			return kx < ky
		}
		return lessKey(identityKey(x["identity"]), identityKey(y["identity"]))
	})
	total := len(items)
	kept, truncated := a.cap(items)
	return map[string]any{"status": "ok", "items": kept, "total": total, "truncated": truncated}, nil
}

// lostByKind is the per-kind "lost" rule: a BGP session that was ESTABLISHED and is absent or not ESTABLISHED, an LLDP adjacency present on
// the base and absent on the after, an interface that was AdminStatus.UP and is absent or anything else. An ACL entry count is never lost.
func lostByKind(kind string, baseValue any, hasAfter bool, afterValue any) bool {
	switch kind {
	case "bgp_session":
		return asString(baseValue) == "ESTABLISHED" && (!hasAfter || asString(afterValue) != "ESTABLISHED")
	case "interface_admin":
		return asString(baseValue) == "AdminStatus.UP" && (!hasAfter || asString(afterValue) != "AdminStatus.UP")
	case "lldp_adjacency":
		return !hasAfter
	}
	return false
}

func sameValue(x, y any) bool {
	var xb, yb bytes.Buffer
	writeSortedJSON(&xb, x, 0)
	writeSortedJSON(&yb, y, 0)
	return xb.String() == yb.String()
}

func (a *assembler) connectivity(in map[string]any) (map[string]any, error) {
	pairs := asMap(in["location_pairs"])
	out := map[string]any{}
	total, truncated := 0, false
	for _, kind := range []string{"ADDED", "DELETED", "MODIFIED"} {
		list := append([]any(nil), asList(pairs[kind])...)
		sort.SliceStable(list, func(i, j int) bool {
			x, y := asMap(list[i]), asMap(list[j])
			return lessKey([]string{asString(x["src"]), asString(x["dst"])}, []string{asString(y["src"]), asString(y["dst"])})
		})
		pairKey := func(m map[string]any) []string { return []string{asString(m["src"]), asString(m["dst"])} }
		if err := rejectDuplicates(list, pairKey); err != nil {
			return nil, fmt.Errorf("%s location pairs: %w", kind, err)
		}
		total += len(list)
		kept, dropped := a.cap(list)
		truncated = truncated || dropped
		out[kind] = kept
	}
	totalPairs := asInt(in["total_subnet_pairs"])
	return map[string]any{
		"status": "ok", "evaluated_subnet_pairs": in["evaluated_subnet_pairs"], "total_subnet_pairs": in["total_subnet_pairs"],
		"no_forwarding_change": totalPairs == 0, "location_pairs": out, "total": total, "truncated": truncated,
	}, nil
}

func (a *assembler) devices(in map[string]any) (map[string]any, error) {
	out := map[string]any{"status": "ok"}
	total, truncated := 0, false
	for _, kind := range []string{"added", "removed", "modified"} {
		var names []string
		for _, v := range asList(in[kind]) {
			names = append(names, asString(v))
		}
		sort.Strings(names)
		for i := 1; i < len(names); i++ {
			if names[i] == names[i-1] {
				return nil, fmt.Errorf("device %s appears twice in %s", names[i], kind)
			}
		}
		list := make([]any, len(names))
		for i, n := range names {
			list[i] = n
		}
		total += len(list)
		kept, dropped := a.cap(list)
		truncated = truncated || dropped
		out[kind] = kept
	}
	out["total"], out["truncated"] = total, truncated
	return out, nil
}

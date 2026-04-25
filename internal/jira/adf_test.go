package jira

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderADF_TextWithLink(t *testing.T) {
	body := `{
		"type":"doc",
		"content":[
			{"type":"paragraph","content":[
				{"type":"text","text":"see ","marks":[]},
				{"type":"text","text":"docs","marks":[{"type":"link","attrs":{"href":"https://example.com"}}]}
			]}
		]
	}`
	var n adfNode
	if err := json.Unmarshal([]byte(body), &n); err != nil {
		t.Fatal(err)
	}
	out := renderADF(n)
	want := "see [docs](https://example.com)"
	if !strings.Contains(out, want) {
		t.Fatalf("expected %q in:\n%q", want, out)
	}
}

func TestRenderADF_LinkWithEmptyHrefRendersBareText(t *testing.T) {
	body := `{
		"type":"doc",
		"content":[
			{"type":"paragraph","content":[
				{"type":"text","text":"orphan","marks":[{"type":"link","attrs":{}}]}
			]}
		]
	}`
	var n adfNode
	if err := json.Unmarshal([]byte(body), &n); err != nil {
		t.Fatal(err)
	}
	out := renderADF(n)
	if strings.Contains(out, "[orphan]") {
		t.Fatalf("link with empty href should render as plain text; got %q", out)
	}
	if !strings.Contains(out, "orphan") {
		t.Fatalf("text missing from output: %q", out)
	}
}

func TestRenderADF_MentionUsesAttrText(t *testing.T) {
	body := `{
		"type":"doc",
		"content":[
			{"type":"paragraph","content":[
				{"type":"mention","attrs":{"text":"@Alice"}}
			]}
		]
	}`
	var n adfNode
	if err := json.Unmarshal([]byte(body), &n); err != nil {
		t.Fatal(err)
	}
	if got := renderADF(n); !strings.Contains(got, "@Alice") {
		t.Fatalf("expected '@Alice' in:\n%q", got)
	}
}

func TestRenderADF_MentionFallsBackToTextField(t *testing.T) {
	body := `{
		"type":"doc",
		"content":[
			{"type":"paragraph","content":[
				{"type":"mention","text":"Bob"}
			]}
		]
	}`
	var n adfNode
	if err := json.Unmarshal([]byte(body), &n); err != nil {
		t.Fatal(err)
	}
	if got := renderADF(n); !strings.Contains(got, "@Bob") {
		t.Fatalf("expected '@Bob' fallback, got %q", got)
	}
}

func TestRenderADF_AllInlineMarks(t *testing.T) {
	body := `{
		"type":"doc",
		"content":[
			{"type":"paragraph","content":[
				{"type":"text","text":"a","marks":[{"type":"strong"}]},
				{"type":"text","text":"b","marks":[{"type":"em"}]},
				{"type":"text","text":"c","marks":[{"type":"strike"}]},
				{"type":"text","text":"d","marks":[{"type":"code"}]}
			]}
		]
	}`
	var n adfNode
	if err := json.Unmarshal([]byte(body), &n); err != nil {
		t.Fatal(err)
	}
	out := renderADF(n)
	for _, want := range []string{"**a**", "*b*", "~~c~~", "`d`"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in:\n%q", want, out)
		}
	}
}

func TestRenderADF_HeadingLevelsClampToValidRange(t *testing.T) {
	cases := []struct {
		level int
		want  string
	}{
		{0, "# "},   // clamps up to 1
		{1, "# "},
		{6, "###### "},
		{9, "###### "}, // clamps down to 6
	}
	for _, tc := range cases {
		body := `{"type":"doc","content":[{"type":"heading","attrs":{"level":` + itoa(tc.level) + `},"content":[{"type":"text","text":"Title"}]}]}`
		var n adfNode
		if err := json.Unmarshal([]byte(body), &n); err != nil {
			t.Fatal(err)
		}
		got := renderADF(n)
		if !strings.HasPrefix(got, tc.want) {
			t.Fatalf("level=%d expected prefix %q in %q", tc.level, tc.want, got)
		}
	}
}

func TestRenderADF_OrderedAndBulletLists(t *testing.T) {
	body := `{
		"type":"doc",
		"content":[
			{"type":"orderedList","content":[
				{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"first"}]}]},
				{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"second"}]}]}
			]},
			{"type":"bulletList","content":[
				{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"alpha"}]}]}
			]}
		]
	}`
	var n adfNode
	if err := json.Unmarshal([]byte(body), &n); err != nil {
		t.Fatal(err)
	}
	out := renderADF(n)
	for _, want := range []string{"1. first", "1. second", "- alpha"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in:\n%q", want, out)
		}
	}
}

func TestRenderADF_CodeBlockFenced(t *testing.T) {
	body := `{
		"type":"doc",
		"content":[
			{"type":"codeBlock","content":[{"type":"text","text":"go test ./..."}]}
		]
	}`
	var n adfNode
	if err := json.Unmarshal([]byte(body), &n); err != nil {
		t.Fatal(err)
	}
	out := renderADF(n)
	if !strings.Contains(out, "```\ngo test ./...\n```") {
		t.Fatalf("codeBlock not fenced as expected: %q", out)
	}
}

func TestRenderADF_HardBreakInsertsTwoSpacesNewline(t *testing.T) {
	body := `{
		"type":"doc",
		"content":[
			{"type":"paragraph","content":[
				{"type":"text","text":"line1"},
				{"type":"hardBreak"},
				{"type":"text","text":"line2"}
			]}
		]
	}`
	var n adfNode
	if err := json.Unmarshal([]byte(body), &n); err != nil {
		t.Fatal(err)
	}
	if got := renderADF(n); !strings.Contains(got, "line1  \nline2") {
		t.Fatalf("hardBreak not rendered as '  \\n': %q", got)
	}
}

func TestRenderADF_UnknownNodeFallsThroughToChildren(t *testing.T) {
	body := `{
		"type":"unknownContainer",
		"content":[
			{"type":"paragraph","content":[{"type":"text","text":"survives"}]}
		]
	}`
	var n adfNode
	if err := json.Unmarshal([]byte(body), &n); err != nil {
		t.Fatal(err)
	}
	if got := renderADF(n); !strings.Contains(got, "survives") {
		t.Fatalf("unknown node should still render children, got %q", got)
	}
}

// itoa is local to this test file (package jira already imports strconv via
// other source files, but reaching for it here for a single 1-byte constant
// would be overkill).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := []byte{}
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}

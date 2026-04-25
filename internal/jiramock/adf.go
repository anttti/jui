package jiramock

import "encoding/json"

// adfDoc is the minimal Atlassian Document Format envelope. See
// https://developer.atlassian.com/cloud/jira/platform/apis/document/structure/.
type adfDoc struct {
	Type    string    `json:"type"`
	Version int       `json:"version"`
	Content []adfNode `json:"content"`
}

type adfNode struct {
	Type    string         `json:"type"`
	Attrs   map[string]any `json:"attrs,omitempty"`
	Text    string         `json:"text,omitempty"`
	Marks   []adfMark      `json:"marks,omitempty"`
	Content []adfNode      `json:"content,omitempty"`
}

type adfMark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs,omitempty"`
}

// adfFromText wraps plain text in a single-paragraph ADF document. Empty
// input returns an empty doc rather than nil so the field round-trips.
func adfFromText(s string) adfDoc {
	if s == "" {
		return adfDoc{Type: "doc", Version: 1, Content: []adfNode{}}
	}
	return adfDoc{
		Type:    "doc",
		Version: 1,
		Content: []adfNode{{
			Type:    "paragraph",
			Content: []adfNode{{Type: "text", Text: s}},
		}},
	}
}

// pickADF returns rawADF as an arbitrary JSON value if non-empty,
// otherwise an ADF doc wrapping fallback.
func pickADF(rawADF json.RawMessage, fallback string) any {
	if len(rawADF) > 0 {
		return rawADF
	}
	return adfFromText(fallback)
}

package jira

import (
	"encoding/json"
	"strings"
)

// adfNode is a loose decoding of Atlassian Document Format. We keep it
// untyped so unknown node shapes degrade gracefully.
type adfNode struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Content []adfNode       `json:"content"`
	Attrs   json.RawMessage `json:"attrs"`
	Marks   []adfMark       `json:"marks"`
}

type adfMark struct {
	Type  string          `json:"type"`
	Attrs json.RawMessage `json:"attrs"`
}

// renderADF converts an ADF document to markdown. The subset handled covers
// the nodes Jira Cloud issues + comments emit in practice: doc, paragraph,
// heading, bulletList, orderedList, listItem, codeBlock, inlineCode marks,
// strong/em/strike/link marks, text, hardBreak.
func renderADF(n adfNode) string {
	var b strings.Builder
	renderNode(&b, n, 0, false)
	return strings.TrimRight(b.String(), "\n")
}

func renderNode(b *strings.Builder, n adfNode, listDepth int, ordered bool) {
	switch n.Type {
	case "", "doc":
		for _, c := range n.Content {
			renderNode(b, c, listDepth, ordered)
		}

	case "paragraph":
		for _, c := range n.Content {
			renderNode(b, c, listDepth, ordered)
		}
		b.WriteString("\n\n")

	case "heading":
		level := headingLevel(n.Attrs)
		b.WriteString(strings.Repeat("#", level))
		b.WriteByte(' ')
		for _, c := range n.Content {
			renderNode(b, c, listDepth, ordered)
		}
		b.WriteString("\n\n")

	case "bulletList":
		for _, c := range n.Content {
			renderNode(b, c, listDepth+1, false)
		}

	case "orderedList":
		for _, c := range n.Content {
			renderNode(b, c, listDepth+1, true)
		}

	case "listItem":
		b.WriteString(strings.Repeat("  ", maxInt(listDepth-1, 0)))
		if ordered {
			b.WriteString("1. ")
		} else {
			b.WriteString("- ")
		}
		var inner strings.Builder
		for _, c := range n.Content {
			renderNode(&inner, c, listDepth, ordered)
		}
		b.WriteString(strings.TrimRight(inner.String(), "\n"))
		b.WriteByte('\n')

	case "codeBlock":
		b.WriteString("```\n")
		for _, c := range n.Content {
			renderNode(b, c, listDepth, ordered)
		}
		b.WriteString("\n```\n\n")

	case "text":
		b.WriteString(applyMarks(n.Text, n.Marks))

	case "hardBreak":
		b.WriteString("  \n")

	case "mention":
		name := mentionName(n.Attrs)
		if name == "" {
			name = n.Text
		}
		b.WriteString("@" + name)

	default:
		// Unknown node: render children as fallback.
		for _, c := range n.Content {
			renderNode(b, c, listDepth, ordered)
		}
		if n.Text != "" {
			b.WriteString(n.Text)
		}
	}
}

func applyMarks(s string, marks []adfMark) string {
	for _, m := range marks {
		switch m.Type {
		case "strong":
			s = "**" + s + "**"
		case "em":
			s = "*" + s + "*"
		case "strike":
			s = "~~" + s + "~~"
		case "code":
			s = "`" + s + "`"
		case "link":
			if href := linkHref(m.Attrs); href != "" {
				s = "[" + s + "](" + href + ")"
			}
		}
	}
	return s
}

func headingLevel(attrs json.RawMessage) int {
	var a struct {
		Level int `json:"level"`
	}
	_ = json.Unmarshal(attrs, &a)
	if a.Level < 1 {
		return 1
	}
	if a.Level > 6 {
		return 6
	}
	return a.Level
}

func linkHref(attrs json.RawMessage) string {
	var a struct {
		Href string `json:"href"`
	}
	_ = json.Unmarshal(attrs, &a)
	return a.Href
}

func mentionName(attrs json.RawMessage) string {
	var a struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(attrs, &a)
	return strings.TrimPrefix(a.Text, "@")
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

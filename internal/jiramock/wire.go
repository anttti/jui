package jiramock

// Wire structs mirror Atlassian's published OpenAPI v3 schemas at
// https://developer.atlassian.com/cloud/jira/platform/swagger-v3.v3.json.
// Only the fields we serve are modelled — Jira returns far more than
// we need, but the JSON shape and key names match the upstream contract.

type wireUser struct {
	AccountID    string `json:"accountId"`
	AccountType  string `json:"accountType,omitempty"`
	Active       bool   `json:"active"`
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress,omitempty"`
	Self         string `json:"self,omitempty"`
	TimeZone     string `json:"timeZone,omitempty"`
}

type wireNamed struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
	Self string `json:"self,omitempty"`
}

type wireProject struct {
	ID   string `json:"id,omitempty"`
	Key  string `json:"key"`
	Name string `json:"name,omitempty"`
	Self string `json:"self,omitempty"`
}

type wireStatusCategory struct {
	ID   int    `json:"id,omitempty"`
	Key  string `json:"key"`
	Name string `json:"name,omitempty"`
}

type wireStatus struct {
	ID             string             `json:"id,omitempty"`
	Name           string             `json:"name"`
	Self           string             `json:"self,omitempty"`
	StatusCategory wireStatusCategory `json:"statusCategory"`
}

type wireIssueFields struct {
	Summary     string     `json:"summary"`
	Description any        `json:"description,omitempty"`
	IssueType   wireNamed  `json:"issuetype"`
	Status      wireStatus `json:"status"`
	Priority    *wireNamed `json:"priority,omitempty"`
	Project     wireProject `json:"project"`
	Assignee    *wireUser  `json:"assignee"`
	Reporter    *wireUser  `json:"reporter"`
	Labels      []string   `json:"labels"`
	DueDate     string     `json:"duedate,omitempty"`
	Created     string     `json:"created"`
	Updated     string     `json:"updated"`
}

// IssueBean (subset) — id/key/self/fields are the bits clients touch.
type wireIssue struct {
	Expand string          `json:"expand,omitempty"`
	ID     string          `json:"id"`
	Self   string          `json:"self"`
	Key    string          `json:"key"`
	Fields wireIssueFields `json:"fields"`
}

// SearchAndReconcileResults.
type wireSearchPage struct {
	Issues        []wireIssue `json:"issues"`
	NextPageToken string      `json:"nextPageToken,omitempty"`
	IsLast        bool        `json:"isLast"`
}

// PageOfComments.
type wireCommentsPage struct {
	Comments   []wireComment `json:"comments"`
	StartAt    int           `json:"startAt"`
	MaxResults int           `json:"maxResults"`
	Total      int           `json:"total"`
}

type wireComment struct {
	ID           string    `json:"id"`
	Self         string    `json:"self,omitempty"`
	Author       *wireUser `json:"author,omitempty"`
	UpdateAuthor *wireUser `json:"updateAuthor,omitempty"`
	Body         any       `json:"body"`
	Created      string    `json:"created"`
	Updated      string    `json:"updated"`
	JsdPublic    bool      `json:"jsdPublic"`
}

// ErrorCollection — Jira's standard error envelope.
type wireErrorCollection struct {
	ErrorMessages []string          `json:"errorMessages"`
	Errors        map[string]string `json:"errors,omitempty"`
	Status        int               `json:"status,omitempty"`
}

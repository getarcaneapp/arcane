package templates

import (
	"bytes"
	"fmt"
	"html/template"
)

const emailLayout = `<!DOCTYPE html>
<html>
<body style="font-family: Arial, Helvetica, sans-serif;">
<h2>{{.Title}}</h2>
{{if .Intro}}<p>{{.Intro}}</p>{{end}}
<table cellpadding="6" cellspacing="0" border="0">
{{range .Rows}}<tr><td><strong>{{.Label}}</strong></td><td>{{.Value}}</td></tr>
{{end}}</table>
<p><a href="{{.AppURL}}">Open Arcane</a></p>
</body>
</html>`

var emailTemplate = template.Must(template.New("email").Parse(emailLayout))

type emailRow struct {
	Label string
	Value string
}

type emailData struct {
	Title  string
	Intro  string
	AppURL string
	Rows   []emailRow
}

func renderEmail(data emailData) (string, error) {
	var buf bytes.Buffer
	if err := emailTemplate.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute email template: %w", err)
	}
	return buf.String(), nil
}

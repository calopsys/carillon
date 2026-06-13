package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"text/template"
	"time"
	"unicode/utf8"
)

// releaseEmojis is the pool the card title prefix is rolled from, one at random
// per notification.
var releaseEmojis = []string{"🚀", "🎉", "✨", "🔔", "📦", "🆕", "⚡", "🌟", "🎊", "🛎️"}

// defaultTemplate renders the attachment body (markdown). It is overridable via
// [notify].template in the config.
const defaultTemplate = `{{if .FirstSeen}}Now tracking **{{.Name}}** — latest is ` + "`{{.Version}}`" + `.{{else}}` + "`{{.Previous}}`" + ` → ` + "`{{.Version}}`" + `{{end}}{{if .Body}}

{{.Body}}{{end}}`

const maxBodyLen = 1500

// Mattermost posts templated attachment cards to Mattermost incoming webhooks.
type Mattermost struct {
	webhooks map[string]string // name -> URL
	tmpl     *template.Template
	hc       *http.Client
}

// NewMattermost builds a notifier. webhooks maps logical (already default-resolved)
// names to resolved URLs. A custom tmplText overrides the default body template
// ("" to keep the default).
func NewMattermost(webhooks map[string]string, tmplText string, hc *http.Client) (*Mattermost, error) {
	if tmplText == "" {
		tmplText = defaultTemplate
	}
	t, err := template.New("mattermost").Parse(tmplText)
	if err != nil {
		return nil, fmt.Errorf("parse notify template: %w", err)
	}
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	return &Mattermost{webhooks: webhooks, tmpl: t, hc: hc}, nil
}

type mmField struct {
	Title string `json:"title"`
	Value string `json:"value"`
	Short bool   `json:"short"`
}

type mmAttachment struct {
	Fallback  string    `json:"fallback,omitempty"`
	Color     string    `json:"color,omitempty"`
	Title     string    `json:"title,omitempty"`
	TitleLink string    `json:"title_link,omitempty"`
	Text      string    `json:"text,omitempty"`
	Fields    []mmField `json:"fields,omitempty"`
}

type mmPayload struct {
	Attachments []mmAttachment `json:"attachments"`
}

func (m *Mattermost) Notify(ctx context.Context, webhook string, e Event) error {
	url, err := m.url(webhook)
	if err != nil {
		return err
	}

	ev := e
	if len(ev.Body) > maxBodyLen {
		ev.Body = truncateBytes(ev.Body, maxBodyLen) + "…"
	}
	var buf bytes.Buffer
	if err := m.tmpl.Execute(&buf, ev); err != nil {
		return fmt.Errorf("render notify template: %w", err)
	}

	emoji := releaseEmojis[rand.IntN(len(releaseEmojis))]
	att := mmAttachment{
		Fallback:  fmt.Sprintf("%s %s released", e.Name, e.Version),
		Color:     "#933fba",
		Title:     fmt.Sprintf("%s %s — %s", emoji, e.Name, e.Version),
		TitleLink: e.URL,
		Text:      buf.String(),
	}
	if e.Previous != "" {
		att.Fields = append(att.Fields, mmField{Title: "Previous", Value: e.Previous, Short: true})
	}

	body, err := json.Marshal(mmPayload{Attachments: []mmAttachment{att}})
	if err != nil {
		return err
	}
	return m.post(ctx, url, body)
}

func (m *Mattermost) url(webhook string) (string, error) {
	url, ok := m.webhooks[webhook]
	if !ok {
		return "", fmt.Errorf("no webhook URL for %q", webhook)
	}
	return url, nil
}

// truncateBytes returns s clamped to at most n bytes without splitting a
// multi-byte UTF-8 rune at the boundary.
func truncateBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 {
		// A valid rune (size > 1) or a real single-byte rune stops the trim;
		// only an incomplete trailing fragment (RuneError with size 1) is shed.
		if r, size := utf8.DecodeLastRuneInString(s); r != utf8.RuneError || size > 1 {
			break
		}
		s = s[:len(s)-1]
	}
	return s
}

func (m *Mattermost) post(ctx context.Context, url string, body []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.hc.Do(req)
	if err != nil {
		return fmt.Errorf("post to mattermost: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("mattermost webhook returned %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

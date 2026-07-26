package main

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/pkg/errors"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

type duration struct {
	time.Duration
}

func (d *duration) UnmarshalText(text []byte) error {
	var err error
	d.Duration, err = time.ParseDuration(string(text))
	return err
}

func (d *duration) IsZero() bool {
	return d.Duration == 0
}

func (d *duration) ShortString() string {
	s := d.Duration.String()
	if strings.HasSuffix(s, "m0s") {
		s = s[:len(s)-2]
	}
	if strings.HasSuffix(s, "h0m") {
		s = s[:len(s)-2]
	}
	return s
}

func MustDuration(s string) duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		panic(fmt.Sprintf("failed to parse duration: %v", err))
	}
	return duration{d}
}

type markdown struct {
	original string
	html     string
}

func MustMarkdown(s string) *markdown {
	m := &markdown{}
	err := m.UnmarshalText([]byte(s))
	if err != nil {
		panic(fmt.Sprintf("failed to convert markdown: %v", err))
	}
	return m
}

func (m *markdown) UnmarshalText(source []byte) error {
	md := goldmark.New(
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			html.WithHardWraps(),
			html.WithUnsafe(),
		),
	)
	var buf bytes.Buffer
	if err := md.Convert(source, &buf); err != nil {
		return err
	}
	m.html = buf.String()
	m.original = string(source)
	return nil
}

func (m *markdown) HTML() template.HTML {
	return template.HTML(m.html)
}

func (m *markdown) IsEmpty() bool {
	return m.original == ""
}

type Config struct {
	Lang             string      `toml:"lang" json:"-"`
	Title            string      `toml:"title" json:"title"`
	Favicon          string      `toml:"favicon"`
	NavTitle         *markdown   `toml:"nav_title" json:"-"`
	NavButtonName    string      `toml:"nav_button_name" json:"-"`
	NavButtonLink    string      `toml:"nav_button_link" json:"-"`
	HeaderMessage    *markdown   `toml:"header_message" json:"-"`
	FooterMessage    *markdown   `toml:"footer_message" json:"-"`
	PoweredBy        *markdown   `toml:"powered_by" json:"-"`
	Categories       []*Category `toml:"category" json:"categories"`
	WorkerInterval   duration    `toml:"worker_interval" json:"-"`
	WorkerTimeout    duration    `toml:"worker_timeout" json:"-"`
	NumOfWorker      int         `toml:"num_of_worker" json:"-"`
	MaxCheckAttempts int         `toml:"max_check_attempts" json:"-"`
	RetryInterval    duration    `toml:"retry_interval" json:"-"`
	LatestTimeRange  duration    `toml:"latest_time_range" json:"-"`
	Days             []string    `json:"days"`
	LastUpdatedAt    time.Time   `json:"last_updated_at"`
}

type Category struct {
	Name         string      `toml:"name" json:"name"`
	Comment      string      `toml:"comment" json:"comment"`
	Services     []*Service  `toml:"service" json:"services"`
	LatestStatus *statusText `json:"latest_status"`
	Hide         bool        `toml:"hide" json:"-"`
}

type Service struct {
	categoryName   string
	Name           string        `toml:"name" json:"name"`
	Command        []string      `toml:"command" json:"-"`
	LatestStatus   *statusText   `json:"latest_status"`
	LatestStatusAt time.Time     `json:"latest_status_at"`
	StatusHistory  []*statusText `json:"status_history"`
}

type ServiceLog struct {
	Time         time.Time `json:"time"`
	CategoryName string    `json:"category_name"`
	Name         string    `json:"name"`
	Command      []string  `json:"command"`
	Status       int       `json:"status"`
	Message      string    `json:"message"`
}

func durationDefault(d *duration, defaultValue string) {
	if d.IsZero() {
		*d = MustDuration(defaultValue)
	}
}

func markdownDefault(m **markdown, defaultValue string) {
	if m == nil || *m == nil || (*m).IsEmpty() {
		*m = MustMarkdown(defaultValue)
	}
}

func stringDefault(s *string, defaultValue string) {
	if s == nil || *s == "" {
		*s = defaultValue
	}
}

func loadToml(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.Wrap(err, "could not open toml")
	}
	defer file.Close()

	var conf Config
	if _, err := toml.NewDecoder(file).Decode(&conf); err != nil {
		return nil, errors.Wrap(err, "failed to decode toml")
	}

	for _, category := range conf.Categories {
		for _, service := range category.Services {
			service.categoryName = category.Name
			if len(service.Command) == 0 {
				return nil, errors.Errorf("service %s in category %s has no command", service.Name, category.Name)
			}
		}
	}

	if conf.NumOfWorker == 0 {
		conf.NumOfWorker = 4
	}

	if conf.MaxCheckAttempts == 0 {
		conf.MaxCheckAttempts = 3
	}

	durationDefault(&conf.WorkerInterval, "5m")
	durationDefault(&conf.WorkerTimeout, "30s")
	durationDefault(&conf.LatestTimeRange, "1h")
	durationDefault(&conf.RetryInterval, "5s")

	stringDefault(&conf.Lang, "ja")
	stringDefault(&conf.Title, "Status Board")
	stringDefault(&conf.Favicon, "/favicon.ico")
	stringDefault(&conf.NavButtonName, "HOME")
	stringDefault(&conf.NavButtonLink, "/")

	markdownDefault(&conf.NavTitle, "Status Board")
	markdownDefault(&conf.HeaderMessage, "")
	markdownDefault(&conf.FooterMessage, "")
	markdownDefault(&conf.PoweredBy, "Powered by statusboard.")

	conf.LastUpdatedAt = time.Now()

	return &conf, nil
}

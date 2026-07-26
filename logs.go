package main

import (
	"bufio"
	"bytes"
	"context"

	// To use go:embed
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"text/template"
	"time"

	"github.com/goccy/go-json"
	"github.com/pkg/errors"
)

//go:embed files/index.html
var indexhtml []byte

func (o *Opt) createServiceLog() error {
	day := time.Now().Format("20060102")
	path := filepath.Join(o.Data, fmt.Sprintf("log%s.txt", day))

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	return nil
}

func (o *Opt) appendServiceLog(log *ServiceLog) error {
	day := log.Time.Format("20060102")
	path := filepath.Join(o.Data, fmt.Sprintf("log%s.txt", day))

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	return json.NewEncoder(file).Encode(log)
}

func (o *Opt) loadServiceLog(_ context.Context, d time.Time) (time.Time, []*ServiceLog, []*ServiceLog, error) {
	logs := make([]*ServiceLog, 0, 500)
	latestLogs := make([]*ServiceLog, 0, 500)

	day := d.Format("20060102")
	path := filepath.Join(o.Data, fmt.Sprintf("log%s.txt", day))
	file, err := os.Open(path)
	if err != nil {
		return time.Now(), logs, latestLogs, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lastUpdated := time.Now()
	// 各行を読み込み
	for scanner.Scan() {
		servicelog := &ServiceLog{}
		// JSON をデコード
		err := json.Unmarshal(scanner.Bytes(), servicelog)
		if err != nil {
			slog.Warn("Error decoding JSON", slog.Any("error", err))
			continue
		}
		lastUpdated = servicelog.Time
		logs = append(logs, servicelog)

		// 直近のログ
		now := time.Now()
		diff := now.Sub(servicelog.Time)
		if diff <= o.config.LatestTimeRange.Duration {
			latestLogs = append(latestLogs, servicelog)
		}
	}
	// エラーチェック
	if err := scanner.Err(); err != nil {
		slog.Warn("Error reading file", slog.Any("error", err))
	}

	return lastUpdated, logs, latestLogs, nil
}

func sameCommand(c, s []string) bool {
	if len(c) != len(s) {
		return false
	}
	for i := len(c) - 1; i >= 0; i-- {
		if c[i] != s[i] {
			return false
		}
	}
	return true
}

func (o *Opt) countByService(logs []*ServiceLog, service *Service) (int, int) {
	ok := 0
	fail := 0
	for _, log := range logs {
		// カテゴリ名とサービス名が一致 or コマンドが一緒する行を対象とする
		if (log.CategoryName == service.categoryName && log.Name == service.Name) ||
			sameCommand(log.Command, service.Command) {
			if log.Status == 0 {
				ok++
			} else {
				fail++
			}
		}
	}
	return ok, fail
}

func (o *Opt) loadLog(ctx context.Context) {
	d := time.Now()
	days := make([]string, 0, 10)
	days = append(days, o.config.LatestTimeRange.ShortString())

	// initialize
	o.initializeServices()

	// process 7 days of logs
	for i := 0; i < 7; i++ {
		days = append(days, d.Format("01/02"))
		lastUpdated, logs, latestLogs, err := o.loadServiceLog(ctx, d)
		d = d.Add(-1 * time.Hour * 24)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Warn("failed to loadlog", slog.Any("error", err))
			continue
		}
		o.updateServiceStatuses(logs, latestLogs, lastUpdated, i == 0, i)
	}

	o.updateCategoryStatuses()

	o.config.Days = days
	o.config.LastUpdatedAt = time.Now()
}

// initializeServices resets all services to their default state
func (o *Opt) initializeServices() {
	for _, category := range o.config.Categories {
		for _, service := range category.Services {
			service.LatestStatus = NoDATA
			service.LatestStatusAt = time.Now()
			service.StatusHistory = []*statusText{
				NoDATA, NoDATA, NoDATA, NoDATA,
				NoDATA, NoDATA, NoDATA,
			}
		}
	}
}

// updateServiceStatuses calculates and updates status for all services based on logs
func (o *Opt) updateServiceStatuses(
	logs []*ServiceLog,
	latestLogs []*ServiceLog,
	lastUpdated time.Time,
	isLatest bool,
	dayIndex int,
) {
	for _, category := range o.config.Categories {
		for _, service := range category.Services {
			service.LatestStatusAt = lastUpdated

			if isLatest {
				ok, fail := o.countByService(latestLogs, service)
				service.LatestStatus = o.calculateServiceStatus(ok, fail)
			} else {
				ok, fail := o.countByService(logs, service)
				service.StatusHistory[dayIndex] = o.calculateServiceStatus(ok, fail)
			}
		}
	}
}

// calculateServiceStatus returns the appropriate statusText based on ok and fail counts
func (o *Opt) calculateServiceStatus(ok, fail int) *statusText {
	if ok == 0 && fail == 0 {
		return NoDATA
	} else if fail > 0 {
		return Outage
	}
	return Operational
}

// updateCategoryStatuses calculates category-level status from its services
func (o *Opt) updateCategoryStatuses() {
	for _, category := range o.config.Categories {
		var operational, outage, noData int
		for _, service := range category.Services {
			switch {
			case service.LatestStatus.IsOperational():
				operational++
			case service.LatestStatus.IsOutage():
				outage++
			default:
				noData++
			}
		}
		category.LatestStatus = o.calculateCategoryStatus(operational, outage)
	}
}

// calculateCategoryStatus returns the appropriate category status
func (o *Opt) calculateCategoryStatus(operational, outage int) *statusText {
	if outage == 0 && operational > 0 {
		return Operational
	} else if outage > 0 {
		return Outage
	}
	return NoDATA
}

func (o *Opt) renderStatusPage(ctx context.Context) error {
	r := template.Must(template.New("index").Parse(string(indexhtml)))
	o.rwlock.Lock()
	defer o.rwlock.Unlock()
	o.loadLog(ctx)
	w := &bytes.Buffer{}
	err := r.ExecuteTemplate(w, "index", o.config)
	if err != nil {
		return err
	}
	o.htmlBlob = w.Bytes()
	return nil
}

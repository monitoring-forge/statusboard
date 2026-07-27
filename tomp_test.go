package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func writeTempToml(t *testing.T, content string) string {
	t.Helper()
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.toml")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp toml: %v", err)
	}
	return tmpFile
}

func TestLoadToml_Basic(t *testing.T) {
	tomlContent := `
title = "Test Board"
favicon = "/favicon.ico"
worker_interval = "2m"
worker_timeout = "10s"
num_of_worker = 2
max_check_attempts = 5
retry_interval = "3s"
latest_time_range = "2h"

[[category]]
name = "Web"
comment = "Web services"
hide = false

  [[category.service]]
  name = "Google"
  command = ["ping", "google.com"]
`
	path := writeTempToml(t, tomlContent)
	conf, err := loadToml(path)
	assert.NoError(t, err)

	// Basic config fields
	assert.Equal(t, "ja", conf.Lang)
	assert.Equal(t, "Test Board", conf.Title)
	assert.Equal(t, "/favicon.ico", conf.Favicon)

	// Worker settings
	assert.Equal(t, 2*time.Minute, conf.WorkerInterval.Duration)
	assert.Equal(t, 10*time.Second, conf.WorkerTimeout.Duration)
	assert.Equal(t, 2, conf.NumOfWorker)
	assert.Equal(t, 5, conf.MaxCheckAttempts)
	assert.Equal(t, 3*time.Second, conf.RetryInterval.Duration)
	assert.Equal(t, 2*time.Hour, conf.LatestTimeRange.Duration)

	// Category fields
	assert.Equal(t, 1, len(conf.Categories))
	cat := conf.Categories[0]
	assert.Equal(t, "Web", cat.Name)
	assert.Equal(t, "Web services", cat.Comment)
	assert.False(t, cat.Hide)

	// Service fields
	assert.Equal(t, 1, len(cat.Services))
	svc := cat.Services[0]
	assert.Equal(t, "Google", svc.Name)
	assert.Equal(t, "Web", svc.categoryName)
	assert.Equal(t, []string{"ping", "google.com"}, svc.Command)

	// Generated fields
	assert.NotNil(t, conf.PoweredBy)
	assert.Contains(t, conf.PoweredBy.html, "Powered by statusboard")
	assert.False(t, conf.LastUpdatedAt.IsZero())
}

func TestLoadToml_Defaults(t *testing.T) {
	tomlContent := `
title = "Defaults Test"
[[category]]
name = "Cat"
comment = "C"
  [[category.service]]
  name = "Svc"
  command = ["echo"]
`
	path := writeTempToml(t, tomlContent)
	conf, err := loadToml(path)
	assert.NoError(t, err)

	assert.Equal(t, 4, conf.NumOfWorker)
	assert.Equal(t, 5*time.Minute, conf.WorkerInterval.Duration)
	assert.Equal(t, 30*time.Second, conf.WorkerTimeout.Duration)
	assert.Equal(t, 1*time.Hour, conf.LatestTimeRange.Duration)
	assert.Equal(t, 3, conf.MaxCheckAttempts)
	assert.Equal(t, 5*time.Second, conf.RetryInterval.Duration)

	assert.NotNil(t, conf.PoweredBy)
	assert.Contains(t, conf.PoweredBy.html, "Powered by statusboard")
}

func TestLoadToml_FileNotFound(t *testing.T) {
	_, err := loadToml("not_exist.toml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoadToml_InvalidToml(t *testing.T) {
	tomlContent := `invalid =`
	path := writeTempToml(t, tomlContent)
	_, err := loadToml(path)
	if err == nil {
		t.Fatal("expected error for invalid toml, got nil")
	}
}

func TestLoadToml_MissingCommand(t *testing.T) {
	tomlContent := `
[[category]]
name = "Cat"
  [[category.service]]
  name = "Svc"
`
	path := writeTempToml(t, tomlContent)
	_, err := loadToml(path)
	if err == nil {
		t.Fatal("expected error for missing command, got nil")
	}
}

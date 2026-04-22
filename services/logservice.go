package services

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const errorLogFileName = "pmt-errors.log"

// LogService writes error logs to a file alongside the DB.
type LogService struct {
	logPath string
}

// ServiceStartup ensures the app dir exists and truncates the error log (overwrite on app launch).
func (l *LogService) ServiceStartup() error {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("user config dir: %w", err)
	}
	appDir := filepath.Join(configDir, appConfigDirName)
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	l.logPath = filepath.Join(appDir, errorLogFileName)
	return os.WriteFile(l.logPath, nil, 0o644)
}

// LogError appends a timestamped error line to pmt-errors.log.
func (l *LogService) LogError(message string, stack string) error {
	if l.logPath == "" {
		return nil
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	line := ts + " " + message
	if stack != "" {
		line += "\n" + stack
	}
	line += "\n"
	f, err := os.OpenFile(l.logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line)
	return err
}

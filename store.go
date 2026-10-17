package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rob121/cannon/extension"
	"golang.org/x/oauth2"
	"gorm.io/gorm"
)

type connectionSettings struct {
	ClientID      string `json:"client_id"`
	ClientSecret  string `json:"client_secret"`
	TokenJSON     string `json:"token_json"`
	FolderID      string `json:"folder_id"`
	FolderName    string `json:"folder_name"`
	AccountEmail  string `json:"account_email"`
	EnsurePublic  bool   `json:"ensure_public"`
	LastSyncAt    string `json:"last_sync_at"`
	LastSyncError string `json:"last_sync_error"`
}

type galleryFolder struct {
	ID           uint      `gorm:"primaryKey;column:folder_id"`
	DriveID      string    `gorm:"column:drive_id;size:128;not null;uniqueIndex"`
	ParentDriveID string   `gorm:"column:parent_drive_id;size:128;index"`
	Name         string    `gorm:"column:name;size:255;not null"`
	Title        string    `gorm:"column:title;size:255"`
	Slug         string    `gorm:"column:slug;size:255;not null;uniqueIndex"`
	Path         string    `gorm:"column:path;size:1024;not null;index"` // slash-separated slugs from root children
	Description  string    `gorm:"column:description;type:text"`
	Depth        int       `gorm:"column:depth;not null;default:0;index"`
	SortOrder    int       `gorm:"column:sort_order;not null;default:0;index"`
	Status       string    `gorm:"column:status;size:16;not null;default:active;index"`
	SyncedAt     time.Time `gorm:"column:synced_at"`
	CreatedAt    time.Time `gorm:"column:created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at"`
}

type galleryImage struct {
	ID           uint      `gorm:"primaryKey;column:image_id"`
	DriveFileID  string    `gorm:"column:drive_file_id;size:128;not null;uniqueIndex"`
	FolderDriveID string   `gorm:"column:folder_drive_id;size:128;not null;index"`
	Name         string    `gorm:"column:name;size:255;not null"`
	Slug         string    `gorm:"column:slug;size:255;not null;index"`
	MimeType     string    `gorm:"column:mime_type;size:128"`
	ViewURL      string    `gorm:"column:view_url;size:1024"`
	ThumbURL     string    `gorm:"column:thumb_url;size:1024"`
	Description  string    `gorm:"column:description;type:text"`
	Width        int64     `gorm:"column:width"`
	Height       int64     `gorm:"column:height"`
	SizeBytes    int64     `gorm:"column:size_bytes"`
	ModifiedTime string    `gorm:"column:modified_time;size:64"`
	SortOrder    int       `gorm:"column:sort_order;not null;default:0;index"`
	Status       string    `gorm:"column:status;size:16;not null;default:active;index"`
	SyncedAt     time.Time `gorm:"column:synced_at"`
	CreatedAt    time.Time `gorm:"column:created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at"`
}

type configStore struct {
	table string
}

func (s configStore) Load(section string) (map[string]any, error) {
	db, _, err := extension.OpenDB()
	if err != nil {
		return nil, err
	}
	if err := extension.MigrateConfigurationStore(db, s.table); err != nil {
		return nil, err
	}
	return extension.DBConfigurationStore(db, s.table).Load(section)
}

func (s configStore) Save(section string, data map[string]any) error {
	db, _, err := extension.OpenDB()
	if err != nil {
		return err
	}
	if err := extension.MigrateConfigurationStore(db, s.table); err != nil {
		return err
	}
	return extension.DBConfigurationStore(db, s.table).Save(section, data)
}

func (app *galleryApp) settingsStore() configStore {
	return configStore{table: app.settingsTable}
}

func (app *galleryApp) loadConnection() (connectionSettings, error) {
	data, err := app.settingsStore().Load(connectionSection)
	if err != nil {
		return connectionSettings{}, err
	}
	conn := connectionSettings{
		ClientID:      strings.TrimSpace(stringValue(data, "client_id")),
		ClientSecret:  strings.TrimSpace(stringValue(data, "client_secret")),
		TokenJSON:     strings.TrimSpace(stringValue(data, "token_json")),
		FolderID:      strings.TrimSpace(stringValue(data, "folder_id")),
		FolderName:    strings.TrimSpace(stringValue(data, "folder_name")),
		AccountEmail:  strings.TrimSpace(stringValue(data, "account_email")),
		EnsurePublic:  true,
		LastSyncAt:    strings.TrimSpace(stringValue(data, "last_sync_at")),
		LastSyncError: strings.TrimSpace(stringValue(data, "last_sync_error")),
	}
	if _, ok := data["ensure_public"]; ok {
		conn.EnsurePublic = boolValue(data, "ensure_public")
	}
	return conn, nil
}

func (app *galleryApp) saveConnection(conn connectionSettings) error {
	return app.settingsStore().Save(connectionSection, map[string]any{
		"client_id":       conn.ClientID,
		"client_secret":   conn.ClientSecret,
		"token_json":      conn.TokenJSON,
		"folder_id":       conn.FolderID,
		"folder_name":     conn.FolderName,
		"account_email":   conn.AccountEmail,
		"ensure_public":   conn.EnsurePublic,
		"last_sync_at":    conn.LastSyncAt,
		"last_sync_error": conn.LastSyncError,
	})
}

func (conn connectionSettings) token() (*oauth2.Token, error) {
	if conn.TokenJSON == "" {
		return nil, fmt.Errorf("Google Drive is not connected")
	}
	var tok oauth2.Token
	if err := json.Unmarshal([]byte(conn.TokenJSON), &tok); err != nil {
		return nil, fmt.Errorf("decode oauth token: %w", err)
	}
	if tok.AccessToken == "" && tok.RefreshToken == "" {
		return nil, fmt.Errorf("Google Drive token is empty")
	}
	return &tok, nil
}

func (conn connectionSettings) connected() bool {
	return conn.ClientID != "" && conn.ClientSecret != "" && conn.TokenJSON != ""
}

func (conn connectionSettings) ready() bool {
	return conn.connected() && conn.FolderID != ""
}

func (app *galleryApp) db() (*gorm.DB, error) {
	db, _, err := extension.OpenDB()
	return db, err
}

func stringValue(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	v, ok := data[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	default:
		return fmt.Sprint(t)
	}
}

func boolValue(data map[string]any, key string) bool {
	if data == nil {
		return false
	}
	v, ok := data[key]
	if !ok || v == nil {
		return false
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "1" || strings.EqualFold(t, "true") || strings.EqualFold(t, "on") || strings.EqualFold(t, "yes")
	case float64:
		return t != 0
	case int:
		return t != 0
	default:
		return false
	}
}

func intValue(data map[string]any, key string, fallback int) int {
	if data == nil {
		return fallback
	}
	v, ok := data[key]
	if !ok || v == nil {
		return fallback
	}
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(t))
		if err != nil {
			return fallback
		}
		return n
	default:
		return fallback
	}
}

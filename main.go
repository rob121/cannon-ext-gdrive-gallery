package main

import (
	"embed"
	"encoding/json"
	"html/template"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/rob121/cannon/extension"
)

//go:embed all:templates
var templateFiles embed.FS

//go:embed help/*.md
var helpFiles embed.FS

const (
	extensionName              = "cannon-ext-gdrive-gallery"
	extensionTitle             = "Google Drive Gallery"
	extensionDescription       = "Photo galleries sourced from a Google Drive folder. Images stay on Drive (anyone-with-link); folder and image metadata is cached locally with editable descriptions."
	extensionVersion           = "0.1.4"
	updateURLBase              = "https://github.com/rob121/cannon-ext-gdrive-gallery/releases/download"
	defaultAdminMenu           = "Drive Gallery"
	connectionSection          = "connection"
	scheduleSection            = "schedule"
	defaultSyncIntervalMinutes = 60
	minSyncIntervalMinutes     = 5
)

var scheduleConfigSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "enabled": {
      "type": "boolean",
      "title": "Enable Scheduled Sync",
      "default": false,
      "description": "When enabled, the gallery syncs from Google Drive automatically on the interval below."
    },
    "interval_minutes": {
      "type": "integer",
      "title": "Sync Interval (minutes)",
      "minimum": 5,
      "default": 60,
      "description": "How often to sync folder and image metadata from Drive. Minimum 5 minutes."
    }
  }
}`)

var scheduleConfigUI = json.RawMessage(`{
  "type": "VerticalLayout",
  "elements": [
    {"type": "Control", "scope": "#/properties/enabled"},
    {"type": "Control", "scope": "#/properties/interval_minutes"}
  ]
}`)

type galleryApp struct {
	info          extension.Info
	templates     *extension.Templates
	settingsTable string
	foldersTable  string
	imagesTable   string
	mu            sync.Mutex
	syncing       bool
	oauthState    sync.Map
	scheduler     *galleryScheduler
}

func main() {
	app, s := newServer()
	// --data=… is answered inside Run before site/socket; skip scheduler startup for that path.
	if _, dataQuery := extension.DataArgKey(os.Args); !dataQuery {
		app.startScheduler()
	}
	if err := s.Run(); err != nil {
		log.Fatal(err)
	}
}

func newServer() (*galleryApp, *extension.Server) {
	info := extension.Info{
		Name:          extensionName,
		Title:         extensionTitle,
		Description:   extensionDescription,
		Version:       extensionVersion,
		UpdateURLBase: updateURLBase,
		AdminMenuName: defaultAdminMenu,
	}
	app := &galleryApp{
		info:          info,
		templates:     extension.NewTemplates(templateFiles, "templates"),
		settingsTable: extension.TableName(info.TablePrefix(), "settings"),
		foldersTable:  extension.TableName(info.TablePrefix(), "folders"),
		imagesTable:   extension.TableName(info.TablePrefix(), "images"),
	}
	app.scheduler = newGalleryScheduler(app)

	s := extension.New(info)
	s.EmbedTemplates(templateFiles, "templates")
	s.EmbedHelp(helpFiles, "help")
	s.OnInstall(app.install)
	s.RegisterPermissions([]extension.PermissionDef{
		{ID: "gallery.view", DisplayName: "View Drive gallery admin", Description: "View gallery sync status and cached metadata.", Category: "gallery"},
		{ID: "gallery.sync", DisplayName: "Sync Drive gallery", Description: "Sync folders and images from Google Drive.", Category: "gallery"},
		{ID: "gallery.edit", DisplayName: "Edit gallery metadata", Description: "Edit image descriptions and gallery titles.", Category: "gallery"},
		{ID: "gallery.setup", DisplayName: "Configure Drive gallery", Description: "Connect Google Drive and choose the gallery root folder.", Category: "gallery", Dangerous: true},
	})
	s.OnConfiguration(extension.MapConfiguration([]extension.ConfigurationDefinition{
		{
			ID:       scheduleSection,
			Title:    "Sync Schedule",
			Schema:   scheduleConfigSchema,
			UISchema: scheduleConfigUI,
		},
	}, configStore{table: app.settingsTable, onSave: app.onScheduleSaved}))
	s.OnPageList(app.pageDefinitions)
	s.RegisterPage(extension.PageDefinition{ID: "gallery-index", Title: "Gallery Index"}, app.indexPage)
	s.RegisterPage(extension.PageDefinition{ID: "gallery-folder", Title: "Gallery Folder"}, app.folderPage)
	s.HandleAdmin("/admin/", app.admin)
	return app, s
}

func (app *galleryApp) onScheduleSaved(section string, data map[string]any) {
	if section != scheduleSection {
		return
	}
	app.scheduler.reload()
}

func (app *galleryApp) startScheduler() {
	app.scheduler.reload()
}

func (app *galleryApp) install(req extension.WireRequest) error {
	db, _, err := extension.OpenDB()
	if err != nil {
		return err
	}
	if err := extension.MigrateConfigurationStore(db, app.settingsTable); err != nil {
		return err
	}
	if err := db.Table(app.foldersTable).AutoMigrate(&galleryFolder{}); err != nil {
		return err
	}
	return db.Table(app.imagesTable).AutoMigrate(&galleryImage{})
}

func (app *galleryApp) render(name string, data map[string]any) extension.WireResponse {
	body, err := app.templates.WithFuncs(template.FuncMap{
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
		"nl2br": func(s string) template.HTML {
			escaped := template.HTMLEscapeString(s)
			return template.HTML(strings.ReplaceAll(escaped, "\n", "<br>\n"))
		},
	}).Execute(name, data)
	if err != nil {
		return extension.Error(500, err.Error())
	}
	return extension.HTML(200, body)
}

func csrfField(req extension.WireRequest) template.HTML {
	return template.HTML(extension.CSRFHiddenField(req))
}

package main

import (
	"embed"
	"html/template"
	"log"
	"strings"
	"sync"

	"github.com/rob121/cannon/extension"
)

//go:embed all:templates
var templateFiles embed.FS

//go:embed help/*.md
var helpFiles embed.FS

const (
	extensionName        = "cannon-ext-gdrive-gallery"
	extensionTitle       = "Google Drive Gallery"
	extensionDescription = "Photo galleries sourced from a Google Drive folder. Images stay on Drive (anyone-with-link); folder and image metadata is cached locally with editable descriptions."
	extensionVersion     = "0.1.3"
	updateURLBase        = "https://github.com/rob121/cannon-ext-gdrive-gallery/releases/download"
	defaultAdminMenu     = "Drive Gallery"
	connectionSection    = "connection"
)

type galleryApp struct {
	info          extension.Info
	templates     *extension.Templates
	settingsTable string
	foldersTable  string
	imagesTable   string
	mu            sync.Mutex
	syncing       bool
	oauthState    sync.Map
}

func main() {
	_, s := newServer()
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
	s.OnPageList(app.pageDefinitions)
	s.RegisterPage(extension.PageDefinition{ID: "gallery-index", Title: "Gallery Index"}, app.indexPage)
	s.RegisterPage(extension.PageDefinition{ID: "gallery-folder", Title: "Gallery Folder"}, app.folderPage)
	s.HandleAdmin("/admin/", app.admin)
	return app, s
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

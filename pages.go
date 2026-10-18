package main

import (
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/rob121/cannon/extension"
)

type folderView struct {
	galleryFolder
	URL string
}

type imageView struct {
	galleryImage
}

type publishedItem struct {
	Title string
	Slug  string
	Intro string
	Body  string
}

func (app *galleryApp) pageDefinitions() ([]extension.PageDefinition, error) {
	folderOptions := app.activeFolderOptions()
	common := []extension.PageField{
		{Name: "title", Label: "Page Title", Type: "text"},
		{Name: "gallery_base_url", Label: "Gallery Base URL", Type: "text", Help: "Base path for gallery links, e.g. /gallery"},
	}
	return []extension.PageDefinition{
		{
			ID:    "gallery-index",
			Title: "Gallery Index",
			Fields: append([]extension.PageField{
				{
					Name:  "intro_item_slug",
					Label: "Introduction Content Item",
					Type:  "item",
					Help:  "Published content item whose body is shown above the gallery list. Edit it in Content with the full WYSIWYG editor.",
				},
				{
					Name:  "introduction",
					Label: "Introduction (plain text)",
					Type:  "textarea",
					Help:  "Used only when no content item is selected.",
				},
			}, common...),
		},
		{
			ID:    "gallery-folder",
			Title: "Gallery Folder",
			Fields: append([]extension.PageField{
				{Name: "folder_id", Label: "Folder", Type: "select", Help: "Leave empty to infer from the URL path.", Options: folderOptions},
				{Name: "introduction", Label: "Introduction", Type: "textarea", Help: "Fallback when the folder has no description set in Drive Gallery admin."},
			}, common...),
		},
	}, nil
}

func (app *galleryApp) activeFolderOptions() []extension.PageFieldOption {
	db, err := app.db()
	if err != nil {
		return nil
	}
	var rows []galleryFolder
	if err := db.Table(app.foldersTable).Where("status = ?", "active").Order("path asc").Find(&rows).Error; err != nil {
		return nil
	}
	out := make([]extension.PageFieldOption, 0, len(rows))
	for _, row := range rows {
		label := row.Title
		if label == "" {
			label = row.Name
		}
		if row.Path != "" {
			label = label + " (" + row.Path + ")"
		}
		out = append(out, extension.PageFieldOption{
			Value: strconv.FormatUint(uint64(row.ID), 10),
			Label: label,
		})
	}
	return out
}

func (app *galleryApp) indexPage(item string, req extension.WireRequest) extension.WireResponse {
	db, err := app.db()
	if err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	base := galleryBaseURL(req)
	var rows []galleryFolder
	if err := db.Table(app.foldersTable).
		Where("status = ? AND depth = 0", "active").
		Order("sort_order asc, title asc, folder_id asc").
		Find(&rows).Error; err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	folders := make([]folderView, 0, len(rows))
	for _, row := range rows {
		folders = append(folders, folderView{
			galleryFolder: row,
			URL:           joinURL(base, row.Path),
		})
	}
	introHTML, introPlain := app.indexIntroduction(req)
	return app.render("gallery/index.html", map[string]any{
		"Title":            pageString(req, "title", "Gallery"),
		"IntroductionHTML": introHTML,
		"Introduction":     introPlain,
		"Folders":          folders,
		"PageItem":         item,
	})
}

// indexIntroduction prefers a published content item body; falls back to plain route text.
func (app *galleryApp) indexIntroduction(req extension.WireRequest) (template.HTML, string) {
	if slug := strings.TrimSpace(extension.PageDataString(req, "intro_item_slug")); slug != "" {
		if item, ok := app.loadPublishedItem(slug); ok {
			html := strings.TrimSpace(item.Body)
			if html == "" {
				html = strings.TrimSpace(item.Intro)
			}
			if html != "" {
				return template.HTML(html), ""
			}
		}
	}
	return "", strings.TrimSpace(extension.PageDataString(req, "introduction"))
}

func (app *galleryApp) loadPublishedItem(slug string) (publishedItem, bool) {
	db, err := app.db()
	if err != nil {
		return publishedItem{}, false
	}
	var row publishedItem
	err = db.Table("items").
		Select("title, slug, intro, body").
		Where("slug = ? AND status = ?", slug, "published").
		First(&row).Error
	if err != nil {
		return publishedItem{}, false
	}
	return row, true
}

func (app *galleryApp) folderPage(item string, req extension.WireRequest) extension.WireResponse {
	folder, found, err := app.findActiveFolder(req)
	if err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	if !found {
		return extension.Error(http.StatusNotFound, "gallery not found")
	}
	db, err := app.db()
	if err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	base := galleryBaseURL(req)

	var children []galleryFolder
	_ = db.Table(app.foldersTable).
		Where("status = ? AND parent_drive_id = ?", "active", folder.DriveID).
		Order("sort_order asc, title asc").
		Find(&children).Error
	childViews := make([]folderView, 0, len(children))
	for _, child := range children {
		childViews = append(childViews, folderView{
			galleryFolder: child,
			URL:           joinURL(base, child.Path),
		})
	}

	var images []galleryImage
	_ = db.Table(app.imagesTable).
		Where("status = ? AND folder_drive_id = ?", "active", folder.DriveID).
		Order("sort_order asc, name asc").
		Find(&images).Error
	imageViews := make([]imageView, 0, len(images))
	for _, img := range images {
		imageViews = append(imageViews, imageView{galleryImage: img})
	}

	title := pageString(req, "title", "")
	if title == "" {
		title = folder.Title
		if title == "" {
			title = folder.Name
		}
	}
	parentURL, parentTitle := "", ""
	if folder.ParentDriveID != "" {
		var parent galleryFolder
		if err := db.Table(app.foldersTable).Where("drive_id = ? AND status = ?", folder.ParentDriveID, "active").First(&parent).Error; err == nil {
			parentURL = joinURL(base, parent.Path)
			parentTitle = parent.Title
			if parentTitle == "" {
				parentTitle = parent.Name
			}
		}
	} else {
		parentURL = strings.TrimRight(base, "/")
		if parentURL == "" {
			parentURL = "/"
		}
		parentTitle = "All galleries"
	}

	// Per-folder description is authoritative; route introduction is only a fallback.
	desc := strings.TrimSpace(folder.Description)
	if desc == "" {
		desc = strings.TrimSpace(extension.PageDataString(req, "introduction"))
	}

	return app.render("gallery/folder.html", map[string]any{
		"Title":        title,
		"Description":  desc,
		"Folder":       folder,
		"ChildFolders": childViews,
		"Images":       imageViews,
		"ParentURL":    parentURL,
		"ParentTitle":  parentTitle,
		"PageItem":     item,
	})
}

func (app *galleryApp) findActiveFolder(req extension.WireRequest) (galleryFolder, bool, error) {
	db, err := app.db()
	if err != nil {
		return galleryFolder{}, false, err
	}
	if id, ok := extension.PageDataInt(req, "folder_id"); ok && id > 0 {
		var folder galleryFolder
		err := db.Table(app.foldersTable).Where("folder_id = ? AND status = ?", id, "active").First(&folder).Error
		return folder, err == nil, ignoreNotFound(err)
	}
	path := folderPathFromURL(req.URL)
	if path == "" {
		return galleryFolder{}, false, nil
	}
	var folder galleryFolder
	err = db.Table(app.foldersTable).Where("path = ? AND status = ?", path, "active").First(&folder).Error
	if err == nil {
		return folder, true, nil
	}
	// Also try last slug segment.
	slug := path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		slug = path[i+1:]
	}
	err = db.Table(app.foldersTable).Where("slug = ? AND status = ?", slug, "active").First(&folder).Error
	return folder, err == nil, ignoreNotFound(err)
}

func folderPathFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	path := strings.Trim(u.Path, "/")
	if path == "" {
		return ""
	}
	parts := strings.Split(path, "/")
	// Drop a leading "gallery" segment when present.
	if len(parts) > 0 && (parts[0] == "gallery" || parts[0] == "galleries") {
		parts = parts[1:]
	}
	return strings.Join(parts, "/")
}

func galleryBaseURL(req extension.WireRequest) string {
	if base := strings.TrimSpace(extension.PageDataString(req, "gallery_base_url")); base != "" {
		return strings.TrimRight(base, "/")
	}
	u, err := url.Parse(req.URL)
	if err != nil {
		return "/gallery"
	}
	path := strings.Trim(u.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		return "/gallery"
	}
	if parts[0] == "gallery" || parts[0] == "galleries" {
		return "/" + parts[0]
	}
	return "/" + parts[0]
}

func joinURL(base, path string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	path = strings.Trim(strings.TrimSpace(path), "/")
	if base == "" {
		base = "/gallery"
	}
	if path == "" {
		return base
	}
	return base + "/" + path
}

func pageString(req extension.WireRequest, key, fallback string) string {
	if v := strings.TrimSpace(extension.PageDataString(req, key)); v != "" {
		return v
	}
	return fallback
}

func ignoreNotFound(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(strings.ToLower(err.Error()), "record not found") {
		return nil
	}
	return err
}

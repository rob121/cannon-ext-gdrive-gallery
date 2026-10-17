package main

import (
	"context"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rob121/cannon/extension"
)

func (app *galleryApp) admin(req extension.WireRequest) extension.WireResponse {
	adminURL, err := parseWireURL(req.URL)
	if err != nil {
		return extension.Error(http.StatusBadRequest, err.Error())
	}
	basePath, parts := adminRoute(adminURL.Path)
	if basePath == "" {
		basePath = adminHomeURL()
	}

	if len(parts) == 2 && parts[0] == "oauth" && parts[1] == "callback" {
		return app.oauthCallback(req)
	}

	if !extension.UserAuthenticated(req) {
		return extension.Error(http.StatusForbidden, "sign in required")
	}
	csrf := csrfField(req)

	switch {
	case len(parts) == 0:
		return app.adminHome(req, adminURL, basePath, csrf)
	case len(parts) == 1 && parts[0] == "sync":
		if !strings.EqualFold(req.Method, http.MethodPost) {
			return extension.Error(http.StatusMethodNotAllowed, "method not allowed")
		}
		folders, images, syncErr := app.runSync()
		loc := basePath
		if syncErr != nil {
			loc += "?error=" + url.QueryEscape(syncErr.Error())
		} else {
			loc += "?notice=" + url.QueryEscape("Synced "+strconv.Itoa(folders)+" folders and "+strconv.Itoa(images)+" images.")
		}
		return extension.Redirect(http.StatusSeeOther, loc)
	case len(parts) == 1 && parts[0] == "setup":
		return app.adminSetup(req, adminURL, basePath)
	case len(parts) == 2 && parts[0] == "setup" && parts[1] == "credentials":
		if !strings.EqualFold(req.Method, http.MethodPost) {
			return extension.Error(http.StatusMethodNotAllowed, "method not allowed")
		}
		return app.saveCredentials(req, basePath)
	case len(parts) == 2 && parts[0] == "setup" && parts[1] == "connect":
		conn, err := app.loadConnection()
		if err != nil {
			return extension.Redirect(http.StatusSeeOther, basePath+"/setup?error="+url.QueryEscape(err.Error()))
		}
		authURL, err := app.startOAuthURL(conn, req)
		if err != nil {
			return extension.Redirect(http.StatusSeeOther, basePath+"/setup?error="+url.QueryEscape(err.Error()))
		}
		return extension.Redirect(http.StatusSeeOther, authURL)
	case len(parts) == 2 && parts[0] == "setup" && parts[1] == "disconnect":
		if !strings.EqualFold(req.Method, http.MethodPost) {
			return extension.Error(http.StatusMethodNotAllowed, "method not allowed")
		}
		conn, err := app.loadConnection()
		if err != nil {
			return extension.Redirect(http.StatusSeeOther, basePath+"/setup?error="+url.QueryEscape(err.Error()))
		}
		conn.TokenJSON = ""
		conn.AccountEmail = ""
		conn.FolderID = ""
		conn.FolderName = ""
		if err := app.saveConnection(conn); err != nil {
			return extension.Redirect(http.StatusSeeOther, basePath+"/setup?error="+url.QueryEscape(err.Error()))
		}
		return extension.Redirect(http.StatusSeeOther, basePath+"/setup?notice="+url.QueryEscape("Disconnected from Google Drive."))
	case len(parts) == 2 && parts[0] == "setup" && parts[1] == "folder":
		if !strings.EqualFold(req.Method, http.MethodPost) {
			return extension.Error(http.StatusMethodNotAllowed, "method not allowed")
		}
		return app.saveFolder(req, basePath)
	case len(parts) == 2 && parts[0] == "folder":
		id, _ := strconv.ParseUint(parts[1], 10, 64)
		return app.adminFolder(req, adminURL, basePath, uint(id), csrf)
	default:
		return extension.Error(http.StatusNotFound, "admin route not found")
	}
}

func (app *galleryApp) adminHome(req extension.WireRequest, adminURL *url.URL, basePath string, csrf template.HTML) extension.WireResponse {
	conn, _ := app.loadConnection()
	db, err := app.db()
	if err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	var folders []galleryFolder
	_ = db.Table(app.foldersTable).Order("path asc").Find(&folders).Error
	var folderCount, imageCount int64
	_ = db.Table(app.foldersTable).Where("status = ?", "active").Count(&folderCount).Error
	_ = db.Table(app.imagesTable).Where("status = ?", "active").Count(&imageCount).Error

	return app.render("gallery/admin.html", map[string]any{
		"Title":       "Drive Gallery",
		"Subtitle":    "Sync Google Drive folders into local gallery metadata.",
		"BasePath":    basePath,
		"SetupURL":    basePath + "/setup",
		"CSRFField":   csrf,
		"Conn":        conn,
		"Folders":     folders,
		"FolderCount": folderCount,
		"ImageCount":  imageCount,
		"Syncing":     app.isSyncing(),
		"Notice":      adminURL.Query().Get("notice"),
		"Error":       adminURL.Query().Get("error"),
	})
}

func (app *galleryApp) adminSetup(req extension.WireRequest, adminURL *url.URL, basePath string) extension.WireResponse {
	conn, err := app.loadConnection()
	if err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	parentID := strings.TrimSpace(adminURL.Query().Get("parent"))
	var folders []driveFolder
	var folderErr string
	if conn.connected() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		folders, err = app.listDriveFolders(ctx, parentID)
		if err != nil {
			folderErr = err.Error()
		}
	}
	redirectURL, _ := app.oauthRedirectURL(req)
	notice := adminURL.Query().Get("notice")
	if adminURL.Query().Get("oauth") == "connected" {
		notice = "Google Drive connected. Select a gallery root folder below."
	}
	errMsg := adminURL.Query().Get("error")
	if oauthErr := adminURL.Query().Get("oauth_error"); oauthErr != "" {
		errMsg = oauthErr
	}
	parentLabel := "My Drive"
	if parentID != "" && parentID != "root" {
		parentLabel = parentID
	}
	return app.render("gallery/admin_setup.html", map[string]any{
		"Title":       "Drive Gallery Setup",
		"BasePath":    basePath,
		"HomeURL":     basePath,
		"CSRFField":   csrfField(req),
		"Conn":        conn,
		"Connected":   conn.connected(),
		"Ready":       conn.ready(),
		"AccountEmail": conn.AccountEmail,
		"Folders":     folders,
		"FolderError": folderErr,
		"ParentID":    parentID,
		"ParentLabel": parentLabel,
		"RedirectURL": redirectURL,
		"Notice":      notice,
		"Error":       errMsg,
	})
}

func (app *galleryApp) adminFolder(req extension.WireRequest, adminURL *url.URL, basePath string, folderID uint, csrf template.HTML) extension.WireResponse {
	if folderID == 0 {
		return extension.Error(http.StatusNotFound, "folder not found")
	}
	db, err := app.db()
	if err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	var folder galleryFolder
	if err := db.Table(app.foldersTable).Where("folder_id = ?", folderID).First(&folder).Error; err != nil {
		return extension.Error(http.StatusNotFound, "folder not found")
	}

	if strings.EqualFold(req.Method, http.MethodPost) {
		values, err := url.ParseQuery(req.Body)
		if err != nil {
			return extension.Redirect(http.StatusSeeOther, basePath+"/folder/"+strconv.FormatUint(uint64(folderID), 10)+"?error="+url.QueryEscape("invalid form"))
		}
		switch strings.TrimSpace(values.Get("action")) {
		case "save_folder":
			title := strings.TrimSpace(values.Get("title"))
			if title == "" {
				title = folder.Name
			}
			_ = db.Table(app.foldersTable).Where("folder_id = ?", folderID).Updates(map[string]any{
				"title":       title,
				"description": strings.TrimSpace(values.Get("description")),
				"updated_at":  time.Now().UTC(),
			}).Error
			return extension.Redirect(http.StatusSeeOther, basePath+"/folder/"+strconv.FormatUint(uint64(folderID), 10)+"?notice="+url.QueryEscape("Gallery saved."))
		case "save_image":
			imageID, _ := strconv.ParseUint(values.Get("image_id"), 10, 64)
			_ = db.Table(app.imagesTable).Where("image_id = ? AND folder_drive_id = ?", imageID, folder.DriveID).Updates(map[string]any{
				"description": strings.TrimSpace(values.Get("description")),
				"updated_at":  time.Now().UTC(),
			}).Error
			return extension.Redirect(http.StatusSeeOther, basePath+"/folder/"+strconv.FormatUint(uint64(folderID), 10)+"?notice="+url.QueryEscape("Image description saved."))
		}
	}

	_ = db.Table(app.foldersTable).Where("folder_id = ?", folderID).First(&folder).Error
	var images []galleryImage
	_ = db.Table(app.imagesTable).
		Where("folder_drive_id = ? AND status = ?", folder.DriveID, "active").
		Order("sort_order asc, name asc").
		Find(&images).Error

	return app.render("gallery/admin_folder.html", map[string]any{
		"Title":     folder.Title,
		"BasePath":  basePath,
		"CSRFField": csrf,
		"Folder":    folder,
		"Images":    images,
		"Notice":    adminURL.Query().Get("notice"),
		"Error":     adminURL.Query().Get("error"),
	})
}

func (app *galleryApp) saveCredentials(req extension.WireRequest, basePath string) extension.WireResponse {
	values, err := url.ParseQuery(req.Body)
	if err != nil {
		return extension.Redirect(http.StatusSeeOther, basePath+"/setup?error="+url.QueryEscape("invalid form"))
	}
	conn, err := app.loadConnection()
	if err != nil {
		return extension.Redirect(http.StatusSeeOther, basePath+"/setup?error="+url.QueryEscape(err.Error()))
	}
	prevID := conn.ClientID
	conn.ClientID = strings.TrimSpace(values.Get("client_id"))
	secret := strings.TrimSpace(values.Get("client_secret"))
	if secret != "" {
		conn.ClientSecret = secret
	}
	conn.EnsurePublic = values.Get("ensure_public") == "1"
	if conn.ClientID == "" {
		return extension.Redirect(http.StatusSeeOther, basePath+"/setup?error="+url.QueryEscape("client ID is required"))
	}
	if conn.ClientSecret == "" {
		return extension.Redirect(http.StatusSeeOther, basePath+"/setup?error="+url.QueryEscape("client secret is required"))
	}
	if prevID != "" && prevID != conn.ClientID {
		conn.TokenJSON = ""
		conn.AccountEmail = ""
		conn.FolderID = ""
		conn.FolderName = ""
	}
	if err := app.saveConnection(conn); err != nil {
		return extension.Redirect(http.StatusSeeOther, basePath+"/setup?error="+url.QueryEscape(err.Error()))
	}
	return extension.Redirect(http.StatusSeeOther, basePath+"/setup?notice="+url.QueryEscape("OAuth credentials saved."))
}

func (app *galleryApp) saveFolder(req extension.WireRequest, basePath string) extension.WireResponse {
	values, err := url.ParseQuery(req.Body)
	if err != nil {
		return extension.Redirect(http.StatusSeeOther, basePath+"/setup?error="+url.QueryEscape("invalid form"))
	}
	folderID := strings.TrimSpace(values.Get("folder_id"))
	folderName := strings.TrimSpace(values.Get("folder_name"))
	if folderID == "" {
		return extension.Redirect(http.StatusSeeOther, basePath+"/setup?error="+url.QueryEscape("folder is required"))
	}
	if folderName == "" {
		folderName = folderID
	}
	conn, err := app.loadConnection()
	if err != nil {
		return extension.Redirect(http.StatusSeeOther, basePath+"/setup?error="+url.QueryEscape(err.Error()))
	}
	conn.FolderID = folderID
	conn.FolderName = folderName
	if err := app.saveConnection(conn); err != nil {
		return extension.Redirect(http.StatusSeeOther, basePath+"/setup?error="+url.QueryEscape(err.Error()))
	}
	return extension.Redirect(http.StatusSeeOther, basePath+"/setup?notice="+url.QueryEscape("Gallery root folder saved: "+folderName))
}

func parseWireURL(raw string) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		return url.Parse(adminHomeURL())
	}
	return url.Parse(raw)
}

func adminRoute(path string) (base string, parts []string) {
	path = strings.TrimSpace(path)
	marker := "/extension-apps/" + extensionName
	idx := strings.Index(path, marker)
	if idx < 0 {
		marker = "/extension-apps/" + url.PathEscape(extensionName)
		idx = strings.Index(path, marker)
	}
	if idx < 0 {
		return "", nil
	}
	base = path[:idx+len(marker)]
	rest := strings.TrimPrefix(path[idx+len(marker):], "/")
	if rest == "" {
		return base, nil
	}
	return base, strings.Split(rest, "/")
}

package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
)

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = nonSlug.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "item"
	}
	return s
}

func (app *galleryApp) isSyncing() bool {
	app.mu.Lock()
	defer app.mu.Unlock()
	return app.syncing
}

func (app *galleryApp) runSync() (folders, images int, err error) {
	app.mu.Lock()
	if app.syncing {
		app.mu.Unlock()
		return 0, 0, fmt.Errorf("a sync is already running")
	}
	app.syncing = true
	app.mu.Unlock()
	defer func() {
		app.mu.Lock()
		app.syncing = false
		app.mu.Unlock()
	}()

	conn, err := app.loadConnection()
	if err != nil {
		return 0, 0, err
	}
	if !conn.ready() {
		return 0, 0, fmt.Errorf("connect Google Drive and choose a root gallery folder first")
	}

	db, err := app.db()
	if err != nil {
		return 0, 0, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	now := time.Now().UTC()
	seenFolders := map[string]struct{}{}
	seenImages := map[string]struct{}{}

	children, err := app.listDriveFolders(ctx, conn.FolderID)
	if err != nil {
		app.recordSyncError(conn, err)
		return 0, 0, err
	}

	for i, child := range children {
		nf, ni, walkErr := app.syncFolderTree(ctx, db, conn, child.ID, child.Name, "", "", 0, i*100, now, seenFolders, seenImages)
		folders += nf
		images += ni
		if walkErr != nil {
			app.recordSyncError(conn, walkErr)
			return folders, images, walkErr
		}
	}

	if err := app.deactivateMissing(db, seenFolders, seenImages); err != nil {
		app.recordSyncError(conn, err)
		return folders, images, err
	}

	conn.LastSyncAt = now.Format(time.RFC3339)
	conn.LastSyncError = ""
	_ = app.saveConnection(conn)
	return folders, images, nil
}

func (app *galleryApp) syncFolderTree(
	ctx context.Context,
	db *gorm.DB,
	conn connectionSettings,
	driveID, name, parentDriveID, parentPath string,
	depth, sortBase int,
	now time.Time,
	seenFolders, seenImages map[string]struct{},
) (folders, images int, err error) {
	slug := slugify(name)
	path := slug
	if parentPath != "" {
		path = parentPath + "/" + slug
	}

	var clash galleryFolder
	if err := db.Table(app.foldersTable).Where("path = ? AND drive_id <> ?", path, driveID).First(&clash).Error; err == nil {
		suffix := shortID(driveID)
		path = path + "-" + suffix
		slug = slug + "-" + suffix
	}

	title := name
	var prior galleryFolder
	hasPrior := db.Table(app.foldersTable).Where("drive_id = ?", driveID).First(&prior).Error == nil
	if hasPrior && strings.TrimSpace(prior.Title) != "" {
		title = prior.Title
	}

	folder := galleryFolder{
		DriveID:       driveID,
		ParentDriveID: parentDriveID,
		Name:          name,
		Title:         title,
		Slug:          slug,
		Path:          path,
		Depth:         depth,
		SortOrder:     sortBase,
		Status:        "active",
		SyncedAt:      now,
		UpdatedAt:     now,
		CreatedAt:     now,
	}
	if hasPrior {
		folder.ID = prior.ID
		folder.Description = prior.Description
		folder.CreatedAt = prior.CreatedAt
		if err := db.Table(app.foldersTable).Where("drive_id = ?", driveID).Updates(map[string]any{
			"parent_drive_id": folder.ParentDriveID,
			"name":            folder.Name,
			"title":           folder.Title,
			"slug":            folder.Slug,
			"path":            folder.Path,
			"depth":           folder.Depth,
			"sort_order":      folder.SortOrder,
			"status":          "active",
			"synced_at":       now,
			"updated_at":      now,
		}).Error; err != nil {
			return 0, 0, err
		}
	} else {
		if err := db.Table(app.foldersTable).Create(&folder).Error; err != nil {
			return 0, 0, err
		}
	}
	seenFolders[driveID] = struct{}{}
	folders++

	imgs, err := app.listDriveImages(ctx, driveID)
	if err != nil {
		return folders, images, err
	}
	for i, img := range imgs {
		if conn.EnsurePublic {
			_ = app.ensureAnyoneWithLink(ctx, img.ID)
		}
		if err := app.upsertImage(db, driveID, img, i, now); err != nil {
			return folders, images, err
		}
		images++
		seenImages[img.ID] = struct{}{}
	}

	children, err := app.listDriveFolders(ctx, driveID)
	if err != nil {
		return folders, images, err
	}
	for i, child := range children {
		nf, ni, err := app.syncFolderTree(ctx, db, conn, child.ID, child.Name, driveID, path, depth+1, sortBase+i+1, now, seenFolders, seenImages)
		folders += nf
		images += ni
		if err != nil {
			return folders, images, err
		}
	}
	return folders, images, nil
}

func (app *galleryApp) upsertImage(db *gorm.DB, folderDriveID string, img driveImageFile, sort int, now time.Time) error {
	var prior galleryImage
	hasPrior := db.Table(app.imagesTable).Where("drive_file_id = ?", img.ID).First(&prior).Error == nil
	slug := slugify(img.Name)
	if hasPrior {
		if strings.TrimSpace(prior.Slug) != "" {
			slug = prior.Slug
		}
		return db.Table(app.imagesTable).Where("drive_file_id = ?", img.ID).Updates(map[string]any{
			"folder_drive_id": folderDriveID,
			"name":            img.Name,
			"slug":            slug,
			"mime_type":       img.MimeType,
			"view_url":        publicViewURL(img.ID),
			"thumb_url":       publicThumbURL(img.ID, 800),
			"width":           img.Width,
			"height":          img.Height,
			"size_bytes":      img.Size,
			"modified_time":   img.ModifiedTime,
			"sort_order":      sort,
			"status":          "active",
			"synced_at":       now,
			"updated_at":      now,
		}).Error
	}
	row := galleryImage{
		DriveFileID:   img.ID,
		FolderDriveID: folderDriveID,
		Name:          img.Name,
		Slug:          slug,
		MimeType:      img.MimeType,
		ViewURL:       publicViewURL(img.ID),
		ThumbURL:      publicThumbURL(img.ID, 800),
		Width:         img.Width,
		Height:        img.Height,
		SizeBytes:     img.Size,
		ModifiedTime:  img.ModifiedTime,
		SortOrder:     sort,
		Status:        "active",
		SyncedAt:      now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	return db.Table(app.imagesTable).Create(&row).Error
}

func (app *galleryApp) deactivateMissing(db *gorm.DB, seenFolders, seenImages map[string]struct{}) error {
	var folders []galleryFolder
	if err := db.Table(app.foldersTable).Where("status = ?", "active").Find(&folders).Error; err != nil {
		return err
	}
	for _, f := range folders {
		if _, ok := seenFolders[f.DriveID]; !ok {
			_ = db.Table(app.foldersTable).Where("drive_id = ?", f.DriveID).Update("status", "inactive")
		}
	}
	var images []galleryImage
	if err := db.Table(app.imagesTable).Where("status = ?", "active").Find(&images).Error; err != nil {
		return err
	}
	for _, img := range images {
		if _, ok := seenImages[img.DriveFileID]; !ok {
			_ = db.Table(app.imagesTable).Where("drive_file_id = ?", img.DriveFileID).Update("status", "inactive")
		}
	}
	return nil
}

func (app *galleryApp) recordSyncError(conn connectionSettings, err error) {
	if err == nil {
		return
	}
	conn.LastSyncError = err.Error()
	_ = app.saveConnection(conn)
}

func shortID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) <= 6 {
		return id
	}
	return id[len(id)-6:]
}

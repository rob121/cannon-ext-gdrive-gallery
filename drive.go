package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"google.golang.org/api/drive/v3"
)

type driveFolder struct {
	ID   string
	Name string
}

type driveImageFile struct {
	ID           string
	Name         string
	MimeType     string
	Size         int64
	ModifiedTime string
	Width        int64
	Height       int64
}

func (app *galleryApp) listDriveFolders(ctx context.Context, parentID string) ([]driveFolder, error) {
	svc, _, err := app.driveService(ctx)
	if err != nil {
		return nil, err
	}
	parentID = strings.TrimSpace(parentID)
	if parentID == "" {
		parentID = "root"
	}
	q := fmt.Sprintf("'%s' in parents and mimeType = 'application/vnd.google-apps.folder' and trashed = false", escapeDriveQuery(parentID))
	var out []driveFolder
	pageToken := ""
	for {
		call := svc.Files.List().
			Q(q).
			Spaces("drive").
			Fields("nextPageToken, files(id, name)").
			OrderBy("name").
			PageSize(100).
			SupportsAllDrives(true).
			IncludeItemsFromAllDrives(true)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Context(ctx).Do()
		if err != nil {
			return nil, err
		}
		for _, f := range resp.Files {
			out = append(out, driveFolder{ID: f.Id, Name: f.Name})
		}
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func (app *galleryApp) listDriveImages(ctx context.Context, parentID string) ([]driveImageFile, error) {
	svc, _, err := app.driveService(ctx)
	if err != nil {
		return nil, err
	}
	parentID = strings.TrimSpace(parentID)
	if parentID == "" {
		return nil, fmt.Errorf("parent folder is required")
	}
	// Image mime types commonly used in photo galleries.
	q := fmt.Sprintf(
		"'%s' in parents and trashed = false and (mimeType contains 'image/' or mimeType = 'application/vnd.google-apps.photo')",
		escapeDriveQuery(parentID),
	)
	var out []driveImageFile
	pageToken := ""
	for {
		call := svc.Files.List().
			Q(q).
			Spaces("drive").
			Fields("nextPageToken, files(id, name, mimeType, size, modifiedTime, imageMediaMetadata(width,height))").
			OrderBy("name").
			PageSize(100).
			SupportsAllDrives(true).
			IncludeItemsFromAllDrives(true)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Context(ctx).Do()
		if err != nil {
			return nil, err
		}
		for _, f := range resp.Files {
			item := driveImageFile{
				ID:           f.Id,
				Name:         f.Name,
				MimeType:     f.MimeType,
				Size:         f.Size,
				ModifiedTime: f.ModifiedTime,
			}
			if f.ImageMediaMetadata != nil {
				item.Width = f.ImageMediaMetadata.Width
				item.Height = f.ImageMediaMetadata.Height
			}
			out = append(out, item)
		}
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}
	return out, nil
}

func (app *galleryApp) ensureAnyoneWithLink(ctx context.Context, fileID string) error {
	svc, _, err := app.driveService(ctx)
	if err != nil {
		return err
	}
	_, err = svc.Permissions.Create(fileID, &drive.Permission{
		Type: "anyone",
		Role: "reader",
	}).SupportsAllDrives(true).Context(ctx).Do()
	if err != nil {
		// Ignore "already exists" style failures — permission may already be present.
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "already") || strings.Contains(msg, "duplicate") {
			return nil
		}
		return err
	}
	return nil
}

func publicViewURL(fileID string) string {
	// Large thumbnail URL for in-browser viewing (uc?export=view often forces a download).
	return publicThumbURL(fileID, 2400)
}

func publicThumbURL(fileID string, width int) string {
	if width <= 0 {
		width = 800
	}
	return fmt.Sprintf("https://drive.google.com/thumbnail?id=%s&sz=w%d", urlQueryEscapeID(fileID), width)
}

func urlQueryEscapeID(id string) string {
	// Drive IDs are URL-safe; keep simple.
	return strings.TrimSpace(id)
}

func escapeDriveQuery(s string) string {
	return strings.ReplaceAll(s, `'`, `\'`)
}

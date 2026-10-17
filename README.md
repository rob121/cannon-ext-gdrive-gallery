# cannon-ext-gdrive-gallery

Cannon extension that exposes photo galleries from a Google Drive folder. Images remain on Drive (anyone-with-link); folder/image metadata is cached locally with editable descriptions.

OAuth and folder picking follow the same pattern as `cannon-ext-backup-gdrive`.

## Build

```bash
go build -o cannon-ext-gdrive-gallery .
cp cannon-ext-gdrive-gallery /path/to/extensions/
```

## Setup

1. Enable the extension for a site.
2. **Extension Apps → Drive Gallery → Setup** — OAuth credentials, Connect Drive, choose root folder.
3. Redirect URI: `/admin/extension-apps/cannon-ext-gdrive-gallery/oauth/callback`
4. **Sync now**, then add routes:
   - **Gallery Index** → e.g. `/gallery`
   - **Gallery Folder** → e.g. `/gallery/*` (or bind a specific folder)

Subfolders become separate gallery pages and can be linked from menus via the folder page type.

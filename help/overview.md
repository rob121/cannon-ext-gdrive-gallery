# Google Drive Gallery

Photo galleries sourced from a Google Drive folder. Images stay on Drive (shared as anyone-with-link); folder and image metadata is cached locally so you can attach descriptions.

## Setup

1. Enable **Drive Gallery** for the site.
2. Open **Extension Apps → Drive Gallery → Setup**.
3. Add a Google OAuth Web client (same pattern as Drive Backup / rclone).
4. Register redirect URI: `/admin/extension-apps/cannon-ext-gdrive-gallery/oauth/callback`
5. Connect Drive, choose the **root gallery folder**, then **Sync**.

## Public pages

- **Gallery Index** — lists top-level subfolders of the root (with each folder’s title/description).
- **Gallery Folder** — one synced folder (images + child gallery links). Bind a folder in route metadata or infer from the URL path.
- Clicking a photo opens an in-page lightbox (prev/next, Escape to close) instead of downloading the file.

## Editing folder pages

1. **Extension Apps → Drive Gallery**
2. Open a synced gallery with **Edit**
3. Set **Title** and **Page description** — shown on the public folder page and index card
4. Caption individual images in the same screen

Folder titles/descriptions are stored locally and kept across syncs.

## Notes

- Sync walks subfolders recursively; each folder becomes a gallery page.
- Image files are not downloaded; thumbnails and large views use Drive’s thumbnail API for in-browser display.
- Optional **Ensure public** setting attempts to grant anyone-with-link reader on sync.

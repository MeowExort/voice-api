package handler

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/meowexort/voice-api/internal/model"
	"github.com/minio/minio-go/v7"
)

// --- Sticker Packs ---

type createStickerPackRequest struct {
	Name string `json:"name" binding:"required,min=1,max=100"`
}

func (h *Handler) createStickerPack(c *gin.Context) {
	serverID := c.Param("serverId")
	userID := c.GetString("user_id")

	if !h.hasServerPermission(userID, serverID, model.PermManageServer) {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	var req createStickerPackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var id string
	err := h.db.QueryRow(context.Background(),
		`INSERT INTO sticker_packs (server_id, name) VALUES ($1, $2) RETURNING id`, serverID, req.Name,
	).Scan(&id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create sticker pack"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": id, "name": req.Name, "server_id": serverID})
}

func (h *Handler) getStickerPacks(c *gin.Context) {
	serverID := c.Param("serverId")
	userID := c.GetString("user_id")

	if !h.isMember(userID, serverID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member"})
		return
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT id, name, created_at FROM sticker_packs WHERE server_id = $1 OR server_id IS NULL ORDER BY created_at`, serverID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch sticker packs"})
		return
	}
	defer rows.Close()

	var packs []gin.H
	for rows.Next() {
		var id, name string
		var createdAt time.Time
		if err := rows.Scan(&id, &name, &createdAt); err != nil {
			continue
		}
		packs = append(packs, gin.H{"id": id, "name": name, "created_at": createdAt})
	}
	if packs == nil {
		packs = []gin.H{}
	}
	c.JSON(http.StatusOK, packs)
}

func (h *Handler) deleteStickerPack(c *gin.Context) {
	serverID := c.Param("serverId")
	packID := c.Param("packId")
	userID := c.GetString("user_id")

	if !h.hasServerPermission(userID, serverID, model.PermManageServer) {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	h.db.Exec(context.Background(), `DELETE FROM sticker_packs WHERE id = $1 AND server_id = $2`, packID, serverID)
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

// --- Stickers ---

func (h *Handler) uploadSticker(c *gin.Context) {
	serverID := c.Param("serverId")
	packID := c.Param("packId")
	userID := c.GetString("user_id")

	if !h.hasServerPermission(userID, serverID, model.PermManageServer) {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	name := c.PostForm("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name required"})
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file required"})
		return
	}
	defer file.Close()

	if header.Size > 2<<20 { // 2MB max for stickers
		c.JSON(http.StatusBadRequest, gin.H{"error": "sticker too large (max 2MB)"})
		return
	}

	if err := h.ensureBucket(bucketStickers); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "storage error"})
		return
	}

	ext := filepath.Ext(header.Filename)
	objectName := fmt.Sprintf("%s/%s/%s%s", serverID, packID, time.Now().Format("20060102150405"), ext)
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/png"
	}

	_, err = h.minio.PutObject(context.Background(), bucketStickers, objectName, file, header.Size,
		minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to upload sticker"})
		return
	}

	url := fmt.Sprintf("/files/%s/%s", bucketStickers, objectName)

	var id string
	err = h.db.QueryRow(context.Background(),
		`INSERT INTO stickers (pack_id, name, url, content_type) VALUES ($1, $2, $3, $4) RETURNING id`,
		packID, name, url, contentType,
	).Scan(&id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save sticker"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": id, "name": name, "url": url, "content_type": contentType})
}

func (h *Handler) getStickers(c *gin.Context) {
	serverID := c.Param("serverId")
	packID := c.Param("packId")
	userID := c.GetString("user_id")

	if !h.isMember(userID, serverID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member"})
		return
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT id, name, url, content_type FROM stickers WHERE pack_id = $1 ORDER BY created_at`, packID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch stickers"})
		return
	}
	defer rows.Close()

	var stickers []gin.H
	for rows.Next() {
		var id, name, url, ct string
		if err := rows.Scan(&id, &name, &url, &ct); err != nil {
			continue
		}
		stickers = append(stickers, gin.H{"id": id, "name": name, "url": url, "content_type": ct})
	}
	if stickers == nil {
		stickers = []gin.H{}
	}
	c.JSON(http.StatusOK, stickers)
}

func (h *Handler) deleteSticker(c *gin.Context) {
	serverID := c.Param("serverId")
	stickerID := c.Param("stickerId")
	userID := c.GetString("user_id")

	if !h.hasServerPermission(userID, serverID, model.PermManageServer) {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	h.db.Exec(context.Background(), `DELETE FROM stickers WHERE id = $1`, stickerID)
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

// --- Custom Emoji ---

func (h *Handler) uploadEmoji(c *gin.Context) {
	serverID := c.Param("serverId")
	userID := c.GetString("user_id")

	if !h.hasServerPermission(userID, serverID, model.PermManageServer) {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	name := c.PostForm("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name required"})
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file required"})
		return
	}
	defer file.Close()

	if header.Size > 512<<10 { // 512KB max for emoji
		c.JSON(http.StatusBadRequest, gin.H{"error": "emoji too large (max 512KB)"})
		return
	}

	if err := h.ensureBucket(bucketEmojis); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "storage error"})
		return
	}

	ext := filepath.Ext(header.Filename)
	objectName := fmt.Sprintf("%s/%s%s", serverID, time.Now().Format("20060102150405"), ext)
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/png"
	}

	_, err = h.minio.PutObject(context.Background(), bucketEmojis, objectName, file, header.Size,
		minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to upload emoji"})
		return
	}

	url := fmt.Sprintf("/files/%s/%s", bucketEmojis, objectName)

	var id string
	err = h.db.QueryRow(context.Background(),
		`INSERT INTO emojis (server_id, name, url, creator_id) VALUES ($1, $2, $3, $4) RETURNING id`,
		serverID, name, url, userID,
	).Scan(&id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save emoji (name may already exist)"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": id, "name": name, "url": url})
}

func (h *Handler) getEmojis(c *gin.Context) {
	serverID := c.Param("serverId")
	userID := c.GetString("user_id")

	if !h.isMember(userID, serverID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member"})
		return
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT id, name, url FROM emojis WHERE server_id = $1 ORDER BY created_at`, serverID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch emojis"})
		return
	}
	defer rows.Close()

	var emojis []gin.H
	for rows.Next() {
		var id, name, url string
		if err := rows.Scan(&id, &name, &url); err != nil {
			continue
		}
		emojis = append(emojis, gin.H{"id": id, "name": name, "url": url})
	}
	if emojis == nil {
		emojis = []gin.H{}
	}
	c.JSON(http.StatusOK, emojis)
}

func (h *Handler) deleteEmoji(c *gin.Context) {
	serverID := c.Param("serverId")
	emojiID := c.Param("emojiId")
	userID := c.GetString("user_id")

	if !h.hasServerPermission(userID, serverID, model.PermManageServer) {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	h.db.Exec(context.Background(), `DELETE FROM emojis WHERE id = $1 AND server_id = $2`, emojiID, serverID)
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

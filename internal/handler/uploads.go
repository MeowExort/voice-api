package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/meowexort/voice-api/internal/model"
	"github.com/minio/minio-go/v7"
)

const (
	bucketAttachments = "attachments"
	bucketStickers    = "stickers"
	bucketEmojis      = "emojis"
	maxUploadSize     = 25 << 20 // 25 MB
)

func (h *Handler) ensureBucket(bucket string) error {
	ctx := context.Background()
	exists, err := h.minio.BucketExists(ctx, bucket)
	if err != nil {
		return err
	}
	if !exists {
		return h.minio.MakeBucket(ctx, bucket, minio.MakeBucketOptions{})
	}
	return nil
}

func (h *Handler) uploadAttachment(c *gin.Context) {
	serverID := c.Param("serverId")
	channelID := c.Param("channelId")
	messageID := c.Param("messageId")
	userID := c.GetString("user_id")

	if !h.isMember(userID, serverID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member"})
		return
	}
	if !h.hasServerPermission(userID, serverID, model.PermAttachFiles) {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	// Verify message belongs to this channel and user
	var authorID string
	err := h.db.QueryRow(context.Background(),
		`SELECT author_id FROM messages WHERE id = $1 AND channel_id = $2`, messageID, channelID,
	).Scan(&authorID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "message not found"})
		return
	}
	if authorID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "can only attach to own messages"})
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file required"})
		return
	}
	defer file.Close()

	if header.Size > maxUploadSize {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file too large (max 25MB)"})
		return
	}

	if err := h.ensureBucket(bucketAttachments); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "storage error"})
		return
	}

	ext := filepath.Ext(header.Filename)
	objectName := fmt.Sprintf("%s/%s/%s%s", channelID, messageID, time.Now().Format("20060102150405"), ext)
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	_, err = h.minio.PutObject(context.Background(), bucketAttachments, objectName, file, header.Size,
		minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to upload file"})
		return
	}

	url := fmt.Sprintf("/files/%s/%s", bucketAttachments, objectName)

	var attachID string
	err = h.db.QueryRow(context.Background(),
		`INSERT INTO attachments (message_id, filename, url, content_type, size_bytes) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		messageID, header.Filename, url, contentType, header.Size,
	).Scan(&attachID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save attachment"})
		return
	}

	attachment := gin.H{
		"id": attachID, "filename": header.Filename, "url": url,
		"content_type": contentType, "size_bytes": header.Size,
	}

	// Notify via WebSocket
	eventData, _ := json.Marshal(gin.H{
		"message_id": messageID, "channel_id": channelID, "attachment": attachment,
	})
	h.hub.PublishToChannel(channelID, WSEvent{Type: "attachment_add", Data: eventData})

	c.JSON(http.StatusCreated, attachment)
}

// serveFile proxies file downloads from MinIO
func (h *Handler) serveFile(c *gin.Context) {
	bucket := c.Param("bucket")
	objectPath := c.Param("path")
	if objectPath != "" && objectPath[0] == '/' {
		objectPath = objectPath[1:]
	}

	obj, err := h.minio.GetObject(context.Background(), bucket, objectPath, minio.GetObjectOptions{})
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}
	defer obj.Close()

	info, err := obj.Stat()
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}

	c.Header("Content-Type", info.ContentType)
	c.Header("Content-Length", fmt.Sprintf("%d", info.Size))
	c.DataFromReader(http.StatusOK, info.Size, info.ContentType, obj, nil)
}

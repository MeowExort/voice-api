package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/meowexort/voice-api/internal/middleware"
	"github.com/minio/minio-go/v7"
	"github.com/redis/go-redis/v9"
)

type Handler struct {
	db        *pgxpool.Pool
	redis     *redis.Client
	minio     *minio.Client
	jwtSecret string
	hub       *Hub
}

func New(db *pgxpool.Pool, rdb *redis.Client, minioClient *minio.Client, jwtSecret string) *Handler {
	hub := NewHub(rdb)
	go hub.Run()

	return &Handler{
		db:        db,
		redis:     rdb,
		minio:     minioClient,
		jwtSecret: jwtSecret,
		hub:       hub,
	}
}

func (h *Handler) RegisterRoutes(r *gin.Engine) {
	r.GET("/health", h.health)

	// File serving (public, URLs contain unique paths)
	r.GET("/files/:bucket/*path", h.serveFile)

	api := r.Group("/api/v1")

	// auth (публичные)
	api.POST("/auth/register", h.register)
	api.POST("/auth/login", h.login)

	// invite join (авторизованный, но без привязки к серверу)
	api.POST("/invites/:code/join", middleware.AuthRequired(h.jwtSecret), h.joinByInvite)

	// защищённые маршруты
	auth := api.Group("", middleware.AuthRequired(h.jwtSecret))

	// WebSocket
	auth.GET("/ws", h.wsConnect)

	auth.GET("/users/me", h.me)

	// серверы
	auth.POST("/servers", h.createServer)
	auth.GET("/servers", h.getServers)

	srv := auth.Group("/servers/:serverId")
	srv.GET("", h.getServer)
	srv.PATCH("", h.updateServer)
	srv.DELETE("", h.deleteServer)

	// участники
	srv.GET("/members", h.getMembers)

	// категории
	srv.POST("/categories", h.createCategory)
	srv.GET("/categories", h.getCategories)
	srv.PATCH("/categories/:categoryId", h.updateCategory)
	srv.DELETE("/categories/:categoryId", h.deleteCategory)

	// каналы
	srv.POST("/channels", h.createChannel)
	srv.GET("/channels", h.getChannels)
	srv.PATCH("/channels/:channelId", h.updateChannel)
	srv.DELETE("/channels/:channelId", h.deleteChannel)

	// сообщения
	ch := srv.Group("/channels/:channelId")
	ch.GET("/messages", h.getMessages)
	ch.POST("/messages", h.sendMessage)
	ch.PATCH("/messages/:messageId", h.editMessage)
	ch.DELETE("/messages/:messageId", h.deleteMessage)
	ch.POST("/messages/:messageId/attachments", h.uploadAttachment)
	ch.POST("/ack", h.ackMessages)

	// уведомления (непрочитанные)
	srv.GET("/unread", h.getUnreadCounts)

	// роли
	srv.POST("/roles", h.createRole)
	srv.GET("/roles", h.getRoles)
	srv.PATCH("/roles/:roleId", h.updateRole)
	srv.DELETE("/roles/:roleId", h.deleteRole)
	srv.POST("/roles/:roleId/assign", h.assignRole)
	srv.POST("/roles/:roleId/revoke", h.revokeRole)

	// инвайты
	srv.POST("/invites", h.createInvite)
	srv.GET("/invites", h.getInvites)
	srv.DELETE("/invites/:code", h.deleteInvite)

	// стикеры
	srv.POST("/sticker-packs", h.createStickerPack)
	srv.GET("/sticker-packs", h.getStickerPacks)
	srv.DELETE("/sticker-packs/:packId", h.deleteStickerPack)
	srv.POST("/sticker-packs/:packId/stickers", h.uploadSticker)
	srv.GET("/sticker-packs/:packId/stickers", h.getStickers)
	srv.DELETE("/stickers/:stickerId", h.deleteSticker)

	// эмодзи
	srv.POST("/emojis", h.uploadEmoji)
	srv.GET("/emojis", h.getEmojis)
	srv.DELETE("/emojis/:emojiId", h.deleteEmoji)
}

func (h *Handler) health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

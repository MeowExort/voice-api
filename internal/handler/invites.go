package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/meowexort/voice-api/internal/model"
)

type createInviteRequest struct {
	MaxUses   *int `json:"max_uses"`
	ExpiresIn *int `json:"expires_in"` // секунды
}

func generateCode() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (h *Handler) createInvite(c *gin.Context) {
	serverID := c.Param("serverId")
	userID := c.GetString("user_id")

	if !h.hasServerPermission(userID, serverID, model.PermCreateInvite) {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	var req createInviteRequest
	c.ShouldBindJSON(&req)

	code := generateCode()

	var expiresAt *time.Time
	if req.ExpiresIn != nil && *req.ExpiresIn > 0 {
		t := time.Now().Add(time.Duration(*req.ExpiresIn) * time.Second)
		expiresAt = &t
	}

	_, err := h.db.Exec(context.Background(),
		`INSERT INTO invites (code, server_id, creator_id, max_uses, expires_at) VALUES ($1, $2, $3, $4, $5)`,
		code, serverID, userID, req.MaxUses, expiresAt,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create invite"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"code": code, "expires_at": expiresAt, "max_uses": req.MaxUses})
}

func (h *Handler) getInvites(c *gin.Context) {
	serverID := c.Param("serverId")
	userID := c.GetString("user_id")

	if !h.hasServerPermission(userID, serverID, model.PermManageServer) {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT code, creator_id, max_uses, uses, expires_at, created_at FROM invites WHERE server_id = $1`, serverID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch invites"})
		return
	}
	defer rows.Close()

	var invites []gin.H
	for rows.Next() {
		var code, creatorID string
		var maxUses *int
		var uses int
		var expiresAt, createdAt *time.Time
		if err := rows.Scan(&code, &creatorID, &maxUses, &uses, &expiresAt, &createdAt); err != nil {
			continue
		}
		invites = append(invites, gin.H{
			"code": code, "creator_id": creatorID, "max_uses": maxUses,
			"uses": uses, "expires_at": expiresAt, "created_at": createdAt,
		})
	}
	if invites == nil {
		invites = []gin.H{}
	}

	c.JSON(http.StatusOK, invites)
}

func (h *Handler) joinByInvite(c *gin.Context) {
	code := c.Param("code")
	userID := c.GetString("user_id")

	var serverID string
	var maxUses *int
	var uses int
	var expiresAt *time.Time
	err := h.db.QueryRow(context.Background(),
		`SELECT server_id, max_uses, uses, expires_at FROM invites WHERE code = $1`, code,
	).Scan(&serverID, &maxUses, &uses, &expiresAt)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "invite not found"})
		return
	}

	if expiresAt != nil && time.Now().After(*expiresAt) {
		c.JSON(http.StatusGone, gin.H{"error": "invite expired"})
		return
	}
	if maxUses != nil && uses >= *maxUses {
		c.JSON(http.StatusGone, gin.H{"error": "invite max uses reached"})
		return
	}

	if h.isMember(userID, serverID) {
		c.JSON(http.StatusConflict, gin.H{"error": "already a member"})
		return
	}

	tx, err := h.db.Begin(context.Background())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to begin transaction"})
		return
	}
	defer tx.Rollback(context.Background())

	var memberID string
	err = tx.QueryRow(context.Background(),
		`INSERT INTO members (server_id, user_id) VALUES ($1, $2) RETURNING id`, serverID, userID,
	).Scan(&memberID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to join server"})
		return
	}

	// назначаем роль @everyone
	var everyoneRoleID string
	err = tx.QueryRow(context.Background(),
		`SELECT id FROM roles WHERE server_id = $1 AND name = '@everyone'`, serverID,
	).Scan(&everyoneRoleID)
	if err == nil {
		tx.Exec(context.Background(),
			`INSERT INTO member_roles (member_id, role_id) VALUES ($1, $2)`, memberID, everyoneRoleID,
		)
	}

	tx.Exec(context.Background(), `UPDATE invites SET uses = uses + 1 WHERE code = $1`, code)

	if err := tx.Commit(context.Background()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to commit"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"server_id": serverID, "status": "joined"})
}

func (h *Handler) deleteInvite(c *gin.Context) {
	serverID := c.Param("serverId")
	code := c.Param("code")
	userID := c.GetString("user_id")

	if !h.hasServerPermission(userID, serverID, model.PermManageServer) {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	h.db.Exec(context.Background(), `DELETE FROM invites WHERE code = $1 AND server_id = $2`, code, serverID)
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

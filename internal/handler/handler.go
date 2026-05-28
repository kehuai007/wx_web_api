package handler

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"wx_web_api/internal/config"
	"wx_web_api/internal/model"
	"wx_web_api/internal/service"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	parser      *service.ParserService
	pwd         string
	validTokens map[string]bool
}

func New(pwd string) *Handler {
	cfg := config.Get()
	tokens := cfg.Tokens
	if tokens == nil {
		tokens = []string{}
	}
	h := &Handler{
		parser:      service.NewParserService(),
		pwd:         pwd,
		validTokens: make(map[string]bool),
	}
	for _, t := range tokens {
		h.validTokens[t] = true
	}
	return h
}

func (h *Handler) IsValidToken(token string) bool {
	return h.validTokens[token]
}

// TokenAuth middleware for external API routes
func (h *Handler) TokenAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("Authorization")
		if token != "" {
			token = strings.TrimPrefix(token, "Bearer ")
		}
		if token == "" {
			token = c.Query("token")
		}
		if !h.IsValidToken(token) {
			c.AbortWithStatusJSON(401, model.SimpleResponse{Code: 401, Msg: "unauthorized"})
			return
		}
		c.Next()
	}
}

// SessionAuth middleware for web UI routes (challenge-response)
func (h *Handler) SessionAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("Authorization")
		if token != "" {
			token = strings.TrimPrefix(token, "Bearer ")
		}
		if token == "" {
			token = c.Query("token")
		}
		if !h.IsValidToken(token) {
			c.AbortWithStatusJSON(401, model.SimpleResponse{Code: 401, Msg: "unauthorized"})
			return
		}
		c.Next()
	}
}

func (h *Handler) GetChallenge(c *gin.Context) {
	b := make([]byte, 16)
	rand.Read(b)
	challenge := hex.EncodeToString(b)
	c.JSON(http.StatusOK, gin.H{"code": 0, "challenge": challenge})
}

func (h *Handler) Login(c *gin.Context) {
	var req struct {
		Pwd       string `json:"pwd"`
		Challenge string `json:"challenge"`
		Response  string `json:"response"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "invalid request"})
		return
	}
	if req.Pwd == "" || req.Challenge == "" || req.Response == "" {
		c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "missing parameters"})
		return
	}
	// Verify: SHA256(pwd + challenge) == response
	expected := sha256hex(h.pwd + req.Challenge)
	if expected != req.Response {
		c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "invalid password"})
		return
	}
	// Generate a session token (random 32 bytes hex)
	token := generateToken()
	h.validTokens[token] = true
	c.JSON(http.StatusOK, gin.H{"code": 0, "token": token})
}

func (h *Handler) ParseWxURL(c *gin.Context) {
	var req model.WxParseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, model.WxParseResponse{Code: 1, Msg: "url is required"})
		return
	}

	data, err := h.parser.Parse(req.URL)
	if err != nil {
		c.JSON(http.StatusOK, model.WxParseResponse{Code: 1, Msg: err.Error()})
		return
	}

	c.JSON(http.StatusOK, model.WxParseResponse{Code: 0, Msg: "success", Data: data})
}

func sha256hex(data string) string {
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])
}

func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}
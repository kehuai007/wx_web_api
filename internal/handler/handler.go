package handler

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
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
	// Verify: simpleHash(pwd + challenge) == response
	expected := simpleHash(h.pwd + req.Challenge)
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

// ParseFinderFeedByObjectID 通过 objectID/objectNonceID 解析视频信息
func (h *Handler) ParseFinderFeedByObjectID(c *gin.Context) {
	var req model.FinderFeedRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, model.WxParseResponse{Code: 1, Msg: "objectId and objectNonceId are required"})
		return
	}

	if req.ObjectID == "" || req.ObjectNonceID == "" {
		c.JSON(http.StatusOK, model.WxParseResponse{Code: 1, Msg: "objectId and objectNonceId are required"})
		return
	}

	data, err := h.parser.ParseFinderFeedByObjectID(req.ObjectID, req.ObjectNonceID)
	if err != nil {
		c.JSON(http.StatusOK, model.WxParseResponse{Code: 1, Msg: err.Error()})
		return
	}

	c.JSON(http.StatusOK, model.WxParseResponse{Code: 0, Msg: "success", Data: data})
}

func simpleHash(data string) string {
	h := uint64(0)
	primes := []uint64{31, 37, 41, 43, 47, 53, 59, 61, 67, 71, 73, 79}
	for i, c := range data {
		h += uint64(c) * primes[(i+1)%12]
	}
	return fmt.Sprintf("%016x", h)
}

func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}
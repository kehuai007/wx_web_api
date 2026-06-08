package handler

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"wx_web_api/internal/config"
	"wx_web_api/internal/model"
	"wx_web_api/internal/service"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	parser        *service.ParserService
	pwd           string
	sessionTokens map[string]bool
	dateCache     sync.Map // string (yyyy-MM-dd) -> time.Time
}

func New(pwd string) *Handler {
	return &Handler{
		parser:        service.NewParserService(),
		pwd:           pwd,
		sessionTokens: make(map[string]bool),
	}
}

// TokenAuth middleware for external API routes.
// Reads cfg.Tokens live so admin updates take effect without restart.
func (h *Handler) TokenAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("Authorization")
		if token != "" {
			token = strings.TrimPrefix(token, "Bearer ")
		}
		if token == "" {
			token = c.Query("token")
		}
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "unauthorized"})
			return
		}
		cfg := config.Get()
		matched := false
		var matchedTok *config.Token
		for i := range cfg.Tokens {
			if cfg.Tokens[i].Value == token {
				matched = true
				matchedTok = &cfg.Tokens[i]
				break
			}
		}
		if !matched {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "unauthorized"})
			return
		}
		if h.isExpired(matchedTok.ExpiresAt) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "token expired"})
			return
		}
		c.Next()
	}
}

// SessionAuth middleware for web UI routes (admin sessions).
// Session tokens are NOT subject to expiry — they live in h.sessionTokens
// only and are dropped on process restart.
func (h *Handler) SessionAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("Authorization")
		if token != "" {
			token = strings.TrimPrefix(token, "Bearer ")
		}
		if token == "" {
			token = c.Query("token")
		}
		if token == "" || !h.sessionTokens[token] {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "unauthorized"})
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
	expected := simpleHash(h.pwd + req.Challenge)
	if expected != req.Response {
		c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "invalid password"})
		return
	}
	token := generateToken()
	h.sessionTokens[token] = true
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

// isExpired: includes the expiry day itself. expires_at=2026-06-08 means
// the token is valid all day on 6/8, and rejects starting at 6/9 00:00:00.
// Malformed dates are treated as permanent (defensive — do not block requests
// just because someone hand-edited a config).
func (h *Handler) isExpired(expiresAt string) bool {
	if expiresAt == "" {
		return false
	}
	cached, ok := h.dateCache.Load(expiresAt)
	var parsed time.Time
	if ok {
		parsed = cached.(time.Time)
	} else {
		var err error
		parsed, err = time.Parse("2006-01-02", expiresAt)
		if err != nil {
			return false
		}
		h.dateCache.Store(expiresAt, parsed)
	}
	parsedDate := parsed.Add(24 * time.Hour) // start of day after expiry
	return !time.Now().Before(parsedDate)
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

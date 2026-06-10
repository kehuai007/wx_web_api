package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
	"wx_web_api/internal/config"
	"wx_web_api/internal/model"
	"wx_web_api/internal/service"
	"wx_web_api/internal/storage"

	"github.com/gin-gonic/gin"
)

// sessMu guards sessionTokens.
type Handler struct {
	parser        *service.ParserService
	storage       *storage.Storage
	pwd           string
	sessMu        sync.RWMutex
	sessionTokens map[string]bool
	dateCache     sync.Map // string (yyyy-MM-dd) -> time.Time
}

func New(pwd string, storage *storage.Storage) *Handler {
	return &Handler{
		parser:        service.NewParserService(),
		storage:       storage,
		pwd:           pwd,
		sessionTokens: make(map[string]bool),
	}
}

// TokenAuth middleware for external API routes.
// Reads cfg.Tokens live so admin updates take effect without restart.
// Sets "token_label" and "source" on the gin context for downstream handlers
// and for the writeLog helper to pick up. On any 401, an async log row is
// written with kind='auth' so admins can audit rejected calls.
func (h *Handler) TokenAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		t0 := time.Now()

		token := c.GetHeader("Authorization")
		if token != "" {
			token = strings.TrimPrefix(token, "Bearer ")
		}
		if token == "" {
			token = c.Query("token")
		}
		if token == "" {
			h.writeLog(c, t0, "", "external", "auth", 401, "missing token", nil, gin.H{"path": c.Request.Method + " " + c.FullPath()})
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "unauthorized"})
			return
		}

		cfg := config.Get()
		var matchedTok *config.Token
		for i := range cfg.Tokens {
			if cfg.Tokens[i].Value == token {
				matchedTok = &cfg.Tokens[i]
				break
			}
		}
		if matchedTok == nil {
			h.writeLog(c, t0, "", "external", "auth", 401, "unknown token", nil, gin.H{"path": c.Request.Method + " " + c.FullPath()})
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "unauthorized"})
			return
		}
		if h.isExpired(matchedTok.ExpiresAt) {
			h.writeLog(c, t0, matchedTok.Label, "external", "auth", 401, "token expired", nil, gin.H{"path": c.Request.Method + " " + c.FullPath()})
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "token expired"})
			return
		}

		// Auth ok — inject label and source for downstream handlers.
		source := c.GetHeader("X-Wx-Source")
		if source != "admin_test" {
			source = "external"
		}
		c.Set("token_label", matchedTok.Label)
		c.Set("source", source)
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
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "unauthorized"})
			return
		}
		h.sessMu.RLock()
		ok := h.sessionTokens[token]
		h.sessMu.RUnlock()
		if !ok {
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
	h.sessMu.Lock()
	h.sessionTokens[token] = true
	h.sessMu.Unlock()
	c.JSON(http.StatusOK, gin.H{"code": 0, "token": token})
}

func (h *Handler) ParseWxURL(c *gin.Context) {
	t0 := time.Now()
	label := c.GetString("token_label")
	source := c.GetString("source")

	var req model.WxParseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.writeLog(c, t0, label, source, "url", 1, "url is required", nil, gin.H{"url": ""})
		c.JSON(http.StatusOK, model.WxParseResponse{Code: 1, Msg: "url is required"})
		return
	}

	data, err := h.parser.Parse(req.URL)
	if err != nil {
		h.writeLog(c, t0, label, source, "url", 1, err.Error(), nil, gin.H{"url": req.URL})
		c.JSON(http.StatusOK, model.WxParseResponse{Code: 1, Msg: err.Error()})
		return
	}
	h.writeLog(c, t0, label, source, "url", 0, "", data, gin.H{"url": req.URL})
	c.JSON(http.StatusOK, model.WxParseResponse{Code: 0, Msg: "success", Data: data})
}

func (h *Handler) ParseFinderFeedByObjectID(c *gin.Context) {
	t0 := time.Now()
	label := c.GetString("token_label")
	source := c.GetString("source")

	var req model.FinderFeedRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.writeLog(c, t0, label, source, "finder", 1, "objectId and objectNonceId are required", nil, gin.H{})
		c.JSON(http.StatusOK, model.WxParseResponse{Code: 1, Msg: "objectId and objectNonceId are required"})
		return
	}
	if req.ObjectID == "" || req.ObjectNonceID == "" {
		h.writeLog(c, t0, label, source, "finder", 1, "objectId and objectNonceId are required", nil,
			gin.H{"objectId": req.ObjectID, "objectNonceId": req.ObjectNonceID})
		c.JSON(http.StatusOK, model.WxParseResponse{Code: 1, Msg: "objectId and objectNonceId are required"})
		return
	}

	data, err := h.parser.ParseFinderFeedByObjectID(req.ObjectID, req.ObjectNonceID)
	if err != nil {
		h.writeLog(c, t0, label, source, "finder", 1, err.Error(), nil,
			gin.H{"objectId": req.ObjectID, "objectNonceId": req.ObjectNonceID})
		c.JSON(http.StatusOK, model.WxParseResponse{Code: 1, Msg: err.Error()})
		return
	}
	h.writeLog(c, t0, label, source, "finder", 0, "", data,
		gin.H{"objectId": req.ObjectID, "objectNonceId": req.ObjectNonceID})
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
		// Parse in server's local timezone — a date like "2026-06-08" represents
		// the entire local day 2026-06-08, not UTC 2026-06-08 00:00:00. Using
		// time.Parse would treat the expiry as UTC and shift it by the local
		// offset, causing tokens set in the local day to be wrongly rejected.
		var err error
		parsed, err = time.ParseInLocation("2006-01-02", expiresAt, time.Local)
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

// writeLog records a request_log row asynchronously so that storage latency
// never inflates /wx P99. Goroutine captures a copy of all fields; the
// caller is free to return its HTTP response immediately.
//
// Pass kind='auth' for 401 attempts; request may be nil for those.
func (h *Handler) writeLog(c *gin.Context, t0 time.Time, tokenLabel, source, kind string, status int, msg string, result, request any) {
	if h.storage == nil {
		return
	}
	t0Copy := t0
	// Capture client IP synchronously — *gin.Context is not goroutine-safe and
	// must not be touched after the request completes.
	clientIP := c.ClientIP()
	go func() {
		var reqBytes, resBytes []byte
		if request != nil {
			reqBytes, _ = json.Marshal(request)
		}
		if result != nil {
			resBytes, _ = json.Marshal(result)
		}
		rec := &storage.RequestLog{
			Ts:         time.Now().UnixMilli(),
			TokenLabel: tokenLabel,
			Kind:       kind,
			Source:     source,
			ClientIP:   clientIP,
			Request:    reqBytes,
			Status:     status,
			LatencyMs:  time.Since(t0Copy).Milliseconds(),
			Msg:        msg,
		}
		if len(resBytes) > 0 {
			rec.Result = resBytes
		}
		if err := h.storage.LogRequest(rec); err != nil {
			log.Printf("[storage] LogRequest failed: %v", err)
		}
	}()
}

package handler

import (
	"net/http"
	"strconv"
	"time"
	"wx_web_api/internal/config"
	"wx_web_api/internal/model"
	"wx_web_api/internal/storage"

	"github.com/gin-gonic/gin"
)

// GetStats handles GET /api/stats?start=YYYY-MM-DD&end=YYYY-MM-DD
// Returns success-call counts for the [start, end] inclusive-of-start /
// exclusive-of-end range, both globally and per currently-configured token.
//
// start and end are required and interpreted in server local time. The
// end value is normalized to "end-of-day local 23:59:59.999".
// start must be >= today - retention_days; end must be <= today.
func (h *Handler) GetStats(c *gin.Context) {
	cfg := config.Get()
	startStr := c.Query("start")
	endStr := c.Query("end")
	if startStr == "" || endStr == "" {
		c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "start and end are required (yyyy-MM-dd)"})
		return
	}
	startDay, err := time.ParseInLocation("2006-01-02", startStr, time.Local)
	if err != nil {
		c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "invalid start (want yyyy-MM-dd)"})
		return
	}
	endDay, err := time.ParseInLocation("2006-01-02", endStr, time.Local)
	if err != nil {
		c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "invalid end (want yyyy-MM-dd)"})
		return
	}
	if endDay.Before(startDay) {
		c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "end must be on or after start"})
		return
	}
	// end-of-day local: next day 00:00 - 1 ms. We store ms; in CountSuccessBetween
	// the upper bound is exclusive, so the handler returns startMs and endMs+1day.
	startMs := startDay.UnixMilli()
	endExclusiveMs := endDay.AddDate(0, 0, 1).UnixMilli()

	// Retention check: start must be >= today - retentionDays
	today := time.Now()
	todayMid := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())
	earliestAllowed := todayMid.AddDate(0, 0, -cfg.HistoryRetentionDays).UnixMilli()
	if startMs < earliestAllowed {
		c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "start is older than retention window"})
		return
	}
	// end (interpreted as inclusive end-day) must be <= today
	if endDay.After(todayMid) {
		c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "end cannot be in the future"})
		return
	}

	if h.storage == nil {
		c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "storage not ready"})
		return
	}

	total, err := h.storage.CountSuccessBetween(startMs, endExclusiveMs)
	if err != nil {
		c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "storage: " + err.Error()})
		return
	}

	tokenLabels := make([]string, 0, len(cfg.Tokens))
	for _, t := range cfg.Tokens {
		tokenLabels = append(tokenLabels, t.Label)
	}
	byTokenMap, err := h.storage.CountSuccessByTokenBetween(startMs, endExclusiveMs, tokenLabels)
	if err != nil {
		c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "storage: " + err.Error()})
		return
	}

	byToken := make([]TokenStat, 0, len(tokenLabels))
	for _, label := range tokenLabels {
		byToken = append(byToken, TokenStat{
			Label: label,
			Total: byTokenMap[label], // single field; the response uses "count" via a different shape
		})
	}

	// Use a flat shape for GetStats: { range: {start,end}, success_total, by_token: [{label, count}] }
	type byTokenOut struct {
		Label string `json:"label"`
		Count int64  `json:"count"`
	}
	out := struct {
		Range        struct{ Start, End string } `json:"range"`
		SuccessTotal int64                       `json:"success_total"`
		ByToken      []byTokenOut                `json:"by_token"`
	}{}
	out.Range.Start = startStr
	out.Range.End = endStr
	out.SuccessTotal = total
	for _, bt := range byToken {
		out.ByToken = append(out.ByToken, byTokenOut{Label: bt.Label, Count: bt.Total})
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": out})
}

// GetStatsDaily handles GET /api/stats/daily?token=<label|all>&days=<1..retention>
// Returns one bucket per local day for the last N days. The handler performs
// zero-filling (days with no rows appear as count=0). Result is sorted by date
// ascending.
func (h *Handler) GetStatsDaily(c *gin.Context) {
	cfg := config.Get()
	retention := cfg.HistoryRetentionDays
	if retention < 1 {
		retention = 1
	}

	daysStr := c.DefaultQuery("days", strconv.Itoa(retention))
	days, err := strconv.Atoi(daysStr)
	if err != nil || days < 1 {
		days = retention
	}
	if days > retention {
		days = retention
	}

	tokenFilter := c.Query("token")
	if tokenFilter == "all" {
		tokenFilter = ""
	}

	if h.storage == nil {
		c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "storage not ready"})
		return
	}

	// Build the since cutoff: today - (days-1) at 00:00 local
	now := time.Now()
	todayMid := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	sinceDay := todayMid.AddDate(0, 0, -(days - 1))
	sinceMs := sinceDay.UnixMilli()

	raw, err := h.storage.DailySuccessCounts(sinceMs, tokenFilter)
	if err != nil {
		c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "storage: " + err.Error()})
		return
	}

	// Zero-fill: walk day-by-day from sinceDay to todayMid, emitting 0 for gaps.
	byDate := make(map[string]int64, len(raw))
	for _, d := range raw {
		byDate[d.Date] = d.Count
	}
	series := make([]storage.DailyCount, 0, days)
	for i := 0; i < days; i++ {
		d := sinceDay.AddDate(0, 0, i)
		key := d.Format("2006-01-02")
		series = append(series, storage.DailyCount{Date: key, Count: byDate[key]})
	}

	tokenOut := tokenFilter
	if tokenOut == "" {
		tokenOut = "all"
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"days":   days,
			"token":  tokenOut,
			"series": series,
		},
	})
}
